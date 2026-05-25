package utils

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/psuthar/talkback/internal/guardrails"
)

// SCRUM-567 (Slice 4c of SCRUM-560): integration tests for the
// action_items schema-validation retry-then-drop path. The judge in
// SCRUM-566 was tested similarly via a mocked OpenAI httptest server;
// the same pattern applies here.
//
// The retry differentiator is the schema-retry addendum text — the
// stricter prompt only fires on the second LLM call. We assert the
// LogLLMCall sequence + final ActionItemsExtraction shape.

// fakeAIGuardrailsWriter is the action_items local fake — same as
// fakeQAGuardrailsWriter in qa_citation_integration_test.go but
// distinct to avoid cross-test interference.
type fakeAIGuardrailsWriter struct {
	mu   sync.Mutex
	rows []guardrails.LLMCallRow
}

func (f *fakeAIGuardrailsWriter) InsertLLMCallRow(_ context.Context, row guardrails.LLMCallRow) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rows = append(f.rows, row)
	return nil
}

func (f *fakeAIGuardrailsWriter) Rows() []guardrails.LLMCallRow {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]guardrails.LLMCallRow, len(f.rows))
	copy(out, f.rows)
	return out
}

func makeChatCompletionForAI(content string) map[string]any {
	return map[string]any{
		"id":      "chatcmpl-test",
		"object":  "chat.completion",
		"created": 0,
		"model":   "gpt-4o-mini",
		"choices": []map[string]any{
			{
				"index":         0,
				"message":       map[string]any{"role": "assistant", "content": content},
				"finish_reason": "stop",
			},
		},
		"usage": map[string]any{"prompt_tokens": 80, "completion_tokens": 30, "total_tokens": 110},
	}
}

// TestExtractActionItems_SchemaValidation_RetryThenDrop drives two
// malformed-JSON rounds. The second failure should DROP the record
// (return empty + low_signal) — never crash.
func TestExtractActionItems_SchemaValidation_RetryThenDrop(t *testing.T) {
	fake := &fakeAIGuardrailsWriter{}
	guardrails.ResetForTest()
	guardrails.Init(fake)
	t.Cleanup(func() { guardrails.Init(nil); guardrails.ResetForTest() })

	var callCount int
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		callCount++
		mu.Unlock()
		// Always return malformed JSON for the action_items schema.
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(makeChatCompletionForAI("not json at all"))
	}))
	defer srv.Close()
	t.Setenv("OPENAI_API_KEY", "fake-key")
	t.Setenv("OPENAI_BASE_URL", srv.URL+"/v1/")

	chunks := []Chunk{{ChunkID: "c1", SourceType: "transcript", Text: "Alex will draft the proposal by Friday."}}
	got, err := ExtractActionItemsFromContext(context.Background(), chunks, "Test session", SessionContext{})
	if err != nil {
		t.Fatalf("ExtractActionItemsFromContext should not error on schema retry-then-drop; got %v", err)
	}
	if got == nil {
		t.Fatalf("expected non-nil empty/low_signal extraction; got nil")
	}
	if len(got.ActionItems) != 0 {
		t.Errorf("expected empty action_items on retry-then-drop; got %d", len(got.ActionItems))
	}
	if !got.LowSignal {
		t.Errorf("expected LowSignal=true on retry-then-drop")
	}
	if callCount != 2 {
		t.Errorf("expected 2 LLM calls (initial + schema retry), got %d", callCount)
	}

	guardrails.FlushNow(context.Background())
	rows := fake.Rows()
	// Sequence: action_items(allowed, parse failed) + action_items_retry_schema(allowed, parse failed again) + action_items(allowed, schema_validation_failed flag, drop)
	if len(rows) != 3 {
		t.Fatalf("expected 3 LogLLMCall rows, got %d:\n%s", len(rows), rowSummariesAI(rows))
	}
	if rows[0].Site != "action_items" {
		t.Errorf("row 0 site=%q, want action_items", rows[0].Site)
	}
	if rows[1].Site != "action_items_retry_schema" {
		t.Errorf("row 1 site=%q, want action_items_retry_schema", rows[1].Site)
	}
	if rows[2].Site != "action_items" || rows[2].Decision != "allowed" {
		t.Errorf("row 2: site=%q decision=%q, want action_items/allowed", rows[2].Site, rows[2].Decision)
	}
	flagFound := false
	for _, g := range rows[2].GuardrailsFired {
		if g == "schema_validation_failed" {
			flagFound = true
		}
	}
	if !flagFound {
		t.Errorf("row 2 GuardrailsFired should include schema_validation_failed; got %v",
			rows[2].GuardrailsFired)
	}
}

// TestExtractActionItems_SchemaValidation_RecoversOnRetry drives a
// malformed-JSON first round + a valid retry. Must return the retry
// payload, no schema_validation_failed flag.
func TestExtractActionItems_SchemaValidation_RecoversOnRetry(t *testing.T) {
	fake := &fakeAIGuardrailsWriter{}
	guardrails.ResetForTest()
	guardrails.Init(fake)
	t.Cleanup(func() { guardrails.Init(nil); guardrails.ResetForTest() })

	validResponse := `{"action_items":[{"description":"Draft the proposal","owner":"Alex"}],"low_signal":false}`
	var callCount int
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		callCount++
		current := callCount
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if current == 1 {
			_ = json.NewEncoder(w).Encode(makeChatCompletionForAI("malformed first round"))
			return
		}
		_ = json.NewEncoder(w).Encode(makeChatCompletionForAI(validResponse))
	}))
	defer srv.Close()
	t.Setenv("OPENAI_API_KEY", "fake-key")
	t.Setenv("OPENAI_BASE_URL", srv.URL+"/v1/")

	chunks := []Chunk{{ChunkID: "c1", SourceType: "transcript", Text: "Alex agreed to draft."}}
	got, err := ExtractActionItemsFromContext(context.Background(), chunks, "Test", SessionContext{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.ActionItems) != 1 {
		t.Fatalf("expected 1 action item from retry; got %d", len(got.ActionItems))
	}
	if got.ActionItems[0].Description != "Draft the proposal" {
		t.Errorf("description=%q, want 'Draft the proposal'", got.ActionItems[0].Description)
	}
	if got.LowSignal {
		t.Errorf("LowSignal should be false on successful retry")
	}
	if callCount != 2 {
		t.Errorf("expected 2 LLM calls, got %d", callCount)
	}

	guardrails.FlushNow(context.Background())
	rows := fake.Rows()
	if len(rows) != 2 {
		t.Fatalf("expected 2 LogLLMCall rows, got %d:\n%s", len(rows), rowSummariesAI(rows))
	}
	if rows[1].Site != "action_items_retry_schema" {
		t.Errorf("row 1 site=%q, want action_items_retry_schema", rows[1].Site)
	}
	// No schema_validation_failed flag should be set since retry succeeded.
	for _, g := range rows[1].GuardrailsFired {
		if g == "schema_validation_failed" {
			t.Errorf("row 1 should not have schema_validation_failed flag on successful retry")
		}
	}
}

// TestExtractActionItems_HappyPath asserts the no-retry control flow
// hasn't regressed.
func TestExtractActionItems_HappyPath(t *testing.T) {
	fake := &fakeAIGuardrailsWriter{}
	guardrails.ResetForTest()
	guardrails.Init(fake)
	t.Cleanup(func() { guardrails.Init(nil); guardrails.ResetForTest() })

	validResponse := `{"action_items":[{"description":"Task A","owner":"Alex"}],"low_signal":false}`
	var callCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(makeChatCompletionForAI(validResponse))
	}))
	defer srv.Close()
	t.Setenv("OPENAI_API_KEY", "fake-key")
	t.Setenv("OPENAI_BASE_URL", srv.URL+"/v1/")

	chunks := []Chunk{{ChunkID: "c1", Text: "Alex will do Task A."}}
	got, err := ExtractActionItemsFromContext(context.Background(), chunks, "T", SessionContext{})
	if err != nil {
		t.Fatalf("ExtractActionItemsFromContext: %v", err)
	}
	if len(got.ActionItems) != 1 {
		t.Errorf("want 1 item, got %d", len(got.ActionItems))
	}
	if callCount != 1 {
		t.Errorf("expected exactly 1 LLM call (no retry on happy path), got %d", callCount)
	}
	guardrails.FlushNow(context.Background())
	rows := fake.Rows()
	if len(rows) != 1 {
		t.Errorf("expected 1 LogLLMCall row, got %d:\n%s", len(rows), rowSummariesAI(rows))
	}
}

func rowSummariesAI(rows []guardrails.LLMCallRow) string {
	var b strings.Builder
	for i, r := range rows {
		b.WriteString("  ")
		b.WriteString(itoaAI(i))
		b.WriteString(": site=")
		b.WriteString(r.Site)
		b.WriteString(" decision=")
		b.WriteString(r.Decision)
		b.WriteString(" guardrails=")
		b.WriteString(strings.Join(r.GuardrailsFired, ","))
		b.WriteString("\n")
	}
	return b.String()
}

func itoaAI(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
