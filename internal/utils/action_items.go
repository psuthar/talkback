package utils

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/shared"
)

// ActionItemsExtraction is the LLM JSON shape for session action-item extraction (SCRUM-56).
type ActionItemsExtraction struct {
	ActionItems []ActionItemRow `json:"action_items"`
	LowSignal   bool            `json:"low_signal"`
}

// ActionItemRow is one ephemeral action item (not persisted).
type ActionItemRow struct {
	Description string  `json:"description"`
	Owner       *string `json:"owner,omitempty"`
}

const (
	maxActionItemsPerResponse = 25
	maxActionItemDescription  = 500
	maxActionItemOwner        = 200
)

// ExtractActionItemsFromContext performs a single on-read LLM call over retrieved chunks.
// v1: no persistence. When chunks is empty, returns empty items and low_signal without calling the API.
func ExtractActionItemsFromContext(ctx context.Context, chunks []Chunk, sessionTitle string, sessionCtx SessionContext) (*ActionItemsExtraction, error) {
	if len(chunks) == 0 {
		return &ActionItemsExtraction{ActionItems: []ActionItemRow{}, LowSignal: true}, nil
	}

	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("OPENAI_API_KEY not set")
	}

	var contextBuilder strings.Builder
	contextBuilder.WriteString("Context from session content:\n\n")
	for i, chunk := range chunks {
		contextBuilder.WriteString(fmt.Sprintf("[Chunk %d: chunk_id=%s, source_type=%s]\n", i+1, chunk.ChunkID, chunk.SourceType))
		if chunk.Locator != "" {
			contextBuilder.WriteString(fmt.Sprintf("Location: %s\n", chunk.Locator))
		}
		contextBuilder.WriteString(chunk.Text)
		contextBuilder.WriteString("\n\n")
	}

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
		decisionSection.WriteString("\nDECISION CONTEXT (use when framing action items):\n")
		if premiseStr != "" {
			decisionSection.WriteString(fmt.Sprintf("Premise: %s\n", premiseStr))
		}
		if decisionStr != "" {
			decisionSection.WriteString(fmt.Sprintf("Primary decision: %s\n", decisionStr))
		}
	}

	systemPrompt := `You extract actionable follow-ups from the provided session content only.

RULES:
1. Each action item MUST be grounded in the context. Do not invent tasks or owners.
2. "description" is a short, imperative or concrete next step (what to do).
3. "owner" is optional. Set "owner" ONLY when the context explicitly names or assigns a person or role (e.g. "Alice will...", "Finance to..."). Otherwise use null. Never guess from job titles alone unless tied to a task.
4. Merge duplicate or near-duplicate items.
5. At most ` + fmt.Sprintf("%d", maxActionItemsPerResponse) + ` items.
6. If the content has no clear action items, or signal is too weak, set "low_signal" to true and use an empty "action_items" array.

Respond with ONLY valid JSON (no markdown fences) matching this object:
{"action_items":[{"description":"string","owner":null}],"low_signal":false}` + decisionSection.String()

	userPrompt := fmt.Sprintf("Session title: %s\n\n%s", sessionTitle, contextBuilder.String())

	baseURL := os.Getenv("OPENAI_BASE_URL")
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1/"
	}
	if len(baseURL) > 0 && baseURL[len(baseURL)-1] != '/' {
		baseURL += "/"
	}
	client := openai.NewClient(option.WithAPIKey(apiKey), option.WithBaseURL(baseURL))

	maxTok := int64(2048)
	params := openai.ChatCompletionNewParams{
		Model:     openai.ChatModelGPT4oMini,
		MaxTokens: openai.Int(maxTok),
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(systemPrompt),
			openai.UserMessage(userPrompt),
		},
	}
	rf := shared.NewResponseFormatJSONObjectParam()
	params.ResponseFormat = openai.ChatCompletionNewParamsResponseFormatUnion{OfJSONObject: &rf}

	response, err := client.Chat.Completions.New(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("openai chat: %w", err)
	}
	if len(response.Choices) == 0 {
		return nil, fmt.Errorf("no choices in OpenAI response")
	}

	content := strings.TrimSpace(response.Choices[0].Message.Content)
	if strings.HasPrefix(content, "```json") {
		content = strings.TrimPrefix(content, "```json")
		content = strings.TrimSuffix(content, "```")
		content = strings.TrimSpace(content)
	} else if strings.HasPrefix(content, "```") {
		content = strings.TrimPrefix(content, "```")
		content = strings.TrimSuffix(content, "```")
		content = strings.TrimSpace(content)
	}
	if extracted := extractFirstJSONObject(content); extracted != "" {
		content = extracted
	}

	var out ActionItemsExtraction
	if err := json.Unmarshal([]byte(content), &out); err != nil {
		return nil, fmt.Errorf("parse action items JSON: %w", err)
	}

	sanitizeActionItems(&out)
	return &out, nil
}

func sanitizeActionItems(out *ActionItemsExtraction) {
	if out == nil {
		return
	}
	if len(out.ActionItems) > maxActionItemsPerResponse {
		out.ActionItems = out.ActionItems[:maxActionItemsPerResponse]
	}
	dedup := make(map[string]struct{})
	kept := out.ActionItems[:0]
	for _, it := range out.ActionItems {
		desc := strings.TrimSpace(it.Description)
		if desc == "" {
			continue
		}
		if utf8.RuneCountInString(desc) > maxActionItemDescription {
			r := []rune(desc)
			desc = string(r[:maxActionItemDescription]) + "…"
		}
		it.Description = desc
		if it.Owner != nil {
			o := strings.TrimSpace(*it.Owner)
			if o == "" {
				it.Owner = nil
			} else if utf8.RuneCountInString(o) > maxActionItemOwner {
				r := []rune(o)
				o = string(r[:maxActionItemOwner]) + "…"
				it.Owner = &o
			} else {
				it.Owner = &o
			}
		}
		key := strings.ToLower(desc)
		if _, ok := dedup[key]; ok {
			continue
		}
		dedup[key] = struct{}{}
		kept = append(kept, it)
	}
	out.ActionItems = kept
	if len(out.ActionItems) == 0 {
		out.LowSignal = true
	}
}
