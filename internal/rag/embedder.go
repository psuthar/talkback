package rag

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/psuthar/talkback/internal/utils"
)

// Embedder generates embeddings for text (session-scoped RAG)
type Embedder interface {
	ModelName() string
	Embed(ctx context.Context, inputs []string) ([][]float32, error)
}

// OpenAIEmbedder uses OpenAI text-embedding-ada-002 (1536 dimensions)
type OpenAIEmbedder struct{}

func (e *OpenAIEmbedder) ModelName() string {
	return "text-embedding-ada-002"
}

func (e *OpenAIEmbedder) Embed(ctx context.Context, inputs []string) ([][]float32, error) {
	if len(inputs) == 0 {
		return nil, nil
	}
	// Batch in groups of 32 with simple retry on 429
	const batchSize = 32
	var all [][]float32
	for i := 0; i < len(inputs); i += batchSize {
		end := i + batchSize
		if end > len(inputs) {
			end = len(inputs)
		}
		batch := inputs[i:end]
		vecs, err := utils.GenerateEmbeddings(ctx, batch)
		if err != nil {
			if strings.Contains(err.Error(), "429") {
				time.Sleep(2 * time.Second)
				vecs, err = utils.GenerateEmbeddings(ctx, batch)
			}
			if err != nil {
				return nil, fmt.Errorf("embed batch: %w", err)
			}
		}
		all = append(all, vecs...)
	}
	return all, nil
}
