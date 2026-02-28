package rag

import (
	"context"
	"fmt"
	"log"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/database"
	"github.com/psuthar/talkback/internal/models"
	"github.com/psuthar/talkback/internal/storage"
)

// IndexSession builds chunks from transcript + materials, embeds them, and stores (idempotent)
func IndexSession(ctx context.Context, db *database.DB, embedder Embedder, sessionID uuid.UUID) error {
	// Optional: set index_status = building
	_ = setSessionIndexStatus(db, ctx, sessionID, "building")

	// 1) Transcript chunks: prefer session-level transcript table, then fall back to video_sources (Zoom transcript on video)
	var allChunkInputs []ChunkInput
	trans, err := db.GetTranscriptBySessionID(ctx, sessionID, "")
	if err != nil {
		return fmt.Errorf("get transcript: %w", err)
	}
	if trans != nil && trans.Status == models.TranscriptStatusReady {
		segments, err := db.ListSegmentsByTranscriptID(ctx, trans.ID)
		if err != nil {
			return fmt.Errorf("list segments: %w", err)
		}
		if len(segments) > 0 {
			transChunks := BuildTranscriptChunks(sessionID, trans.ID, segments)
			allChunkInputs = append(allChunkInputs, transChunks...)
		}
	}
	if len(allChunkInputs) == 0 {
		// Fallback: index transcript from video_sources (e.g. Zoom sessions where transcript is only on video_sources)
		videoSources, err := db.GetVideoSourcesBySessionID(ctx, sessionID)
		if err != nil {
			log.Printf("IndexSession: get video sources: %v", err)
		} else {
			for _, vs := range videoSources {
				segments := videoSourceToSegmentRows(vs)
				if len(segments) > 0 {
					transChunks := BuildTranscriptChunks(sessionID, vs.ID, segments)
					allChunkInputs = append(allChunkInputs, transChunks...)
					break
				}
				if vs.TranscriptText != nil && *vs.TranscriptText != "" {
					// No segments but have raw text: one chunk for full transcript (no time anchor)
					allChunkInputs = append(allChunkInputs, ChunkInput{
						SessionID:   sessionID,
						SourceType:  "transcript",
						SourceID:    &vs.ID,
						ChunkIdx:    0,
						Text:        *vs.TranscriptText,
						AnchorJSON:  map[string]interface{}{"type": "none"},
						ContentHash: ContentHash(*vs.TranscriptText, map[string]interface{}{"type": "none"}, sessionID, "transcript", vs.ID.String(), 0),
					})
					break
				}
			}
		}
	}

	// 2) Material chunks (PDF with file path → page anchors; else block anchors from extracted text)
	materials, err := db.GetMaterialsBySessionID(ctx, sessionID)
	if err != nil {
		log.Printf("IndexSession: get materials: %v", err)
	} else {
		for _, m := range materials {
			if m.ExtractedText == nil || *m.ExtractedText == "" {
				continue
			}
			var matChunks []ChunkInput
			if (m.ContentType == "application/pdf" || strings.HasSuffix(strings.ToLower(m.Filename), ".pdf")) && m.StorageURL != "" {
				absPath := filepath.Join(storage.UploadRoot(), filepath.FromSlash(m.StorageURL))
				matChunks = BuildMaterialChunksFromPDF(sessionID, m.ID, absPath)
			}
			if matChunks == nil {
				matChunks = BuildMaterialChunks(sessionID, m.ID, *m.ExtractedText)
			}
			allChunkInputs = append(allChunkInputs, matChunks...)
		}
	}

	if len(allChunkInputs) == 0 {
		_ = setSessionIndexStatus(db, ctx, sessionID, "ready")
		return nil
	}

	// 3) Upsert chunks (idempotent) and collect ids + texts for embedding
	chunkIDs := make([]uuid.UUID, 0, len(allChunkInputs))
	texts := make([]string, 0, len(allChunkInputs))
	for _, c := range allChunkInputs {
		ins := database.SessionChunkInsert{
			SessionID:   c.SessionID,
			SourceType:  c.SourceType,
			SourceID:    c.SourceID,
			ChunkIdx:    c.ChunkIdx,
			Text:        c.Text,
			AnchorJSON:  c.AnchorJSON,
			ContentHash: c.ContentHash,
		}
		id, err := db.UpsertSessionChunk(ctx, ins)
		if err != nil {
			return fmt.Errorf("upsert chunk: %w", err)
		}
		chunkIDs = append(chunkIDs, id)
		texts = append(texts, c.Text)
	}

	// 4) Replace embeddings: delete existing for session, then embed and insert
	_ = db.DeleteChunkEmbeddingsBySessionID(ctx, sessionID)
	embeddings, err := embedder.Embed(ctx, texts)
	if err != nil {
		_ = setSessionIndexStatus(db, ctx, sessionID, "failed")
		return fmt.Errorf("embed: %w", err)
	}
	if len(embeddings) != len(chunkIDs) {
		_ = setSessionIndexStatus(db, ctx, sessionID, "failed")
		return fmt.Errorf("embed count mismatch: got %d, want %d", len(embeddings), len(chunkIDs))
	}
	for i, id := range chunkIDs {
		if i >= len(embeddings) {
			break
		}
		if err := db.InsertChunkEmbedding(ctx, id, sessionID, embedder.ModelName(), embeddings[i]); err != nil {
			log.Printf("InsertChunkEmbedding: %v", err)
		}
	}

	_ = setSessionIndexStatus(db, ctx, sessionID, "ready")
	return nil
}

func setSessionIndexStatus(db *database.DB, ctx context.Context, sessionID uuid.UUID, status string) error {
	_, err := db.Pool.Exec(ctx, `UPDATE sessions SET index_status = $1, index_updated_at = now() WHERE id = $2`, status, sessionID)
	return err
}

// videoSourceToSegmentRows converts VideoSource.TranscriptSegments (seconds) to rows (ms) for BuildTranscriptChunks.
func videoSourceToSegmentRows(vs *models.VideoSource) []models.TranscriptSegmentRow {
	if vs == nil || len(vs.TranscriptSegments) == 0 {
		return nil
	}
	rows := make([]models.TranscriptSegmentRow, 0, len(vs.TranscriptSegments))
	for i, s := range vs.TranscriptSegments {
		startMs := int(s.StartTime * 1000)
		endMs := int(s.EndTime * 1000)
		if endMs <= startMs {
			endMs = startMs + 1
		}
		rows = append(rows, models.TranscriptSegmentRow{
			SessionID: vs.SessionID,
			Idx:       i,
			StartMs:   startMs,
			EndMs:     endMs,
			Text:      s.Text,
		})
	}
	return rows
}

// EnsureSessionIndex builds index if not ready (sync on first question)
func EnsureSessionIndex(ctx context.Context, db *database.DB, embedder Embedder, sessionID uuid.UUID) error {
	nEmb, err := db.CountEmbeddingsBySessionID(ctx, sessionID)
	if err != nil {
		return err
	}
	if nEmb > 0 {
		return nil
	}
	return IndexSession(ctx, db, embedder, sessionID)
}

// IndexSessionAsync runs IndexSession in a goroutine so new content is indexed without blocking the caller.
// Call this whenever transcript or material content is added to a session.
func IndexSessionAsync(sessionID uuid.UUID, db *database.DB) {
	go func() {
		bg := context.Background()
		embedder := &OpenAIEmbedder{}
		if err := IndexSession(bg, db, embedder, sessionID); err != nil {
			log.Printf("IndexSessionAsync: %v", err)
		}
	}()
}
