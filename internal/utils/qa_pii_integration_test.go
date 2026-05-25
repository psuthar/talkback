package utils

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/guardrails"
)

// SCRUM-567 (Slice 4c of SCRUM-560): integration test for the PII
// scrubber wired into GenerateAnswer. Drives a QA call whose answer
// text contains PII strings; asserts the response is scrubbed and a
// LogLLMCall row with guardrails_fired=[pii_redacted] is emitted.
//
// The mocked OpenAI server stubs both the QA call (returns an answer
// containing PII) and the grounding judge call (returns grounded:true
// so the path doesn't refuse before the PII scrubber runs).

func TestGenerateAnswer_PIIScrubber_RedactsAnswerText(t *testing.T) {
	fake := &fakeQAGuardrailsWriter{}
	guardrails.ResetForTest()
	guardrails.Init(fake)
	t.Cleanup(func() { guardrails.Init(nil); guardrails.ResetForTest() })
	guardrails.SetDefaultJudgeQuotaCounter(nil) // unlimited

	// The QA answer contains an email + a phone number. After the
	// grounding judge passes, the PII scrubber should redact both.
	qaContent := `{"answer_status":"answered","answer_text":"Alex (alex@example.com) said to call 415-555-1212 about the budget.","confidence":0.9,"citations":[{"chunk_id":"real-A","source_type":"transcript","source_id":"s1","locator":"","snippet":"budget"}]}`
	judgeAllow := `{"grounded":true,"rationale":"supported"}`

	var qaCalls, judgeCalls int
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, 4096)
		n, _ := r.Body.Read(body)
		s := string(body[:n])
		w.Header().Set("Content-Type", "application/json")
		mu.Lock()
		defer mu.Unlock()
		if strings.Contains(s, "strict grounding judge") {
			judgeCalls++
			_ = json.NewEncoder(w).Encode(makeChatCompletionForGrounding(judgeAllow))
			return
		}
		qaCalls++
		_ = json.NewEncoder(w).Encode(makeChatCompletionForGrounding(qaContent))
	}))
	defer srv.Close()
	t.Setenv("OPENAI_API_KEY", "fake-key")
	t.Setenv("OPENAI_BASE_URL", srv.URL+"/v1/")

	ctx := guardrails.WithUserID(context.Background(), uuid.New())
	chunks := []Chunk{{ChunkID: "real-A", SourceType: "transcript", SourceID: "s1", Text: "budget content"}}
	qa, _, err := GenerateAnswer(ctx, "Q?", chunks, "T", SessionContext{}, nil)
	if err != nil {
		t.Fatalf("GenerateAnswer: %v", err)
	}

	if qa.Refusal != nil {
		t.Fatalf("PII scrub should not refuse; got Refusal=%+v", qa.Refusal)
	}
	// Email + phone redacted in answer text
	if strings.Contains(qa.AnswerText, "alex@example.com") {
		t.Errorf("email not redacted; AnswerText=%q", qa.AnswerText)
	}
	if !strings.Contains(qa.AnswerText, guardrails.RedactedEmail) {
		t.Errorf("expected %q marker in AnswerText; got %q", guardrails.RedactedEmail, qa.AnswerText)
	}
	if strings.Contains(qa.AnswerText, "415-555-1212") {
		t.Errorf("phone not redacted; AnswerText=%q", qa.AnswerText)
	}
	if !strings.Contains(qa.AnswerText, guardrails.RedactedPhone) {
		t.Errorf("expected %q marker in AnswerText; got %q", guardrails.RedactedPhone, qa.AnswerText)
	}

	guardrails.FlushNow(context.Background())
	rows := fake.Rows()
	// qa_ask(allowed) + qa_grounding_judge(allowed) + qa_ask(allowed, pii_redacted)
	if len(rows) != 3 {
		t.Fatalf("expected 3 LogLLMCall rows (qa + judge + pii_redacted degradation), got %d:\n%s",
			len(rows), rowSummariesShort(rows))
	}
	piiRow := rows[2]
	if piiRow.Site != "qa_ask" || piiRow.Decision != "allowed" {
		t.Errorf("pii row: site=%q decision=%q, want qa_ask/allowed", piiRow.Site, piiRow.Decision)
	}
	foundFlag := false
	for _, g := range piiRow.GuardrailsFired {
		if g == "pii_redacted" {
			foundFlag = true
		}
	}
	if !foundFlag {
		t.Errorf("pii row GuardrailsFired should include pii_redacted; got %v", piiRow.GuardrailsFired)
	}
	if qaCalls != 1 {
		t.Errorf("expected 1 QA call (no retry), got %d", qaCalls)
	}
	if judgeCalls != 1 {
		t.Errorf("expected 1 judge call, got %d", judgeCalls)
	}
}

// TestGenerateAnswer_PIIScrubber_DisabledByEnv verifies the env
// escape hatch leaves PII in the answer.
func TestGenerateAnswer_PIIScrubber_DisabledByEnv(t *testing.T) {
	fake := &fakeQAGuardrailsWriter{}
	guardrails.ResetForTest()
	guardrails.Init(fake)
	t.Cleanup(func() { guardrails.Init(nil); guardrails.ResetForTest() })
	guardrails.SetDefaultJudgeQuotaCounter(nil)

	t.Setenv("GUARDRAIL_PII_SCRUB", "off")

	qaContent := `{"answer_status":"answered","answer_text":"alex@example.com confirmed.","confidence":0.9,"citations":[{"chunk_id":"c1","source_type":"transcript","source_id":"s1","locator":"","snippet":"x"}]}`
	judgeAllow := `{"grounded":true,"rationale":"ok"}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, 4096)
		n, _ := r.Body.Read(body)
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(string(body[:n]), "strict grounding judge") {
			_ = json.NewEncoder(w).Encode(makeChatCompletionForGrounding(judgeAllow))
			return
		}
		_ = json.NewEncoder(w).Encode(makeChatCompletionForGrounding(qaContent))
	}))
	defer srv.Close()
	t.Setenv("OPENAI_API_KEY", "fake-key")
	t.Setenv("OPENAI_BASE_URL", srv.URL+"/v1/")

	ctx := guardrails.WithUserID(context.Background(), uuid.New())
	chunks := []Chunk{{ChunkID: "c1", SourceType: "transcript", SourceID: "s1", Text: "content"}}
	qa, _, err := GenerateAnswer(ctx, "Q?", chunks, "T", SessionContext{}, nil)
	if err != nil {
		t.Fatalf("GenerateAnswer: %v", err)
	}
	if !strings.Contains(qa.AnswerText, "alex@example.com") {
		t.Errorf("with PII scrub off, email should pass through; got %q", qa.AnswerText)
	}

	guardrails.FlushNow(context.Background())
	rows := fake.Rows()
	// Only qa_ask + qa_grounding_judge — no pii_redacted row.
	if len(rows) != 2 {
		t.Errorf("expected 2 LogLLMCall rows with scrub off, got %d:\n%s",
			len(rows), rowSummariesShort(rows))
	}
}
