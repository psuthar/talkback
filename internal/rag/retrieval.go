// Package rag implements session-scoped chunk indexing and retrieval.
//
// Integrator-facing specification for ranking, top-k, and primary-transcript boost:
// docs/mcp-server.md — section "Deterministic ranking and top-k (SCRUM-47)".
package rag

import (
	"context"
	"fmt"
	"log"
	"math"
	"os"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/database"
	"github.com/psuthar/talkback/internal/models"
)

const DefaultTopK = 10

// PrimaryVideoScoreBoost multiplies similarity for chunks from the primary video transcript so Q&A prefers them.
const PrimaryVideoScoreBoost = 1.2

// RetrievedChunk is a ranked chunk with its deterministic cosine similarity score (after boost when applicable).
type RetrievedChunk struct {
	Chunk models.SessionChunk
	Score float64
}

// RetrieveTopKWithScores returns top-k chunks for a session by embedding similarity (cosine).
// If primaryVideoID is non-nil, chunks from that video's transcript (source_type=transcript, source_id=primaryVideoID) get a score boost so they are preferred over materials.
// See docs/mcp-server.md (SCRUM-47) for defaults, MCP top_k caps, tie behavior, and skipped rows when embedding lengths differ.
func RetrieveTopKWithScores(ctx context.Context, db *database.DB, sessionID uuid.UUID, questionEmbedding []float32, k int, primaryVideoID *uuid.UUID) ([]RetrievedChunk, error) {
	if k <= 0 {
		k = DefaultTopK
	}
	chunksWithEmb, err := db.ListChunksWithEmbeddingsBySessionID(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list chunks: %w", err)
	}
	if os.Getenv("ASK_TRACE") == "1" {
		log.Printf("[ASK_TRACE] RetrieveTopK: session_id=%s index_chunks=%d requested_k=%d", sessionID, len(chunksWithEmb), k)
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
		if primaryVideoID != nil && ce.Chunk.SourceType == "transcript" && ce.Chunk.SourceID != nil && *ce.Chunk.SourceID == *primaryVideoID {
			sim *= PrimaryVideoScoreBoost
		}
		scoredList = append(scoredList, scored{chunk: ce.Chunk, score: sim})
	}
	sort.Slice(scoredList, func(i, j int) bool { return scoredList[i].score > scoredList[j].score })
	if k > len(scoredList) {
		k = len(scoredList)
	}
	out := make([]RetrievedChunk, k)
	for i := 0; i < k; i++ {
		out[i] = RetrievedChunk{Chunk: scoredList[i].chunk, Score: scoredList[i].score}
	}
	if os.Getenv("ASK_TRACE") == "1" {
		var sourceCounts []string
		byType := make(map[string]int)
		for _, sc := range out {
			byType[sc.Chunk.SourceType]++
		}
		for t, n := range byType {
			sourceCounts = append(sourceCounts, fmt.Sprintf("%s=%d", t, n))
		}
		log.Printf("[ASK_TRACE] RetrieveTopK: returning %d chunks (%s)", len(out), strings.Join(sourceCounts, ", "))
	}
	return out, nil
}

// RetrieveTopK retrieves top-k chunks for a session by embedding similarity (cosine).
// If primaryVideoID is non-nil, chunks from that video's transcript (source_type=transcript, source_id=primaryVideoID) get a score boost so they are preferred over materials.
func RetrieveTopK(ctx context.Context, db *database.DB, sessionID uuid.UUID, questionEmbedding []float32, k int, primaryVideoID *uuid.UUID) ([]models.SessionChunk, error) {
	scored, err := RetrieveTopKWithScores(ctx, db, sessionID, questionEmbedding, k, primaryVideoID)
	if err != nil {
		return nil, err
	}
	if len(scored) == 0 {
		return nil, nil
	}
	out := make([]models.SessionChunk, len(scored))
	for i := range scored {
		out[i] = scored[i].Chunk
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
