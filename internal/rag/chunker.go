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
