package utils

import (
	"context"
	"os"
	"regexp"
	"strings"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
)

// collapseRepeatedWords removes consecutive duplicate words (e.g. "the the point" -> "the point").
func collapseRepeatedWords(s string) string {
	words := strings.Fields(s)
	if len(words) <= 1 {
		return s
	}
	var out []string
	for i := 0; i < len(words); i++ {
		out = append(out, words[i])
		for i+1 < len(words) && strings.EqualFold(words[i], words[i+1]) {
			i++
		}
	}
	return strings.Join(out, " ")
}

// PolishSpokenQuestion cleans transcribed speech into a crisp question: removes fillers (um, uh, ah, like),
// collapses repeated words/phrases, and trims whitespace. Rule-based; no LLM.
func PolishSpokenQuestion(raw string) string {
	if raw == "" {
		return ""
	}
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}

	// Common fillers and false starts (word boundaries so we don't strip "um" from "instrument")
	// Match as whole words; order matters for "kind of like" etc.
	fillerPattern := regexp.MustCompile(`(?i)\b(um+|uh+|ah+|er+|hm+|like|you know|i mean|kind of|sort of|basically|actually|literally)\s*`)
	s = fillerPattern.ReplaceAllString(s, " ")

	// Collapse repeated consecutive words (e.g. "the the point" -> "the point")
	s = collapseRepeatedWords(s)

	// Collapse multiple spaces and trim
	spaces := regexp.MustCompile(`\s+`)
	s = spaces.ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

// PolishSpokenQuestionWithLLM rewrites the spoken text into one clear, concise question using an LLM.
// Uses the same OpenAI client pattern as qa.go. Returns the polished text or error.
func PolishSpokenQuestionWithLLM(ctx context.Context, raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		return raw, nil // fallback to unchanged if no key
	}
	baseURL := os.Getenv("OPENAI_BASE_URL")
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1/"
	}
	if len(baseURL) > 0 && baseURL[len(baseURL)-1] != '/' {
		baseURL += "/"
	}
	client := openai.NewClient(option.WithAPIKey(apiKey), option.WithBaseURL(baseURL))

	systemPrompt := "You are a helpful assistant. Rewrite the user's spoken question into one clear, concise written question. Remove fillers (um, uh, like), false starts, and repetition. Keep the exact meaning. Output only the rewritten question, no explanation."
	params := openai.ChatCompletionNewParams{
		Model: openai.ChatModelGPT4oMini,
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(systemPrompt),
			openai.UserMessage(raw),
		},
	}
	response, err := client.Chat.Completions.New(ctx, params)
	if err != nil {
		return "", err
	}
	if len(response.Choices) == 0 {
		return raw, nil
	}
	out := strings.TrimSpace(response.Choices[0].Message.Content)
	if out == "" {
		return raw, nil
	}
	return out, nil
}
