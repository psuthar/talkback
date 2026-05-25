package utils

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/psuthar/talkback/internal/guardrails"
)

// SCRUM-565 (Slice 4a of SCRUM-560): integration test for the
// citation-enforcement retry-refuse path in GenerateAnswer. Stubs the
// OpenAI Chat Completions endpoint with httptest so we can drive the
// LLM rounds deterministically and assert that:
//
//  1. A first response with zero grounded citations triggers a retry.
//  2. If the retry also has zero grounded citations, GenerateAnswer
//     returns a QAResponse with Refusal set (citation_missing).
//  3. The LogLLMCall sequence is initial → retry → refused, with the
//     correct site tags on each row.
//
// `t.Setenv("OPENAI_BASE_URL", ...)` redirects the openai-go SDK to
// the local httptest server; the production code path is unchanged.

// fakeQAGuardrailsWriter — local sibling of the fake writers in
// handlers/mcpserver. Same shape as guardrails.Writer.
type fakeQAGuardrailsWriter struct {
	mu   sync.Mutex
	rows []guardrails.LLMCallRow
}

func (f *fakeQAGuardrailsWriter) InsertLLMCallRow(_ context.Context, row guardrails.LLMCallRow) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rows = append(f.rows, row)
	return nil
}

func (f *fakeQAGuardrailsWriter) Rows() []guardrails.LLMCallRow {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]guardrails.LLMCallRow, len(f.rows))
	copy(out, f.rows)
	return out
}

// chatCompletionResponse is the minimal slice of the OpenAI Chat
// Completions response shape that openai-go consumes — we only need
// .choices[].message.content and .usage.* for our path.
type chatCompletionResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index   int `json:"index"`
		Message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

func makeChatCompletion(content string) chatCompletionResponse {
	r := chatCompletionResponse{
		ID:      "chatcmpl-test",
		Object:  "chat.completion",
		Created: 0,
		Model:   "gpt-4o-mini",
	}
	r.Choices = make([]struct {
		Index   int `json:"index"`
		Message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	}, 1)
	r.Choices[0].Index = 0
	r.Choices[0].Message.Role = "assistant"
	r.Choices[0].Message.Content = content
	r.Choices[0].FinishReason = "stop"
	r.Usage.PromptTokens = 100
	r.Usage.CompletionTokens = 50
	r.Usage.TotalTokens = 150
	return r
}

// TestGenerateAnswer_CitationGuardrail_RetryThenRefuse drives two
// rounds of hallucinated citations and asserts the refusal path.
func TestGenerateAnswer_CitationGuardrail_RetryThenRefuse(t *testing.T) {
	fake := &fakeQAGuardrailsWriter{}
	guardrails.ResetForTest()
	guardrails.Init(fake)
	t.Cleanup(func() { guardrails.Init(nil); guardrails.ResetForTest() })

	// Two LLM responses, both with citations that don't match the
	// retrieved chunk_ids — the guardrail should fire after each.
	hallucinated := `{"answer_status":"answered","answer_text":"X happened.","confidence":0.9,"citations":[{"chunk_id":"hallucinated-id","source_type":"transcript","source_id":"s1","locator":"","snippet":"made up"}]}`

	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "chat/completions") {
			http.NotFound(w, r)
			return
		}
		callCount++
		body := makeChatCompletion(hallucinated)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(body)
	}))
	defer srv.Close()

	t.Setenv("OPENAI_API_KEY", "fake-key-for-test")
	t.Setenv("OPENAI_BASE_URL", srv.URL+"/v1/")

	chunks := []Chunk{
		{ChunkID: "real-A", SourceType: "transcript", SourceID: "s1", Text: "Actual content A."},
		{ChunkID: "real-B", SourceType: "transcript", SourceID: "s1", Text: "Actual content B."},
	}

	qa, _, err := GenerateAnswer(context.Background(), "What happened?", chunks, "Test artifact", SessionContext{}, nil)
	if err != nil {
		t.Fatalf("GenerateAnswer returned error: %v", err)
	}

	// 1) Refusal populated
	if qa == nil {
		t.Fatalf("expected non-nil QAResponse, got nil")
	}
	if qa.Refusal == nil {
		t.Fatalf("expected Refusal to be set after retry also failed citation check; got nil. answer=%s status=%s citations=%v",
			qa.AnswerText, qa.AnswerStatus, qa.Citations)
	}
	if qa.Refusal.Code != "citation_missing" {
		t.Errorf("Refusal.Code=%q, want citation_missing", qa.Refusal.Code)
	}
	if qa.Refusal.UserMessage != guardrails.UserMessageCitationMissing {
		t.Errorf("Refusal.UserMessage=%q, want contract-locked string", qa.Refusal.UserMessage)
	}

	// 2) Two LLM rounds were made
	if callCount != 2 {
		t.Errorf("expected 2 LLM calls (initial + retry), got %d", callCount)
	}

	// 3) Three LogLLMCall rows: initial (allowed) + retry (allowed) + refusal (refused)
	guardrails.FlushNow(context.Background())
	rows := fake.Rows()
	if len(rows) != 3 {
		t.Fatalf("expected 3 LogLLMCall rows, got %d:\n%v", len(rows), rowSummaries(rows))
	}
	if rows[0].Site != "qa_ask" || rows[0].Decision != "allowed" {
		t.Errorf("row 0: site=%q decision=%q, want qa_ask/allowed", rows[0].Site, rows[0].Decision)
	}
	if rows[1].Site != "qa_ask_retry_citation" || rows[1].Decision != "allowed" {
		t.Errorf("row 1: site=%q decision=%q, want qa_ask_retry_citation/allowed", rows[1].Site, rows[1].Decision)
	}
	if rows[2].Site != "qa_ask" || rows[2].Decision != "refused" {
		t.Errorf("row 2: site=%q decision=%q, want qa_ask/refused", rows[2].Site, rows[2].Decision)
	}
	if rows[2].RefusalCode == nil || *rows[2].RefusalCode != "citation_missing" {
		t.Errorf("row 2 RefusalCode: got %v, want citation_missing", rows[2].RefusalCode)
	}
}

// TestGenerateAnswer_CitationGuardrail_AllowsGroundedAnswer drives a
// single QA round that returns a citation actually matching one of
// the retrieved chunk_ids — must NOT retry the citation guardrail,
// must NOT refuse. SCRUM-566 wired the grounding judge in after the
// citation check; this test stubs the judge to return `grounded:true`
// so the path stays clean.
func TestGenerateAnswer_CitationGuardrail_AllowsGroundedAnswer(t *testing.T) {
	fake := &fakeQAGuardrailsWriter{}
	guardrails.ResetForTest()
	guardrails.Init(fake)
	t.Cleanup(func() { guardrails.Init(nil); guardrails.ResetForTest() })
	// SCRUM-566: disable the per-user judge quota so the test is
	// deterministic regardless of process-wide counter state.
	guardrails.SetDefaultJudgeQuotaCounter(nil)

	grounded := `{"answer_status":"answered","answer_text":"X happened.","confidence":0.9,"citations":[{"chunk_id":"real-A","source_type":"transcript","source_id":"s1","locator":"","snippet":"real"}]}`
	judgeAllow := `{"grounded":true,"rationale":"all claims supported"}`

	var qaCalls, judgeCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, 4096)
		n, _ := r.Body.Read(body)
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(string(body[:n]), "strict grounding judge") {
			judgeCalls++
			_ = json.NewEncoder(w).Encode(makeChatCompletion(judgeAllow))
			return
		}
		qaCalls++
		_ = json.NewEncoder(w).Encode(makeChatCompletion(grounded))
	}))
	defer srv.Close()

	t.Setenv("OPENAI_API_KEY", "fake-key-for-test")
	t.Setenv("OPENAI_BASE_URL", srv.URL+"/v1/")

	chunks := []Chunk{{ChunkID: "real-A", SourceType: "transcript", SourceID: "s1", Text: "Actual A."}}
	qa, _, err := GenerateAnswer(context.Background(), "What happened?", chunks, "T", SessionContext{}, nil)
	if err != nil {
		t.Fatalf("GenerateAnswer: %v", err)
	}
	if qa.Refusal != nil {
		t.Errorf("grounded citation should not refuse; got Refusal=%+v", qa.Refusal)
	}
	if qaCalls != 1 {
		t.Errorf("expected 1 QA LLM call (no retry), got %d", qaCalls)
	}
	if judgeCalls != 1 {
		t.Errorf("expected 1 judge LLM call, got %d", judgeCalls)
	}
	guardrails.FlushNow(context.Background())
	rows := fake.Rows()
	// qa_ask(allowed) + qa_grounding_judge(allowed)
	if len(rows) != 2 {
		t.Errorf("expected 2 LogLLMCall rows (qa_ask + qa_grounding_judge), got %d:\n%v",
			len(rows), rowSummaries(rows))
	}
}

// TestGenerateAnswer_CitationGuardrail_RecoversOnRetry drives a first
// response with hallucinated citations and a retry response that
// grounds correctly — must allow the retry's answer through. SCRUM-566
// wired the grounding judge in after the citation check; the judge is
// stubbed to allow on the recovered answer.
func TestGenerateAnswer_CitationGuardrail_RecoversOnRetry(t *testing.T) {
	fake := &fakeQAGuardrailsWriter{}
	guardrails.ResetForTest()
	guardrails.Init(fake)
	t.Cleanup(func() { guardrails.Init(nil); guardrails.ResetForTest() })
	guardrails.SetDefaultJudgeQuotaCounter(nil)

	hallucinated := `{"answer_status":"answered","answer_text":"X happened.","confidence":0.9,"citations":[{"chunk_id":"hallucinated","source_type":"transcript","source_id":"s1","locator":"","snippet":"bad"}]}`
	grounded := `{"answer_status":"answered","answer_text":"X happened.","confidence":0.9,"citations":[{"chunk_id":"real-A","source_type":"transcript","source_id":"s1","locator":"","snippet":"good"}]}`
	judgeAllow := `{"grounded":true,"rationale":"all claims supported"}`

	var qaCalls, judgeCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := make([]byte, 4096)
		n, _ := r.Body.Read(body)
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(string(body[:n]), "strict grounding judge") {
			judgeCalls++
			_ = json.NewEncoder(w).Encode(makeChatCompletion(judgeAllow))
			return
		}
		qaCalls++
		if qaCalls == 1 {
			_ = json.NewEncoder(w).Encode(makeChatCompletion(hallucinated))
			return
		}
		_ = json.NewEncoder(w).Encode(makeChatCompletion(grounded))
	}))
	defer srv.Close()

	t.Setenv("OPENAI_API_KEY", "fake-key-for-test")
	t.Setenv("OPENAI_BASE_URL", srv.URL+"/v1/")

	chunks := []Chunk{{ChunkID: "real-A", SourceType: "transcript", SourceID: "s1", Text: "A."}}
	qa, _, err := GenerateAnswer(context.Background(), "Q?", chunks, "T", SessionContext{}, nil)
	if err != nil {
		t.Fatalf("GenerateAnswer: %v", err)
	}
	if qa.Refusal != nil {
		t.Errorf("retry should have recovered; got Refusal=%+v", qa.Refusal)
	}
	if qaCalls != 2 {
		t.Errorf("expected 2 QA LLM calls (initial + recovering retry), got %d", qaCalls)
	}
	if judgeCalls != 1 {
		t.Errorf("expected 1 judge LLM call (post-recovery), got %d", judgeCalls)
	}
	if len(qa.Citations) != 1 || qa.Citations[0].ChunkID != "real-A" {
		t.Errorf("expected retry citation to be real-A; got %v", qa.Citations)
	}
	guardrails.FlushNow(context.Background())
	rows := fake.Rows()
	// qa_ask(allowed) + qa_ask_retry_citation(allowed) + qa_grounding_judge(allowed)
	if len(rows) != 3 {
		t.Errorf("expected 3 LogLLMCall rows, got %d:\n%v", len(rows), rowSummaries(rows))
	}
	if len(rows) >= 2 && rows[1].Site != "qa_ask_retry_citation" {
		t.Errorf("row 1 site=%q, want qa_ask_retry_citation", rows[1].Site)
	}
}

func rowSummaries(rows []guardrails.LLMCallRow) string {
	var b strings.Builder
	for i, r := range rows {
		rc := ""
		if r.RefusalCode != nil {
			rc = *r.RefusalCode
		}
		fmt.Fprintf(&b, "  %d: site=%s decision=%s refusal_code=%s\n", i, r.Site, r.Decision, rc)
	}
	return b.String()
}
