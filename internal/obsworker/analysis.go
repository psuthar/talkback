package obsworker

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/psuthar/talkback/internal/guardrails"
)

const maxAIContextChars = 3000
const maxAIResponseTokens = 500

// AIAnalysis holds the structured output from the LLM.
// DiagnosticIntents are converted to NRQL at render time via BuildQueriesFromIntents.
type AIAnalysis struct {
	Hypotheses        []string `json:"hypotheses"`
	LikelySubsystems  []string `json:"likely_subsystems"`
	SuggestedQueries  []string `json:"suggested_queries,omitempty"`  // legacy; prefer DiagnosticIntents
	DiagnosticIntents []string `json:"diagnostic_intents,omitempty"`
	ImmediateActions  []string `json:"immediate_actions"`
}

// LLMClient is the interface for calling an LLM (used for testing with mocks).
type LLMClient interface {
	Complete(ctx context.Context, prompt string, maxTokens int) (string, error)
}

// ExtractAIContext returns a trimmed string containing only Summary, Key Deltas, Unexpected Errors,
// Auth Failure Hotspots, and Automatic Deep Dive sections to keep the prompt under ~3KB.
func ExtractAIContext(b *Bundle) string {
	var sb strings.Builder
	if b.Delta != nil {
		d := *b.Delta
		sb.WriteString("## Summary\n\n")
		sb.WriteString(fmt.Sprintf("Status: %s | Confidence: %s\n", d.Status, d.Confidence))
		if len(d.Reasons) > 0 {
			sb.WriteString("Reasons: ")
			for i, r := range d.Reasons {
				if i > 0 {
					sb.WriteString("; ")
				}
				sb.WriteString(r)
			}
			sb.WriteString("\n\n")
		}
		sb.WriteString("## Key Deltas\n\n")
		for _, line := range DeltaSummaryLines(d) {
			sb.WriteString("- " + line + "\n")
		}
		sb.WriteString("\n## Unexpected Errors\n\n")
		if d.UnexpectedErrorSummary != "" {
			sb.WriteString("- " + d.UnexpectedErrorSummary + "\n")
		} else {
			sb.WriteString("- (none)\n")
		}
		sb.WriteString("\n## Auth Failure Hotspots\n\n")
		if len(d.AuthHotspots) > 0 {
			for _, h := range d.AuthHotspots {
				sb.WriteString(fmt.Sprintf("- %s: %d\n", h.Username, h.Count))
			}
		} else {
			sb.WriteString("- (none)\n")
		}
	}
	// Errors by endpoint URI (evidence for localized incidents)
	if rows := getQueryResults(b, "errors_by_endpoint_uri"); len(rows) > 0 {
		sb.WriteString("\n## Errors By Endpoint Uri\n\n")
		for i, row := range rows {
			if i >= 10 {
				sb.WriteString("...\n")
				break
			}
			sb.WriteString(formatRowForAI(row) + "\n")
		}
	}
	// Errors by endpoint / transaction (from base or deep dive)
	if rows := getQueryResults(b, "errors_by_endpoint"); len(rows) > 0 {
		sb.WriteString("\n## Errors By Txn\n\n")
		for i, row := range rows {
			if i >= 10 {
				sb.WriteString("...\n")
				break
			}
			sb.WriteString(formatRowForAI(row) + "\n")
		}
	} else if len(b.DeepDiveResults) > 0 {
		for _, qr := range b.DeepDiveResults {
			if qr.Name == "errors_by_txn" && len(qr.Results) > 0 {
				sb.WriteString("\n## Errors By Txn\n\n")
				for i, row := range qr.Results {
					if i >= 10 {
						sb.WriteString("...\n")
						break
					}
					sb.WriteString(formatRowForAI(row) + "\n")
				}
				break
			}
		}
	}
	// Top errors (class/message)
	if rows := getQueryResults(b, "top_errors"); len(rows) > 0 {
		sb.WriteString("\n## Top Errors\n\n")
		for i, row := range rows {
			if i >= 10 {
				sb.WriteString("...\n")
				break
			}
			sb.WriteString(formatRowForAI(row) + "\n")
		}
	}
	// Deep dive: only throughput and latency by txn (curated)
	if len(b.DeepDiveResults) > 0 {
		sb.WriteString("\n## Automatic Deep Dive\n\n")
		for _, qr := range b.DeepDiveResults {
			if qr.Name != "throughput_by_txn" && qr.Name != "latency_by_txn_p95" {
				continue
			}
			sb.WriteString("### " + qr.Name + "\n")
			if len(qr.Results) > 0 {
				for i, row := range qr.Results {
					if i >= 5 {
						sb.WriteString("...\n")
						break
					}
					sb.WriteString(formatRowForAI(row) + "\n")
				}
			} else {
				sb.WriteString("(no rows)\n")
			}
			sb.WriteString("\n")
		}
	}
	s := sb.String()
	if len(s) > maxAIContextChars {
		s = s[:maxAIContextChars] + "\n...(truncated)"
	}
	return s
}

// LoadAIPrompt loads the prompt template from ops/observability/ai_prompt.txt and replaces {{bundle_markdown}}.
// If OBS_AI_PROMPT_PATH is set (e.g. in tests), that file is used instead.
func LoadAIPrompt(bundleContext string) (string, error) {
	path := strings.TrimSpace(os.Getenv("OBS_AI_PROMPT_PATH"))
	if path == "" {
		path = "ops/observability/ai_prompt.txt"
	}
	if _, err := os.Stat(path); err != nil {
		if exe, err := os.Executable(); err == nil {
			alt := filepath.Join(filepath.Dir(exe), "..", "..", path)
			if _, err := os.Stat(alt); err == nil {
				path = alt
			}
		}
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read AI prompt: %w", err)
	}
	return strings.ReplaceAll(string(raw), "{{bundle_markdown}}", bundleContext), nil
}

// GenerateAIAnalysis calls the LLM with the bundle context and returns parsed AIAnalysis.
// If client is nil, a client is created from env (OBS_AI_PROVIDER, OBS_AI_API_KEY, etc.).
// Bundle generation must succeed even if AI fails: on error, returns (nil, error) and the caller should not fail the pipeline.
func GenerateAIAnalysis(ctx context.Context, bundle Bundle, client LLMClient) (*AIAnalysis, error) {
	if client == nil {
		c, err := NewLLMClientFromEnv()
		if err != nil {
			return nil, err
		}
		client = c
	}
	incidentSummary := ComputeIncidentSummary(&bundle)
	bundleContext := ExtractAIContext(&bundle)
	contextStr := FormatIncidentSummaryForAI(incidentSummary) + "\n\n" + bundleContext
	if len(contextStr) > maxAIContextChars {
		contextStr = contextStr[:maxAIContextChars] + "\n...(truncated)"
	}
	prompt, err := LoadAIPrompt(contextStr)
	if err != nil {
		return nil, err
	}
	llmStart := time.Now()
	response, err := client.Complete(ctx, prompt, maxAIResponseTokens)
	if err != nil {
		return nil, err
	}

	// SCRUM-568: log this LLM round. Token counts not available on the
	// LLMClient interface (Complete returns just the string + error), so
	// input_tokens / output_tokens stay nil here — log-shape.md marks them
	// nullable for exactly this case.
	guardrails.LogLLMCall(ctx, guardrails.LLMCallRow{
		Site:       "obsworker",
		Model:      os.Getenv("OBS_AI_MODEL"),
		PromptHash: guardrails.HashPrompt("obsworker", prompt),
		LatencyMS:  int(time.Since(llmStart).Milliseconds()),
		Decision:   "allowed",
	})
	out, err := parseAIAnalysisResponse(response)
	if err != nil {
		return nil, err
	}
	out.LikelySubsystems = SanitizeLikelySubsystems(incidentSummary, out.LikelySubsystems)
	return out, nil
}

// parseAIAnalysisResponse extracts JSON from the response and unmarshals into AIAnalysis.
// Handles malformed responses (e.g. markdown code blocks or extra text).
func parseAIAnalysisResponse(response string) (*AIAnalysis, error) {
	response = strings.TrimSpace(response)
	// Strip markdown code block if present
	if idx := strings.Index(response, "```"); idx >= 0 {
		response = strings.TrimPrefix(response[idx+3:], "json")
		response = strings.TrimSpace(response)
		if end := strings.Index(response, "```"); end >= 0 {
			response = response[:end]
		}
		response = strings.TrimSpace(response)
	}
	var out AIAnalysis
	if err := json.Unmarshal([]byte(response), &out); err != nil {
		return nil, fmt.Errorf("parse AI response JSON: %w", err)
	}
	return &out, nil
}

func formatRowForAI(row map[string]interface{}) string {
	var parts []string
	for k, v := range row {
		parts = append(parts, fmt.Sprintf("%s=%v", k, v))
	}
	return strings.Join(parts, ", ")
}
