package rag

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/models"
	"github.com/psuthar/talkback/internal/utils"
)

const (
	MaterialChunkSize    = 1200
	MaterialChunkOverlap = 150
)

// ChunkInput is a chunk to be persisted (with content_hash for idempotency)
type ChunkInput struct {
	SessionID   uuid.UUID
	SourceType  string
	SourceID    *uuid.UUID
	ChunkIdx    int
	Text        string
	AnchorJSON  map[string]interface{}
	ContentHash string
}

// ContentHash returns SHA256 of text + stable anchor + source identifiers for idempotency
func ContentHash(text string, anchor map[string]interface{}, sessionID uuid.UUID, sourceType string, sourceID string, chunkIdx int) string {
	anchorBytes, _ := json.Marshal(anchor)
	input := fmt.Sprintf("%s|%s|%s|%s|%s|%d", text, string(anchorBytes), sessionID.String(), sourceType, sourceID, chunkIdx)
	h := sha256.Sum256([]byte(input))
	return hex.EncodeToString(h[:])
}

// BuildTranscriptChunks builds one chunk per transcript segment so citations get precise
// time ranges (e.g. "0:11–0:31" instead of a merged "0:05–0:31").
func BuildTranscriptChunks(sessionID, transcriptID uuid.UUID, segments []models.TranscriptSegmentRow) []ChunkInput {
	if len(segments) == 0 {
		return nil
	}
	chunks := make([]ChunkInput, 0, len(segments))
	for i, seg := range segments {
		if seg.Text == "" {
			continue
		}
		startMs, endMs := seg.StartMs, seg.EndMs
		if endMs <= startMs {
			endMs = startMs + 1
		}
		anchor := map[string]interface{}{
			"start_ms": startMs,
			"end_ms":   endMs,
			"segment_idx_start": seg.Idx,
			"segment_idx_end":   seg.Idx,
		}
		hash := ContentHash(seg.Text, anchor, sessionID, "transcript", transcriptID.String(), i)
		chunks = append(chunks, ChunkInput{
			SessionID:   sessionID,
			SourceType:  "transcript",
			SourceID:    &transcriptID,
			ChunkIdx:    i,
			Text:        seg.Text,
			AnchorJSON:  anchor,
			ContentHash: hash,
		})
	}
	return chunks
}

// BuildMaterialChunks chunks plain text into ~1000-1500 char blocks with overlap
func BuildMaterialChunks(sessionID, materialID uuid.UUID, text string) []ChunkInput {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	chunks := chunkText(text, MaterialChunkSize, MaterialChunkOverlap)
	var out []ChunkInput
	for i, c := range chunks {
		anchor := map[string]interface{}{"material_id": materialID.String(), "block": i}
		hash := ContentHash(c, anchor, sessionID, "material", materialID.String(), i)
		out = append(out, ChunkInput{
			SessionID:   sessionID,
			SourceType:  "material",
			SourceID:    &materialID,
			ChunkIdx:    i,
			Text:        c,
			AnchorJSON:  anchor,
			ContentHash: hash,
		})
	}
	return out
}

// BuildLinkChunks chunks extracted text from a session link; anchor includes url for citation navigation.
func BuildLinkChunks(sessionID, linkID uuid.UUID, extractedText, linkURL string) []ChunkInput {
	text := strings.TrimSpace(extractedText)
	if text == "" {
		return nil
	}
	chunks := chunkText(text, MaterialChunkSize, MaterialChunkOverlap)
	var out []ChunkInput
	for i, c := range chunks {
		anchor := map[string]interface{}{
			"type": "link",
			"url":  linkURL,
		}
		hash := ContentHash(c, anchor, sessionID, "link", linkID.String(), i)
		out = append(out, ChunkInput{
			SessionID:   sessionID,
			SourceType:  "link",
			SourceID:    &linkID,
			ChunkIdx:    i,
			Text:        c,
			AnchorJSON:  anchor,
			ContentHash: hash,
		})
	}
	return out
}

func chunkText(text string, chunkSize, overlap int) []string {
	if len(text) <= chunkSize {
		return []string{text}
	}
	var chunks []string
	start := 0
	for start < len(text) {
		end := start + chunkSize
		if end > len(text) {
			end = len(text)
		}
		chunks = append(chunks, text[start:end])
		if end >= len(text) {
			break
		}
		start = end - overlap
	}
	return chunks
}

// BuildMaterialChunksFromPDF builds chunks from a PDF file with page anchors (one chunk per non-empty page).
// filePath is the path on disk (e.g. from material.StorageURL). Returns nil if extraction fails.
func BuildMaterialChunksFromPDF(sessionID, materialID uuid.UUID, filePath string) []ChunkInput {
	path := filepath.FromSlash(filePath)
	pages, err := utils.ExtractPDFPages(path)
	if err != nil {
		return nil
	}
	var out []ChunkInput
	for i, pageText := range pages {
		if strings.TrimSpace(pageText) == "" {
			continue
		}
		pageNum := i + 1 // 1-based
		anchor := map[string]interface{}{
			"type": "page",
			"page": pageNum,
		}
		hash := ContentHash(pageText, anchor, sessionID, "material", materialID.String(), i)
		out = append(out, ChunkInput{
			SessionID:   sessionID,
			SourceType:  "material",
			SourceID:    &materialID,
			ChunkIdx:    i,
			Text:        pageText,
			AnchorJSON:  anchor,
			ContentHash: hash,
		})
	}
	return out
}

// SessionMetadataCounts holds the aggregate counts surfaced in the session metadata chunk.
// Zero-valued fields are omitted from the rendered chunk so retrieval matches stay tight.
type SessionMetadataCounts struct {
	Participants     int
	Materials        int
	MaterialsByKind  map[string]int // e.g. {"text": 1, "pdf": 0, "video": 0, "presentation": 0}
	Recordings       int
	Questions        int
	Links            int
	TranscriptStatus string // "ready" | "parsing" | "failed" | "none"
	Stances          *models.StanceAggregate
}

// BuildSessionMetadataChunks synthesizes a single chunk describing session-level metadata
// (decision fields + aggregate counts) so it becomes retrievable via the existing RAG path.
// Returns nil when the session yields no meaningful metadata (no title, no decision fields,
// no joined rows) — keeps empty sessions clean.
func BuildSessionMetadataChunks(session *models.Session, counts SessionMetadataCounts) []ChunkInput {
	if session == nil {
		return nil
	}
	var sb strings.Builder
	title := strings.TrimSpace(session.Title)
	if title == "" {
		title = "(untitled session)"
	}
	fmt.Fprintf(&sb, "Session metadata for %q.\n", title)
	fmt.Fprintf(&sb, "Status: %s. Created: %s. Last updated: %s.\n",
		string(session.Status),
		session.CreatedAt.UTC().Format("2006-01-02"),
		session.UpdatedAt.UTC().Format("2006-01-02"),
	)
	if session.Premise != nil && strings.TrimSpace(*session.Premise) != "" {
		fmt.Fprintf(&sb, "Premise: %s\n", strings.TrimSpace(*session.Premise))
	}
	if session.PrimaryDecision != nil && strings.TrimSpace(*session.PrimaryDecision) != "" {
		fmt.Fprintf(&sb, "Primary decision: %s\n", strings.TrimSpace(*session.PrimaryDecision))
	}
	if session.DecisionOutcome != nil && strings.TrimSpace(*session.DecisionOutcome) != "" {
		fmt.Fprintf(&sb, "Decision outcome: %s\n", strings.TrimSpace(*session.DecisionOutcome))
	}
	if counts.Participants > 0 {
		fmt.Fprintf(&sb, "Participants: %d member(s).\n", counts.Participants)
	}
	if counts.Materials > 0 {
		fmt.Fprintf(&sb, "Materials: %d total", counts.Materials)
		var parts []string
		for _, kind := range []string{"text", "pdf", "presentation", "video", "image", "audio"} {
			if n := counts.MaterialsByKind[kind]; n > 0 {
				parts = append(parts, fmt.Sprintf("%d %s", n, kind))
			}
		}
		if len(parts) > 0 {
			fmt.Fprintf(&sb, " (%s)", strings.Join(parts, ", "))
		}
		sb.WriteString(".\n")
	} else {
		sb.WriteString("Materials: none.\n")
	}
	if counts.Recordings > 0 {
		fmt.Fprintf(&sb, "Video recordings: %d.\n", counts.Recordings)
	} else {
		sb.WriteString("Video recordings: none.\n")
	}
	if counts.Questions > 0 {
		fmt.Fprintf(&sb, "Questions asked: %d.\n", counts.Questions)
	}
	if counts.Links > 0 {
		fmt.Fprintf(&sb, "External links: %d.\n", counts.Links)
	}
	if counts.Stances != nil && counts.Stances.Total > 0 {
		s := counts.Stances
		fmt.Fprintf(&sb, "Decision stances: %d agree, %d disagree, %d conditional, %d need more info, %d abstain.\n",
			s.Agree, s.Disagree, s.Conditional, s.NeedMoreInfo, s.Abstain)
	}
	if counts.TranscriptStatus != "" && counts.TranscriptStatus != "none" {
		fmt.Fprintf(&sb, "Transcript: %s.\n", counts.TranscriptStatus)
	}

	text := strings.TrimSpace(sb.String())
	if text == "" {
		return nil
	}
	anchor := map[string]interface{}{"type": "session_metadata"}
	sessionIDCopy := session.ID
	hash := ContentHash(text, anchor, session.ID, "session_metadata", session.ID.String(), 0)
	return []ChunkInput{{
		SessionID:   session.ID,
		SourceType:  "session_metadata",
		SourceID:    &sessionIDCopy,
		ChunkIdx:    0,
		Text:        text,
		AnchorJSON:  anchor,
		ContentHash: hash,
	}}
}

// BuildMaterialChunksFromSlides builds chunks from per-slide text (e.g. PPTX) with page anchors.
// slideTexts[i] is the text of slide i+1. Each chunk gets anchor "page" (1-based) so citations open the correct slide.
func BuildMaterialChunksFromSlides(sessionID, materialID uuid.UUID, slideTexts []string) []ChunkInput {
	var out []ChunkInput
	for i, text := range slideTexts {
		if strings.TrimSpace(text) == "" {
			continue
		}
		pageNum := i + 1 // 1-based for citation navigation (Slide 1, 2, ...)
		anchor := map[string]interface{}{
			"type": "page",
			"page": pageNum,
		}
		hash := ContentHash(text, anchor, sessionID, "material", materialID.String(), i)
		out = append(out, ChunkInput{
			SessionID:   sessionID,
			SourceType:  "material",
			SourceID:    &materialID,
			ChunkIdx:    i,
			Text:        text,
			AnchorJSON:  anchor,
			ContentHash: hash,
		})
	}
	return out
}
