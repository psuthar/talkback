package utils

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/guardrails"
)

// SCRUM-566 (Slice 4b of SCRUM-560): integration test for the
// grounding LLM-as-judge wired into GenerateAnswer. Stubs both the QA
// call and the judge call against the same OPENAI_BASE_URL httptest
// server — they're differentiated by the request body (system prompt
// contents). Verifies:
//
//  1. Judge-refused answer triggers one retry with the grounding
//     addendum; second judge refusal produces a grounding_failed
//     refusal with the right LogLLMCall sequence.
//  2. Judge-allowed answer flows through unchanged (no retry).
//  3. Per-user rate-limit path skips the judge call entirely; the
//     citation-enforced answer is still returned; the log row stamps
//     guardrails_fired=[grounding_judge_rate_limited].

// fakeJudgeCounterForUtils mirrors fakeJudgeCounter in grounding_test
// because that one is package-scoped. Kept local to the utils package.
type fakeJudgeCounterForUtils struct {
	mu       sync.Mutex
	hitCount int
}

func (f *fakeJudgeCounterForUtils) CountLLMCallsBySiteAndUserSince(_ context.Context, _ string, _ uuid.UUID, _ time.Time) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.hitCount, nil
}

// routeQAOrJudge differentiates the two call types by checking whether
// the system prompt contains the grounding judge directive ("grounding
// judge") or one of the QA system markers.
func routeQAOrJudge(body []byte) string {
	// Cheap heuristic — judge prompt mentions "grounding judge"; QA
	// prompt mentions "strict context-only assistant".
	s := string(body)
	if strings.Contains(s, "strict grounding judge") {
		return "judge"
	}
	return "qa"
}

func makeChatCompletionForGrounding(content string) map[string]any {
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
		"usage": map[string]any{"prompt_tokens": 100, "completion_tokens": 50, "total_tokens": 150},
	}
}

// TestGenerateAnswer_GroundingGuardrail_RetryThenRefuse drives two
// rounds of judge-refused responses and asserts the final refusal +
// LogLLMCall sequence.
func TestGenerateAnswer_GroundingGuardrail_RetryThenRefuse(t *testing.T) {
	fake := &fakeQAGuardrailsWriter{}
	guardrails.ResetForTest()
	guardrails.Init(fake)
	t.Cleanup(func() { guardrails.Init(nil); guardrails.ResetForTest() })

	// Disable the per-user quota for this test by stubbing the counter
	// with hitCount=0 and ensuring the max env is set high enough.
	t.Setenv("GUARDRAIL_JUDGE_MAX_PER_USER_PER_HOUR", "100")
	guardrails.SetDefaultJudgeQuotaCounter(&fakeJudgeCounterForUtils{hitCount: 0})
	t.Cleanup(func() { guardrails.SetDefaultJudgeQuotaCounter(nil) })

	// Citation matches retrieved chunk_id, so CheckCitations passes
	// and the judge is invoked.
	groundedCitation := `{"answer_status":"answered","answer_text":"$2.4M was approved.","confidence":0.9,"citations":[{"chunk_id":"real-A","source_type":"transcript","source_id":"s1","locator":"","snippet":"$2.4M proposed"}]}`
	judgeRefuse := `{"grounded":false,"rationale":"$2.4M was proposed, not approved"}`

	var (
		qaCalls    int
		judgeCalls int
		mu         sync.Mutex
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, 4096)
		n, _ := r.Body.Read(body)
		kind := routeQAOrJudge(body[:n])
		mu.Lock()
		switch kind {
		case "judge":
			judgeCalls++
		default:
			qaCalls++
		}
		mu.Unlock()

		w.Header().Set("Content-Type", "application/json")
		if kind == "judge" {
			_ = json.NewEncoder(w).Encode(makeChatCompletionForGrounding(judgeRefuse))
			return
		}
		_ = json.NewEncoder(w).Encode(makeChatCompletionForGrounding(groundedCitation))
	}))
	defer srv.Close()
	t.Setenv("OPENAI_API_KEY", "fake-key")
	t.Setenv("OPENAI_BASE_URL", srv.URL+"/v1/")

	// Stamp a user_id on ctx so CheckJudgeQuota runs (still allows
	// because the fake counter returns 0).
	ctx := guardrails.WithUserID(context.Background(), uuid.New())

	chunks := []Chunk{{ChunkID: "real-A", SourceType: "transcript", SourceID: "s1", Text: "$2.4M proposed"}}
	qa, _, err := GenerateAnswer(ctx, "Q?", chunks, "T", SessionContext{}, nil)
	if err != nil {
		t.Fatalf("GenerateAnswer: %v", err)
	}

	if qa.Refusal == nil {
		t.Fatalf("expected grounding_failed refusal, got Refusal=nil; answer=%s", qa.AnswerText)
	}
	if qa.Refusal.Code != "grounding_failed" {
		t.Errorf("Refusal.Code=%q, want grounding_failed", qa.Refusal.Code)
	}
	if qa.Refusal.UserMessage != guardrails.UserMessageGroundingFailed {
		t.Errorf("Refusal.UserMessage=%q does not match contract", qa.Refusal.UserMessage)
	}
	if qaCalls != 2 {
		t.Errorf("expected 2 QA LLM calls (initial + retry), got %d", qaCalls)
	}
	if judgeCalls != 2 {
		t.Errorf("expected 2 judge LLM calls (per QA call), got %d", judgeCalls)
	}

	guardrails.FlushNow(context.Background())
	rows := fake.Rows()
	// Sequence: qa_ask(allowed) → qa_grounding_judge(allowed) →
	// qa_ask_retry_grounding(allowed) → qa_grounding_judge(allowed) →
	// qa_ask(refused, grounding_failed)
	if len(rows) != 5 {
		t.Fatalf("expected 5 LogLLMCall rows, got %d:\n%s", len(rows), rowSummariesShort(rows))
	}
	expect := []struct {
		site     string
		decision string
	}{
		{"qa_ask", "allowed"},
		{"qa_grounding_judge", "allowed"},
		{"qa_ask_retry_grounding", "allowed"},
		{"qa_grounding_judge", "allowed"},
		{"qa_ask", "refused"},
	}
	for i, e := range expect {
		if rows[i].Site != e.site || rows[i].Decision != e.decision {
			t.Errorf("row %d: got %s/%s, want %s/%s", i, rows[i].Site, rows[i].Decision, e.site, e.decision)
		}
	}
	if rows[4].RefusalCode == nil || *rows[4].RefusalCode != "grounding_failed" {
		t.Errorf("row 4 RefusalCode: got %v, want grounding_failed", rows[4].RefusalCode)
	}
}

// TestGenerateAnswer_GroundingGuardrail_AllowsGroundedAnswer drives a
// judge-allowed verdict and asserts no retry, no refusal.
func TestGenerateAnswer_GroundingGuardrail_AllowsGroundedAnswer(t *testing.T) {
	fake := &fakeQAGuardrailsWriter{}
	guardrails.ResetForTest()
	guardrails.Init(fake)
	t.Cleanup(func() { guardrails.Init(nil); guardrails.ResetForTest() })

	t.Setenv("GUARDRAIL_JUDGE_MAX_PER_USER_PER_HOUR", "100")
	guardrails.SetDefaultJudgeQuotaCounter(&fakeJudgeCounterForUtils{hitCount: 0})
	t.Cleanup(func() { guardrails.SetDefaultJudgeQuotaCounter(nil) })

	groundedCitation := `{"answer_status":"answered","answer_text":"$2.4M was proposed.","confidence":0.9,"citations":[{"chunk_id":"real-A","source_type":"transcript","source_id":"s1","locator":"","snippet":"$2.4M proposed"}]}`
	judgeAllow := `{"grounded":true,"rationale":"claim supported by chunk"}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, 4096)
		n, _ := r.Body.Read(body)
		kind := routeQAOrJudge(body[:n])
		w.Header().Set("Content-Type", "application/json")
		if kind == "judge" {
			_ = json.NewEncoder(w).Encode(makeChatCompletionForGrounding(judgeAllow))
			return
		}
		_ = json.NewEncoder(w).Encode(makeChatCompletionForGrounding(groundedCitation))
	}))
	defer srv.Close()
	t.Setenv("OPENAI_API_KEY", "fake-key")
	t.Setenv("OPENAI_BASE_URL", srv.URL+"/v1/")

	ctx := guardrails.WithUserID(context.Background(), uuid.New())
	chunks := []Chunk{{ChunkID: "real-A", SourceType: "transcript", SourceID: "s1", Text: "$2.4M proposed"}}
	qa, _, err := GenerateAnswer(ctx, "Q?", chunks, "T", SessionContext{}, nil)
	if err != nil {
		t.Fatalf("GenerateAnswer: %v", err)
	}
	if qa.Refusal != nil {
		t.Errorf("grounded answer should not refuse; got Refusal=%+v", qa.Refusal)
	}
	if qa.AnswerStatus != "answered" {
		t.Errorf("AnswerStatus=%q, want answered", qa.AnswerStatus)
	}

	guardrails.FlushNow(context.Background())
	rows := fake.Rows()
	// qa_ask(allowed) + qa_grounding_judge(allowed) — no retry, no refusal.
	if len(rows) != 2 {
		t.Fatalf("expected 2 LogLLMCall rows, got %d:\n%s", len(rows), rowSummariesShort(rows))
	}
	if rows[1].Site != "qa_grounding_judge" {
		t.Errorf("row 1 site=%q, want qa_grounding_judge", rows[1].Site)
	}
}

// TestGenerateAnswer_GroundingGuardrail_RateLimitSkipsJudge drives the
// quota-exhausted path. The judge is skipped entirely; the citation-
// enforced answer is returned; a degradation row is logged.
func TestGenerateAnswer_GroundingGuardrail_RateLimitSkipsJudge(t *testing.T) {
	fake := &fakeQAGuardrailsWriter{}
	guardrails.ResetForTest()
	guardrails.Init(fake)
	t.Cleanup(func() { guardrails.Init(nil); guardrails.ResetForTest() })

	t.Setenv("GUARDRAIL_JUDGE_MAX_PER_USER_PER_HOUR", "5")
	// hitCount=100 puts the user well over the cap of 5.
	guardrails.SetDefaultJudgeQuotaCounter(&fakeJudgeCounterForUtils{hitCount: 100})
	t.Cleanup(func() { guardrails.SetDefaultJudgeQuotaCounter(nil) })

	groundedCitation := `{"answer_status":"answered","answer_text":"$2.4M was proposed.","confidence":0.9,"citations":[{"chunk_id":"real-A","source_type":"transcript","source_id":"s1","locator":"","snippet":"$2.4M proposed"}]}`

	var qaCalls, judgeCalls int
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, 4096)
		n, _ := r.Body.Read(body)
		kind := routeQAOrJudge(body[:n])
		mu.Lock()
		switch kind {
		case "judge":
			judgeCalls++
		default:
			qaCalls++
		}
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(makeChatCompletionForGrounding(groundedCitation))
	}))
	defer srv.Close()
	t.Setenv("OPENAI_API_KEY", "fake-key")
	t.Setenv("OPENAI_BASE_URL", srv.URL+"/v1/")

	ctx := guardrails.WithUserID(context.Background(), uuid.New())
	chunks := []Chunk{{ChunkID: "real-A", SourceType: "transcript", SourceID: "s1", Text: "$2.4M proposed"}}
	qa, _, err := GenerateAnswer(ctx, "Q?", chunks, "T", SessionContext{}, nil)
	if err != nil {
		t.Fatalf("GenerateAnswer: %v", err)
	}
	if qa.Refusal != nil {
		t.Errorf("rate-limit path should still return a normal answer; got Refusal=%+v", qa.Refusal)
	}
	if qaCalls != 1 {
		t.Errorf("expected 1 QA call (no retry), got %d", qaCalls)
	}
	if judgeCalls != 0 {
		t.Errorf("expected 0 judge calls (rate-limited), got %d", judgeCalls)
	}

	guardrails.FlushNow(context.Background())
	rows := fake.Rows()
	// qa_ask(allowed) + qa_ask(allowed, grounding_judge_rate_limited).
	if len(rows) != 2 {
		t.Fatalf("expected 2 LogLLMCall rows, got %d:\n%s", len(rows), rowSummariesShort(rows))
	}
	if rows[1].Site != "qa_ask" || rows[1].Decision != "allowed" {
		t.Errorf("row 1: site=%q decision=%q, want qa_ask/allowed", rows[1].Site, rows[1].Decision)
	}
	foundFlag := false
	for _, g := range rows[1].GuardrailsFired {
		if g == "grounding_judge_rate_limited" {
			foundFlag = true
			break
		}
	}
	if !foundFlag {
		t.Errorf("row 1 GuardrailsFired should include grounding_judge_rate_limited; got %v",
			rows[1].GuardrailsFired)
	}
}

func rowSummariesShort(rows []guardrails.LLMCallRow) string {
	var b strings.Builder
	for i, r := range rows {
		b.WriteString("  ")
		b.WriteString(itoa(i))
		b.WriteString(": site=")
		b.WriteString(r.Site)
		b.WriteString(" decision=")
		b.WriteString(r.Decision)
		b.WriteString("\n")
	}
	return b.String()
}

// itoa duplicated locally to avoid importing strconv just for the
// test summary helper.
func itoa(n int) string {
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
