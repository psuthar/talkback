package obsworker

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractAIContext_ReturnsTrimmedBundleText(t *testing.T) {
	b := &Bundle{
		Delta: &Delta{
			Status:     "RED",
			Confidence: "high",
			Reasons:    []string{"error_rate_high", "latency_spike"},
		},
		DeepDiveResults: []QueryResult{
			{Name: "Errors", NRQL: "SELECT count(*) FROM Transaction", Results: []map[string]interface{}{
				{"count": float64(100)},
			}},
		},
	}
	out := ExtractAIContext(b)
	out = strings.TrimSpace(out)
	if out == "" {
		t.Fatal("ExtractAIContext returned empty string")
	}
	if !strings.Contains(out, "## Summary") {
		t.Error("expected ## Summary in output")
	}
	if !strings.Contains(out, "## Key Deltas") {
		t.Error("expected ## Key Deltas in output")
	}
	if !strings.Contains(out, "## Automatic Deep Dive") {
		t.Error("expected ## Automatic Deep Dive in output")
	}
	if len(out) > maxAIContextChars+50 {
		t.Errorf("output should be capped near %d chars, got len=%d", maxAIContextChars, len(out))
	}
}

func TestAIContext_ContainsIncidentSummaryAndDominantTransaction(t *testing.T) {
	// Build the same context string as GenerateAIAnalysis for a localized-incident bundle.
	b := &Bundle{
		Summary: &BundleSummary{Status: "RED", Confidence: "HIGH"},
		Delta: &Delta{
			Status:          "RED",
			Current:         BaselineMetrics{Errors: 8, P95ms: 0.1, ReqPerMin: 15},
			ExpectedAuth401: 0,
		},
		Results: []QueryResult{
			{
				Name: "errors_by_endpoint",
				Results: []map[string]interface{}{
					{"facet": "WebTransaction/Go/POST /debug/fault/error", "count": float64(8)},
				},
			},
		},
	}
	incidentSummary := ComputeIncidentSummary(b)
	bundleContext := ExtractAIContext(b)
	contextStr := FormatIncidentSummaryForAI(incidentSummary) + "\n\n" + bundleContext
	if !strings.Contains(contextStr, "likely_isolated_incident") {
		t.Error("expected context to contain likely_isolated_incident (from incident summary)")
	}
	dominantTxn := "WebTransaction/Go/POST /debug/fault/error"
	if !strings.Contains(contextStr, dominantTxn) {
		t.Errorf("expected context to contain dominant_transaction %q", dominantTxn)
	}
	if !strings.Contains(contextStr, "Incident summary (deterministic)") {
		t.Error("expected context to contain Incident summary (deterministic) header")
	}
}

func TestExtractAIContext_ExcludesNoisySections(t *testing.T) {
	// Only curated sections (Summary, Key Deltas, Errors by endpoint/uri, Top Errors, throughput/latency deep dive) are included.
	// Discover Appnames and other unrelated deep-dive sections must not appear.
	b := &Bundle{
		Delta: &Delta{Status: "RED", Confidence: "HIGH"},
		DeepDiveResults: []QueryResult{
			{Name: "discover_appnames", Results: []map[string]interface{}{{"appName": "Foo"}}},
			{Name: "throughput_by_txn", Results: []map[string]interface{}{{"name": "T1", "count": float64(10)}}},
		},
	}
	out := ExtractAIContext(b)
	if strings.Contains(out, "discover_appnames") || strings.Contains(out, "Discover Appnames") {
		t.Error("expected ExtractAIContext to exclude discover_appnames / Discover Appnames")
	}
	if !strings.Contains(out, "throughput_by_txn") {
		t.Error("expected curated deep-dive section throughput_by_txn to be included")
	}
}

func TestRenderMarkdown_IncidentShapeAndFollowUpDiagnostics(t *testing.T) {
	b := &Bundle{
		Summary:  &BundleSummary{Status: "RED"},
		Delta:    &Delta{Status: "RED", Current: BaselineMetrics{Errors: 5}},
		Metadata: BundleMetadata{AppName: "TestApp", WindowMins: 30},
		Results: []QueryResult{
			{Name: "errors_by_endpoint", Results: []map[string]interface{}{
				{"facet": "WebTransaction/Go/POST /debug/fault/error", "count": float64(5)},
			}},
		},
		Analysis: &AIAnalysis{
			Hypotheses:       []string{"Fault injection route."},
			DiagnosticIntents: []string{"inspect_failing_transaction_by_name", "inspect_top_error_messages"},
			ImmediateActions:  []string{"Verify if debug traffic is intentional."},
		},
	}
	md := b.RenderMarkdown()
	if !strings.Contains(md, "### Incident Shape") {
		t.Error("expected ### Incident Shape in AI section")
	}
	if !strings.Contains(md, "### Follow-up Diagnostics") {
		t.Error("expected ### Follow-up Diagnostics in AI section")
	}
	if !strings.Contains(md, "intent: inspect_failing_transaction_by_name") {
		t.Error("expected intent label in Follow-up Diagnostics")
	}
	// Deterministic NRQL should appear in blockquote
	if !strings.Contains(md, "> `") {
		t.Error("expected blockquoted NRQL (e.g. > `...`) in Follow-up Diagnostics")
	}
}

func TestRenderMarkdown_OneDiagnosticsSection_NoStaticQueries(t *testing.T) {
	b := &Bundle{
		Delta:    &Delta{Status: "RED", Current: BaselineMetrics{Errors: 3}},
		Metadata: BundleMetadata{WindowMins: 30},
	}
	md := b.RenderMarkdown()
	if strings.Contains(md, "Recommended Next Queries") {
		t.Error("bundle must not contain static Recommended Next Queries section")
	}
	if !strings.Contains(md, "## Incident note") {
		t.Error("bundle must contain deterministic Incident note section")
	}
	if !strings.Contains(md, "## Likely code area") {
		t.Error("bundle must contain Likely code area section")
	}
}

func TestGenerateAIAnalysis_ValidJSON(t *testing.T) {
	// Use temp prompt file so test works from any cwd
	dir := t.TempDir()
	promptPath := filepath.Join(dir, "ai_prompt.txt")
	if err := os.WriteFile(promptPath, []byte("{{bundle_markdown}}"), 0600); err != nil {
		t.Fatal(err)
	}
	os.Setenv("OBS_AI_PROMPT_PATH", promptPath)
	defer os.Unsetenv("OBS_AI_PROMPT_PATH")

	validJSON := `{"hypotheses":["h1"],"likely_subsystems":["s1"],"suggested_queries":["SELECT 1"],"immediate_actions":["scale up"]}`
	mock := &mockLLM{response: validJSON}
	bundle := Bundle{Delta: &Delta{Status: "RED"}}
	ctx := context.Background()
	got, err := GenerateAIAnalysis(ctx, bundle, mock)
	if err != nil {
		t.Fatalf("GenerateAIAnalysis: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil AIAnalysis")
	}
	if len(got.Hypotheses) != 1 || got.Hypotheses[0] != "h1" {
		t.Errorf("hypotheses: got %v", got.Hypotheses)
	}
	if len(got.LikelySubsystems) != 1 || got.LikelySubsystems[0] != "s1" {
		t.Errorf("likely_subsystems: got %v", got.LikelySubsystems)
	}
	if len(got.ImmediateActions) != 1 || got.ImmediateActions[0] != "scale up" {
		t.Errorf("immediate_actions: got %v", got.ImmediateActions)
	}
}

func TestGenerateAIAnalysis_MalformedJSON(t *testing.T) {
	dir := t.TempDir()
	promptPath := filepath.Join(dir, "ai_prompt.txt")
	if err := os.WriteFile(promptPath, []byte("{{bundle_markdown}}"), 0600); err != nil {
		t.Fatal(err)
	}
	os.Setenv("OBS_AI_PROMPT_PATH", promptPath)
	defer os.Unsetenv("OBS_AI_PROMPT_PATH")

	mock := &mockLLM{response: "not valid json at all"}
	bundle := Bundle{Delta: &Delta{Status: "RED"}}
	ctx := context.Background()
	got, err := GenerateAIAnalysis(ctx, bundle, mock)
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
	if got != nil {
		t.Errorf("expected nil analysis on error, got %v", got)
	}
}

func TestGenerateAIAnalysis_StripsMarkdownCodeBlock(t *testing.T) {
	dir := t.TempDir()
	promptPath := filepath.Join(dir, "ai_prompt.txt")
	if err := os.WriteFile(promptPath, []byte("{{bundle_markdown}}"), 0600); err != nil {
		t.Fatal(err)
	}
	os.Setenv("OBS_AI_PROMPT_PATH", promptPath)
	defer os.Unsetenv("OBS_AI_PROMPT_PATH")

	validJSON := `{"hypotheses":["h1"],"likely_subsystems":[],"suggested_queries":[],"immediate_actions":[]}`
	mock := &mockLLM{response: "```json\n" + validJSON + "\n```"}
	bundle := Bundle{Delta: &Delta{Status: "YELLOW"}}
	ctx := context.Background()
	got, err := GenerateAIAnalysis(ctx, bundle, mock)
	if err != nil {
		t.Fatalf("GenerateAIAnalysis: %v", err)
	}
	if got == nil || len(got.Hypotheses) != 1 || got.Hypotheses[0] != "h1" {
		t.Errorf("expected parsed analysis with one hypothesis, got %v", got)
	}
}

func TestSimulationBundlesIncludeSimulationWarning(t *testing.T) {
	b := &Bundle{
		Summary:    &BundleSummary{Status: "RED"},
		Simulation: &Simulation{Enabled: true, ForcedStatus: "RED", Reason: "test"},
		Analysis: &AIAnalysis{
			Hypotheses:       []string{"test hypothesis"},
			LikelySubsystems: []string{"api"},
			SuggestedQueries: []string{"SELECT 1"},
			ImmediateActions: []string{"check logs"},
		},
	}
	md := b.RenderMarkdown()
	if !strings.Contains(md, "AI ANALYSIS GENERATED UNDER SIMULATION") {
		t.Error("expected simulation warning banner in markdown when Simulation.Enabled and Analysis set")
	}
	if !strings.Contains(md, "🤖 AI Incident Analysis") {
		t.Error("expected AI Incident Analysis section in markdown")
	}
}

func TestAIDisabledWhenEnvFlagNotSet(t *testing.T) {
	// Ensure AI is disabled when OBS_ENABLE_AI_ANALYSIS is not true
	os.Setenv("OBS_ENABLE_AI_ANALYSIS", "false")
	defer os.Unsetenv("OBS_ENABLE_AI_ANALYSIS")
	cfg := LoadAIConfig()
	if cfg.Enabled {
		t.Error("expected Enabled false when OBS_ENABLE_AI_ANALYSIS=false")
	}
}

func TestAIDisabledWhenEnvUnset(t *testing.T) {
	os.Unsetenv("OBS_ENABLE_AI_ANALYSIS")
	cfg := LoadAIConfig()
	if cfg.Enabled {
		t.Error("expected Enabled false when OBS_ENABLE_AI_ANALYSIS is unset")
	}
}

type mockLLM struct {
	response string
	err      error
}

func (m *mockLLM) Complete(ctx context.Context, prompt string, maxTokens int) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	return m.response, nil
}
