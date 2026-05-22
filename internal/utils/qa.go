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
	"github.com/openai/openai-go/shared"
	"github.com/psuthar/talkback/internal/models"
)

// QAResponse represents the structured response from the LLM
type QAResponse struct {
	AnswerStatus string            `json:"answer_status"` // "answered" or "not_covered"
	AnswerText   string            `json:"answer_text"`
	Confidence   float32           `json:"confidence"` // 0.0-1.0
	Citations    []models.Citation `json:"citations"`
}

// PriorQAPair represents a previous question-answer pair from the session.
// CitationLabels (e.g. "C1: Transcript 01:12–04:38") allow follow-ups to reference "the second citation".
type PriorQAPair struct {
	Question       string
	Answer         string
	CitationLabels []string // optional: citation_id and label for each citation in this answer
}

// SessionContext carries decision-intelligence fields from the session into the LLM prompt.
type SessionContext struct {
	Premise             *string
	PrimaryDecision     *string
	AskerEmail          *string
	AskerRole           *string
	SessionCreatorEmail *string
}

// extractFirstJSONObject returns the first complete JSON object (from first '{' to matching '}').
// This avoids parse failures when the model appends extra text after the JSON (e.g. "From the slides...").
func extractFirstJSONObject(s string) string {
	start := strings.Index(s, "{")
	if start < 0 {
		return ""
	}
	depth := 0
	inString := false
	escape := false
	quote := byte(0)
	for i := start; i < len(s); i++ {
		c := s[i]
		if escape {
			escape = false
			continue
		}
		if inString {
			if c == '\\' && (quote == '"' || quote == '\'') {
				escape = true
			} else if c == quote {
				inString = false
			}
			continue
		}
		if c == '"' || c == '\'' {
			inString = true
			quote = c
			continue
		}
		if c == '{' {
			depth++
		} else if c == '}' {
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	return ""
}

// GenerateAnswer uses OpenAI to generate a grounded answer from retrieved chunks
// priorQA is an optional list of previous question-answer pairs from the same session for context accumulation
// Returns the QAResponse and the chunks that were used (for debugging)
func GenerateAnswer(ctx context.Context, question string, chunks []Chunk, artifactTitle string, sessionCtx SessionContext, priorQA []PriorQAPair) (*QAResponse, []Chunk, error) {
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
			priorQASection.WriteString(fmt.Sprintf("Previous Answer %d: %s\n", i+1, qa.Answer))
			if len(qa.CitationLabels) > 0 {
				priorQASection.WriteString(fmt.Sprintf("Citations from previous answer %d: %s\n", i+1, strings.Join(qa.CitationLabels, ", ")))
			}
			priorQASection.WriteString("\n")
		}

		priorQASection.WriteString("If the current question is a follow-up or clarification related to earlier questions, incorporate the prior context while still grounding your answer in the provided context chunks.\n")
	}

	// Build system prompt with strict context-only instructions
	basePrompt := `You are a strict context-only assistant. You MUST answer questions using ONLY the provided context chunks.

CRITICAL RULES:
1. Answer STRICTLY from the provided context chunks. DO NOT use any external knowledge, general knowledge, or information not explicitly in the context.
2. If the question cannot be answered from the context, you MUST respond with answer_status="not_covered" and answer_text explaining that the information is not available in the provided context.
3. If you are unsure or the context is insufficient, set answer_status="not_covered".
4. When the question asks whether something is mentioned, present, or included (e.g. "Is X mentioned?", "Does the transcript say Y?", "Is 4 in the list?"), and the context explicitly lists or states what is mentioned (e.g. a list of numbers, names, or items), you MUST answer "Yes" or "No" from that context and use answer_status="answered" with citations. For example: if the context says "the first five prime numbers are 2, 3, 5, 7, 11" and the question is "Is the number 4 mentioned?", answer "No. The number 4 is not mentioned. The transcript mentions the first five prime numbers: 2, 3, 5, 7, and 11." with answer_status="answered" and cite the chunk. Do not use "not_covered" when the context clearly implies the answer is no (or yes).
5. Provide citations from the context (2-5 citations max). Each citation MUST reference a chunk_id from the provided context.
6. Each citation must include: chunk_id (REQUIRED), source_type ("material", "transcript", or "session_metadata"), source_id, locator (if available), and a short snippet (~200-300 chars) extracted from the chunk text. A "session_metadata" chunk describes the session's structure (title, decision fields, counts of participants/materials/recordings/questions/links/stances) and is the authoritative source for questions about the session's shape rather than its content.
7. Set confidence between 0.0 and 1.0 based on how well the context answers the question. If confidence < 0.55, set answer_status="not_covered".
8. If the answer is not fully supported by the context, set answer_status="not_covered".`

	jsonFormatSection := `
You MUST respond in valid JSON format matching this exact structure:
{
  "answer_status": "answered" | "not_covered" | "error",
  "answer_text": "...",
  "confidence": 0.0-1.0,
  "citations": [
    {
      "chunk_id": "...",
      "source_type": "material" | "transcript" | "session_metadata",
      "source_id": "...",
      "locator": "...",
      "snippet": "..."
    }
  ]
}

IMPORTANT: Do not include any text outside the JSON structure. Do not use markdown code blocks. Return ONLY the JSON object.`

	// Build optional decision context section (injected between base prompt and JSON format)
	var decisionSection strings.Builder
	premiseStr := ""
	if sessionCtx.Premise != nil && *sessionCtx.Premise != "" {
		premiseStr = *sessionCtx.Premise
		if len(premiseStr) > 500 {
			premiseStr = premiseStr[:500]
		}
	}
	decisionStr := ""
	if sessionCtx.PrimaryDecision != nil && *sessionCtx.PrimaryDecision != "" {
		decisionStr = *sessionCtx.PrimaryDecision
		if len(decisionStr) > 500 {
			decisionStr = decisionStr[:500]
		}
	}
	if premiseStr != "" || decisionStr != "" {
		decisionSection.WriteString("\n\nDECISION CONTEXT:\n")
		if premiseStr != "" {
			decisionSection.WriteString(fmt.Sprintf("Session background: %s\n", premiseStr))
		}
		if decisionStr != "" {
			decisionSection.WriteString(fmt.Sprintf("Decision being evaluated: %s\n", decisionStr))
		}
		decisionSection.WriteString("Frame your answers within this decision context where relevant.")
	}

	systemPrompt := basePrompt + decisionSection.String() + buildIdentityContextSection(sessionCtx) + priorQASection.String() + jsonFormatSection

	// Build user prompt with chunk IDs clearly listed
	var chunkIDs []string
	for _, chunk := range chunks {
		chunkIDs = append(chunkIDs, chunk.ChunkID)
	}
	userPrompt := fmt.Sprintf("Artifact: %s\n\nQuestion: %s\n\nAvailable Context Chunks (chunk_id list: %s):\n%s\n\nAnswer the question using ONLY the context chunks above. If the answer is not in the context, respond with answer_status=\"not_covered\".",
		artifactTitle, question, strings.Join(chunkIDs, ", "), contextBuilder.String())

	// Create OpenAI client with API key and base URL (required by SDK; default https://api.openai.com/v1/)
	baseURL := os.Getenv("OPENAI_BASE_URL")
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1/"
	}
	if len(baseURL) > 0 && baseURL[len(baseURL)-1] != '/' {
		baseURL += "/"
	}
	client := openai.NewClient(option.WithAPIKey(apiKey), option.WithBaseURL(baseURL))

	params := openai.ChatCompletionNewParams{
		Model: openai.ChatModelGPT4oMini,
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(systemPrompt),
			openai.UserMessage(userPrompt),
		},
	}
	// Force valid JSON output to prevent intermittent parse failures.
	rf := shared.NewResponseFormatJSONObjectParam()
	params.ResponseFormat = openai.ChatCompletionNewParamsResponseFormatUnion{OfJSONObject: &rf}

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

	// Extract first complete JSON object so trailing model text (e.g. "From the slides...") does not break parsing
	if extracted := extractFirstJSONObject(content); extracted != "" {
		content = extracted
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

func buildIdentityContextSection(sessionCtx SessionContext) string {
	if (sessionCtx.AskerEmail == nil || strings.TrimSpace(*sessionCtx.AskerEmail) == "") &&
		(sessionCtx.AskerRole == nil || strings.TrimSpace(*sessionCtx.AskerRole) == "") &&
		(sessionCtx.SessionCreatorEmail == nil || strings.TrimSpace(*sessionCtx.SessionCreatorEmail) == "") {
		return ""
	}

	var b strings.Builder
	b.WriteString("\n\nASKER IDENTITY CONTEXT:\n")
	if sessionCtx.AskerEmail != nil && strings.TrimSpace(*sessionCtx.AskerEmail) != "" {
		b.WriteString(fmt.Sprintf("Current asker email: %s\n", strings.TrimSpace(*sessionCtx.AskerEmail)))
	}
	if sessionCtx.AskerRole != nil && strings.TrimSpace(*sessionCtx.AskerRole) != "" {
		b.WriteString(fmt.Sprintf("Current asker role in session: %s\n", strings.TrimSpace(*sessionCtx.AskerRole)))
	}
	if sessionCtx.SessionCreatorEmail != nil && strings.TrimSpace(*sessionCtx.SessionCreatorEmail) != "" {
		b.WriteString(fmt.Sprintf("Session creator email: %s\n", strings.TrimSpace(*sessionCtx.SessionCreatorEmail)))
	}
	b.WriteString("Interpret identity/access questions from the current asker's perspective. Do not describe the asker as the creator unless the asker identity explicitly matches the session creator email.")
	return b.String()
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
