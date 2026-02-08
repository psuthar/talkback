package rag

import (
	"context"
	"fmt"
	"math"
	"sort"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/database"
	"github.com/psuthar/talkback/internal/models"
)

const DefaultTopK = 10

// RetrieveTopK retrieves top-k chunks for a session by embedding similarity (cosine)
func RetrieveTopK(ctx context.Context, db *database.DB, sessionID uuid.UUID, questionEmbedding []float32, k int) ([]models.SessionChunk, error) {
	if k <= 0 {
		k = DefaultTopK
	}
	chunksWithEmb, err := db.ListChunksWithEmbeddingsBySessionID(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list chunks: %w", err)
	}
	if len(chunksWithEmb) == 0 {
		return nil, nil
	}
	if len(questionEmbedding) == 0 {
		return nil, fmt.Errorf("question embedding is empty")
	}
	type scored struct {
		chunk models.SessionChunk
		score float64
	}
	var scoredList []scored
	for _, ce := range chunksWithEmb {
		if len(ce.Embedding) != len(questionEmbedding) {
			continue
		}
		sim := cosineSimilarity(questionEmbedding, ce.Embedding)
		scoredList = append(scoredList, scored{chunk: ce.Chunk, score: sim})
	}
	sort.Slice(scoredList, func(i, j int) bool { return scoredList[i].score > scoredList[j].score })
	if k > len(scoredList) {
		k = len(scoredList)
	}
	out := make([]models.SessionChunk, k)
	for i := 0; i < k; i++ {
		out[i] = scoredList[i].chunk
	}
	return out, nil
}

func cosineSimilarity(a, b []float32) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}
