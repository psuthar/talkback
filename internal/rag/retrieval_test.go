package rag

import (
	"testing"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/database"
	"github.com/psuthar/talkback/internal/models"
)

func TestCrossSessionTopKByChunks(t *testing.T) {
	t.Parallel()
	sid := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	q := []float32{1, 0, 0}
	chunks := []database.ChunkWithEmbedding{
		{Chunk: models.SessionChunk{SessionID: sid, Text: "a"}, Embedding: []float32{0.9, 0.1, 0}},
		{Chunk: models.SessionChunk{SessionID: sid, Text: "b"}, Embedding: []float32{0.2, 0.8, 0}},
	}
	out := CrossSessionTopKByChunks(chunks, q, 1)
	if len(out) != 1 {
		t.Fatalf("len %d", len(out))
	}
	if out[0].Chunk.Text != "a" {
		t.Fatalf("want first chunk, got %q", out[0].Chunk.Text)
	}
}
