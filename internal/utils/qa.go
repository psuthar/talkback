package utils

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/google/uuid"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/psuthar/talkback/internal/models"
)

// QAResponse represents the structured response from the LLM
type QAResponse struct {
	AnswerStatus string            `json:"answer_status"` // "answered" or "not_covered"
	AnswerText   string            `json:"answer_text"`
	Confidence   float32           `json:"confidence"` // 0.0-1.0
	Citations    []models.Citation `json:"citations"`
}

// PriorQAPair represents a previous question-answer pair from the session
type PriorQAPair struct {
	Question string
	Answer   string
}

// GenerateAnswer uses OpenAI to generate a grounded answer from retrieved chunks
// priorQA is an optional list of previous question-answer pairs from the same session for context accumulation
// Returns the QAResponse and the chunks that were used (for debugging)
func GenerateAnswer(ctx context.Context, question string, chunks []Chunk, artifactTitle string, priorQA []PriorQAPair) (*QAResponse, []Chunk, error) {
	// Short-circuit: if no chunks retrieved, return not_covered without calling OpenAI
	if len(chunks) == 0 {
		return &QAResponse{
			AnswerStatus: "not_covered",
			AnswerText:   "The question cannot be answered from the available content in this artifact. No relevant content was found.",
			Confidence:   0.0,
			Citations:    []models.Citation{},
		}, []Chunk{}, nil
	}

	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		return &QAResponse{
			AnswerStatus: "error",
			AnswerText:   "OPENAI_API_KEY environment variable is not set",
			Confidence:   0,
			Citations:    []models.Citation{},
		}, chunks, fmt.Errorf("OPENAI_API_KEY not set")
	}

	// Build context from chunks with chunk_id
	var contextBuilder strings.Builder
	contextBuilder.WriteString("Context from artifact content:\n\n")

	for i, chunk := range chunks {
		contextBuilder.WriteString(fmt.Sprintf("[Chunk %d: chunk_id=%s, source_type=%s]\n", i+1, chunk.ChunkID, chunk.SourceType))
		if chunk.Locator != "" {
			contextBuilder.WriteString(fmt.Sprintf("Location: %s\n", chunk.Locator))
		}
		// Include full chunk text (already ~1000 chars)
		contextBuilder.WriteString(chunk.Text)
		contextBuilder.WriteString("\n\n")
	}

	// Build prior Q&A context if available (for session-aware responses)
	var priorQASection strings.Builder
	if len(priorQA) > 0 {
		priorQASection.WriteString("\n\nPREVIOUS QUESTIONS AND ANSWERS IN THIS SESSION:\n")
		priorQASection.WriteString("The following questions were asked earlier in this session. Use this context to:\n")
		priorQASection.WriteString("- Understand follow-up questions (questions that reference or build upon earlier questions)\n")
		priorQASection.WriteString("- Maintain conversational continuity\n")
		priorQASection.WriteString("- Provide more coherent answers that reference earlier discussion\n\n")

		for i, qa := range priorQA {
			priorQASection.WriteString(fmt.Sprintf("Previous Question %d: %s\n", i+1, qa.Question))
			priorQASection.WriteString(fmt.Sprintf("Previous Answer %d: %s\n\n", i+1, qa.Answer))
		}

		priorQASection.WriteString("If the current question is a follow-up or clarification related to earlier questions, incorporate the prior context while still grounding your answer in the provided context chunks.\n")
	}

	// Build system prompt with strict context-only instructions
	basePrompt := `You are a strict context-only assistant. You MUST answer questions using ONLY the provided context chunks.

CRITICAL RULES:
1. Answer STRICTLY from the provided context chunks. DO NOT use any external knowledge, general knowledge, or information not explicitly in the context.
2. If the question cannot be answered from the context, you MUST respond with answer_status="not_covered" and answer_text explaining that the information is not available in the provided context.
3. If you are unsure or the context is insufficient, set answer_status="not_covered".
4. Provide citations from the context (2-5 citations max). Each citation MUST reference a chunk_id from the provided context.
5. Each citation must include: chunk_id (REQUIRED), source_type ("material" or "transcript"), source_id, locator (if available), and a short snippet (~200-300 chars) extracted from the chunk text.
6. Set confidence between 0.0 and 1.0 based on how well the context answers the question. If confidence < 0.55, set answer_status="not_covered".
7. If the answer is not fully supported by the context, set answer_status="not_covered".`

	jsonFormatSection := `
You MUST respond in valid JSON format matching this exact structure:
{
  "answer_status": "answered" | "not_covered" | "error",
  "answer_text": "...",
  "confidence": 0.0-1.0,
  "citations": [
    {
      "chunk_id": "...",
      "source_type": "material" | "transcript",
      "source_id": "...",
      "locator": "...",
      "snippet": "..."
    }
  ]
}

IMPORTANT: Do not include any text outside the JSON structure. Do not use markdown code blocks. Return ONLY the JSON object.`

	systemPrompt := basePrompt + priorQASection.String() + jsonFormatSection

	// Build user prompt with chunk IDs clearly listed
	var chunkIDs []string
	for _, chunk := range chunks {
		chunkIDs = append(chunkIDs, chunk.ChunkID)
	}
	userPrompt := fmt.Sprintf("Artifact: %s\n\nQuestion: %s\n\nAvailable Context Chunks (chunk_id list: %s):\n%s\n\nAnswer the question using ONLY the context chunks above. If the answer is not in the context, respond with answer_status=\"not_covered\".",
		artifactTitle, question, strings.Join(chunkIDs, ", "), contextBuilder.String())

	// Create OpenAI client with API key
	client := openai.NewClient(option.WithAPIKey(apiKey))

	params := openai.ChatCompletionNewParams{
		Model: openai.ChatModelGPT4oMini,
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(systemPrompt),
			openai.UserMessage(userPrompt),
		},
	}

	response, err := client.Chat.Completions.New(ctx, params)
	if err != nil {
		return &QAResponse{
			AnswerStatus: "error",
			AnswerText:   fmt.Sprintf("Failed to generate answer: %v", err),
			Confidence:   0,
			Citations:    []models.Citation{},
		}, chunks, fmt.Errorf("failed to call OpenAI: %w", err)
	}

	if len(response.Choices) == 0 {
		return &QAResponse{
			AnswerStatus: "error",
			AnswerText:   "No response from OpenAI",
			Confidence:   0,
			Citations:    []models.Citation{},
		}, chunks, fmt.Errorf("no choices in OpenAI response")
	}

	// Parse JSON response - handle potential markdown code blocks
	content := strings.TrimSpace(response.Choices[0].Message.Content)

	// Remove markdown code blocks if present
	if strings.HasPrefix(content, "```json") {
		content = strings.TrimPrefix(content, "```json")
		content = strings.TrimSuffix(content, "```")
		content = strings.TrimSpace(content)
	} else if strings.HasPrefix(content, "```") {
		content = strings.TrimPrefix(content, "```")
		content = strings.TrimSuffix(content, "```")
		content = strings.TrimSpace(content)
	}

	var qaResponse QAResponse
	if err := json.Unmarshal([]byte(content), &qaResponse); err != nil {
		return &QAResponse{
			AnswerStatus: "error",
			AnswerText:   fmt.Sprintf("Failed to parse OpenAI response: %v. Raw content: %s", err, content[:min(200, len(content))]),
			Confidence:   0,
			Citations:    []models.Citation{},
		}, chunks, fmt.Errorf("failed to parse JSON response: %w", err)
	}

	// Validate answer_status
	if qaResponse.AnswerStatus != "answered" && qaResponse.AnswerStatus != "not_covered" && qaResponse.AnswerStatus != "error" {
		qaResponse.AnswerStatus = "error"
		qaResponse.AnswerText = fmt.Sprintf("Invalid answer_status: %s", qaResponse.AnswerStatus)
	}

	// Validate confidence range
	if qaResponse.Confidence < 0.0 {
		qaResponse.Confidence = 0.0
	} else if qaResponse.Confidence > 1.0 {
		qaResponse.Confidence = 1.0
	}

	// Ensure all citations have chunk_id
	for i := range qaResponse.Citations {
		if qaResponse.Citations[i].ChunkID == "" {
			// Try to match by source_id if chunk_id missing (backward compatibility)
			// But prefer to have chunk_id set
			qaResponse.Citations[i].ChunkID = fmt.Sprintf("unknown_%d", i)
		}
	}

	// Validate and adjust response
	if qaResponse.Confidence < 0.55 || qaResponse.AnswerStatus == "not_covered" {
		qaResponse.AnswerStatus = "not_covered"
		if qaResponse.AnswerText == "" {
			qaResponse.AnswerText = "The question cannot be answered from the available content in this artifact."
		}
		qaResponse.Citations = []models.Citation{} // Clear citations if not covered
	}

	// Limit citations to 5
	if len(qaResponse.Citations) > 5 {
		qaResponse.Citations = qaResponse.Citations[:5]
	}

	// Truncate citation snippets to ~300 chars and ensure chunk_id is set
	// Track which chunks became citations for RAG_DEBUG
	chunkMap := make(map[string]Chunk)
	for _, chunk := range chunks {
		chunkMap[chunk.ChunkID] = chunk
	}

	for i := range qaResponse.Citations {
		if len(qaResponse.Citations[i].Snippet) > 300 {
			qaResponse.Citations[i].Snippet = qaResponse.Citations[i].Snippet[:300] + "..."
		}
		// Ensure chunk_id is set (should be from LLM, but validate)
		if qaResponse.Citations[i].ChunkID == "" {
			qaResponse.Citations[i].ChunkID = fmt.Sprintf("unknown_%d", i)
		}
	}

	// RAG_DEBUG: Log which chunks became citations
	if os.Getenv("RAG_DEBUG") == "true" {
		log.Printf("[RAG_DEBUG] Citations: %d citations from %d retrieved chunks", len(qaResponse.Citations), len(chunks))
		for i, citation := range qaResponse.Citations {
			if _, exists := chunkMap[citation.ChunkID]; exists {
				log.Printf("[RAG_DEBUG] Citation %d: chunk_id=%s, source=%s, snippet_preview=%.100s...",
					i+1, citation.ChunkID, citation.SourceType, citation.Snippet)
			} else {
				log.Printf("[RAG_DEBUG] Citation %d: chunk_id=%s (chunk not found in retrieved chunks)", i+1, citation.ChunkID)
			}
		}
	}

	return &qaResponse, chunks, nil
}

// ConvertQAResponseToAnswer converts QAResponse to models.Answer
func ConvertQAResponseToAnswer(questionID uuid.UUID, qaResponse *QAResponse, model string) (*models.Answer, error) {

	answerStatus := models.AnswerStatusAnswered
	switch qaResponse.AnswerStatus {
	case "not_covered":
		answerStatus = models.AnswerStatusNotCovered
	case "error":
		answerStatus = models.AnswerStatusError
	}

	modelPtr := &model
	if model == "" {
		modelPtr = nil
	}

	return &models.Answer{
		ID:           uuid.New(),
		QuestionID:   questionID,
		AnswerText:   qaResponse.AnswerText,
		AnswerStatus: answerStatus,
		Confidence:   qaResponse.Confidence,
		Citations:    qaResponse.Citations,
		Model:        modelPtr,
	}, nil
}
