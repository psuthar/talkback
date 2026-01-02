package utils

import (
	"fmt"
	"log"
	"os"
	"strings"
	"unicode"

	"github.com/psuthar/talkback/internal/models"
)

// Chunk represents a text chunk with metadata for retrieval
type Chunk struct {
	ChunkID    string // unique identifier for the chunk
	SourceType string // "material" or "transcript"
	SourceID   string // material_id or video_id
	Locator    string // timestamp or other locator
	Text       string // the chunk text (~1000 chars)
}

// RetrieveChunks performs lexical retrieval on artifact content
// Returns top K chunks sorted by relevance score
func RetrieveChunks(question string, materials []*models.Material, videoSource *models.VideoSource, topK int) []Chunk {
	if topK <= 0 {
		topK = 5
	}

	var allChunks []Chunk

	// Extract chunks from materials
	for _, material := range materials {
		if material.ExtractedText == nil || *material.ExtractedText == "" {
			continue // Ignore empty texts
		}
		text := strings.TrimSpace(*material.ExtractedText)
		if text == "" {
			continue // Ignore empty texts
		}
		chunks := chunkText(text, 1000, 200)
		for i, chunk := range chunks {
			chunkID := fmt.Sprintf("mat_%s_%d", material.ID.String(), i)
			allChunks = append(allChunks, Chunk{
				ChunkID:    chunkID,
				SourceType: "material",
				SourceID:   material.ID.String(),
				Locator:    "",
				Text:       chunk,
			})
		}
	}

	// Extract chunks from video transcript
	if videoSource != nil && videoSource.TranscriptText != nil && *videoSource.TranscriptText != "" {
		text := strings.TrimSpace(*videoSource.TranscriptText)
		if text != "" {
			chunks := chunkText(text, 1000, 200)
			for i, chunk := range chunks {
				chunkID := fmt.Sprintf("vid_%s_%d", videoSource.ID.String(), i)
				allChunks = append(allChunks, Chunk{
					ChunkID:    chunkID,
					SourceType: "transcript",
					SourceID:   videoSource.ID.String(),
					Locator:    "", // Could add timestamp parsing later
					Text:       chunk,
				})
			}
		}
	}

	// Score chunks by token overlap with question
	scoredChunks := scoreChunks(question, allChunks)

	// Log top chunks if RAG_DEBUG is enabled
	if os.Getenv("RAG_DEBUG") == "true" {
		log.Printf("[RAG_DEBUG] Retrieved %d chunks, returning top %d", len(allChunks), topK)
		for i, chunk := range scoredChunks {
			if i >= topK {
				break
			}
			// Calculate score for logging
			score := calculateChunkScore(question, chunk)
			log.Printf("[RAG_DEBUG] Chunk %d: chunk_id=%s, source=%s, score=%.4f, text_preview=%.100s...",
				i+1, chunk.ChunkID, chunk.SourceType, score, chunk.Text)
		}
	}

	// Return top K chunks
	if len(scoredChunks) > topK {
		return scoredChunks[:topK]
	}
	return scoredChunks
}

// chunkText splits text into overlapping chunks
// chunkSize: target size of each chunk
// overlap: number of characters to overlap between chunks
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

		chunk := text[start:end]
		chunks = append(chunks, chunk)

		if end >= len(text) {
			break
		}

		// Move start forward by chunkSize - overlap
		start += chunkSize - overlap
	}

	return chunks
}

// scoredChunk represents a chunk with its relevance score
type scoredChunk struct {
	chunk Chunk
	score float64
}

// scoreChunks scores chunks based on token overlap with the question
func scoreChunks(question string, chunks []Chunk) []Chunk {
	questionTokens := tokenize(question)
	questionTokenSet := make(map[string]int)
	for _, token := range questionTokens {
		questionTokenSet[token]++
	}

	var scored []scoredChunk
	for _, chunk := range chunks {
		score := calculateChunkScore(question, chunk)
		scored = append(scored, scoredChunk{
			chunk: chunk,
			score: score,
		})
	}

	// Sort by score (descending) - simple bubble sort for small arrays
	for i := 0; i < len(scored)-1; i++ {
		for j := i + 1; j < len(scored); j++ {
			if scored[i].score < scored[j].score {
				scored[i], scored[j] = scored[j], scored[i]
			}
		}
	}

	// Convert back to chunks
	result := make([]Chunk, len(scored))
	for i, s := range scored {
		result[i] = s.chunk
	}

	return result
}

// calculateChunkScore calculates the relevance score for a chunk based on token overlap
func calculateChunkScore(question string, chunk Chunk) float64 {
	questionTokens := tokenize(question)
	chunkTokens := tokenize(chunk.Text)

	if len(questionTokens) == 0 {
		return 0.0
	}

	// Create token frequency maps
	questionTokenSet := make(map[string]int)
	for _, token := range questionTokens {
		questionTokenSet[token]++
	}

	chunkTokenSet := make(map[string]int)
	for _, token := range chunkTokens {
		chunkTokenSet[token]++
	}

	// Calculate overlap: count matching tokens (weighted by frequency)
	matches := 0
	for token, qFreq := range questionTokenSet {
		if cFreq, exists := chunkTokenSet[token]; exists {
			// Use minimum frequency to avoid over-counting
			matches += min(qFreq, cFreq)
		}
	}

	// Score: matches / len(questionTokens)
	// This gives higher scores to chunks with more question tokens
	score := float64(matches) / float64(len(questionTokens))

	// Slight boost for longer chunks (more context)
	if len(chunk.Text) > 500 {
		score *= 1.05
	}

	return score
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// tokenize splits text into words (simple whitespace-based)
func tokenize(text string) []string {
	words := strings.Fields(text)
	var result []string
	for _, word := range words {
		// Remove punctuation and convert to lowercase
		cleaned := strings.Map(func(r rune) rune {
			if unicode.IsLetter(r) || unicode.IsDigit(r) {
				return r
			}
			return -1
		}, word)
		if len(cleaned) > 0 {
			result = append(result, strings.ToLower(cleaned))
		}
	}
	return result
}

