package obsworker

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const llmTimeout = 10 * time.Second

// AIConfig holds env-derived settings for the LLM (optional AI analysis).
type AIConfig struct {
	Enabled  bool   // OBS_ENABLE_AI_ANALYSIS=true
	Provider string // openai | anthropic
	APIKey   string
	Model    string
}

// LoadAIConfig reads AI-related env vars. Does not require API key if AI is disabled.
func LoadAIConfig() AIConfig {
	enabled := isTruthy(os.Getenv("OBS_ENABLE_AI_ANALYSIS"))
	provider := strings.TrimSpace(strings.ToLower(os.Getenv("OBS_AI_PROVIDER")))
	if provider == "" {
		provider = "openai"
	}
	apiKey := strings.TrimSpace(os.Getenv("OBS_AI_API_KEY"))
	model := strings.TrimSpace(os.Getenv("OBS_AI_MODEL"))
	if model == "" {
		switch provider {
		case "anthropic":
			model = "claude-3-haiku-20240307"
		default:
			model = "gpt-4o-mini"
		}
	}
	return AIConfig{
		Enabled:  enabled,
		Provider: provider,
		APIKey:   apiKey,
		Model:    model,
	}
}

// NewLLMClientFromEnv creates an LLM client from env (OBS_AI_PROVIDER, OBS_AI_API_KEY, OBS_AI_MODEL).
func NewLLMClientFromEnv() (LLMClient, error) {
	cfg := LoadAIConfig()
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("OBS_AI_API_KEY is required when AI analysis is enabled")
	}
	switch cfg.Provider {
	case "openai":
		return &openAIClient{apiKey: cfg.APIKey, model: cfg.Model, client: &http.Client{Timeout: llmTimeout}}, nil
	case "anthropic":
		return &anthropicClient{apiKey: cfg.APIKey, model: cfg.Model, client: &http.Client{Timeout: llmTimeout}}, nil
	default:
		return nil, fmt.Errorf("OBS_AI_PROVIDER must be openai or anthropic, got %q", cfg.Provider)
	}
}

// openAIClient implements LLMClient for OpenAI Chat Completions.
type openAIClient struct {
	apiKey string
	model  string
	client *http.Client
}

type openAIRequest struct {
	Model    string        `json:"model"`
	Messages []openAIMsg   `json:"messages"`
	MaxTokens int          `json:"max_tokens"`
}

type openAIMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

func (c *openAIClient) Complete(ctx context.Context, prompt string, maxTokens int) (string, error) {
	reqBody := openAIRequest{
		Model:    c.model,
		Messages: []openAIMsg{{Role: "user", Content: prompt}},
		MaxTokens: maxTokens,
	}
	body, _ := json.Marshal(reqBody)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.openai.com/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	resp, err := c.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 500))
		return "", fmt.Errorf("OpenAI API %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	var out openAIResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if len(out.Choices) == 0 {
		return "", fmt.Errorf("OpenAI API returned no choices")
	}
	return strings.TrimSpace(out.Choices[0].Message.Content), nil
}

// anthropicClient implements LLMClient for Anthropic Messages API.
type anthropicClient struct {
	apiKey string
	model  string
	client *http.Client
}

type anthropicRequest struct {
	Model     string          `json:"model"`
	MaxTokens int             `json:"max_tokens"`
	Messages  []anthropicMsg  `json:"messages"`
}

type anthropicMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
}

func (c *anthropicClient) Complete(ctx context.Context, prompt string, maxTokens int) (string, error) {
	reqBody := anthropicRequest{
		Model:     c.model,
		MaxTokens: maxTokens,
		Messages:  []anthropicMsg{{Role: "user", Content: prompt}},
	}
	body, _ := json.Marshal(reqBody)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.anthropic.com/v1/messages", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", c.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	resp, err := c.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 500))
		return "", fmt.Errorf("Anthropic API %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	var out anthropicResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	var text strings.Builder
	for _, block := range out.Content {
		if block.Type == "text" {
			text.WriteString(block.Text)
		}
	}
	return strings.TrimSpace(text.String()), nil
}
