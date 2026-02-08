package utils

import (
	"context"
	"fmt"
	"os"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
)

const defaultOpenAIBaseURL = "https://api.openai.com/v1/"

func GenerateEmbeddings(ctx context.Context, texts []string) ([][]float32, error) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("OPENAI_API_KEY environment variable is not set")
	}

	baseURL := os.Getenv("OPENAI_BASE_URL")
	if baseURL == "" {
		baseURL = defaultOpenAIBaseURL
	}
	// Ensure trailing slash so path joining works (openai-go joins paths)
	if len(baseURL) > 0 && baseURL[len(baseURL)-1] != '/' {
		baseURL += "/"
	}

	opts := []option.RequestOption{option.WithAPIKey(apiKey), option.WithBaseURL(baseURL)}
	embeddingService := openai.NewEmbeddingService(opts...)

	var allEmbeddings [][]float32

	// Process in batches (OpenAI allows up to 2048 inputs per request, but we'll use smaller batches)
	batchSize := 100
	for i := 0; i < len(texts); i += batchSize {
		end := i + batchSize
		if end > len(texts) {
			end = len(texts)
		}

		batch := texts[i:end]

		params := openai.EmbeddingNewParams{
			Input: openai.EmbeddingNewParamsInputUnion{
				OfArrayOfStrings: batch,
			},
			Model: openai.EmbeddingModelTextEmbeddingAda002,
		}

		response, err := embeddingService.New(ctx, params)
		if err != nil {
			return nil, fmt.Errorf("failed to generate embeddings: %w", err)
		}

		for _, embedding := range response.Data {
			// Convert []float64 to []float32
			float32Embedding := make([]float32, len(embedding.Embedding))
			for j, v := range embedding.Embedding {
				float32Embedding[j] = float32(v)
			}
			allEmbeddings = append(allEmbeddings, float32Embedding)
		}
	}

	return allEmbeddings, nil
}
