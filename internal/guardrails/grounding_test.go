package guardrails

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

// SCRUM-566 (Slice 4b of SCRUM-560): unit + integration tests for the
// grounding LLM-as-judge. The actual LLM call is stubbed via an
// httptest server that returns the judge verdict we configure per
// case — we're exercising the prompt-assembly + verdict-parsing +
// LogLLMCall + rate-limit + env-var-override surfaces, not the live
// judge model's accuracy.
//
// The fixture set at testdata/grounding_samples.json includes the
// adversarial-grounded cases SCRUM-569 Gap 4 named (an answer that
// cites a real chunk but fabricates a number from it). Each case
// names the expected verdict; the mocked judge returns that verdict.

func TestJudgeModelName_EnvOverridePrecedence(t *testing.T) {
	t.Setenv("GUARDRAIL_JUDGE_MODEL", "")
	t.Setenv("GUARDRAIL_JUDGE_DOWNGRADE_MODEL", "")
	if got := JudgeModelName(); got != defaultJudgeModel {
		t.Errorf("default: got %q, want %q", got, defaultJudgeModel)
	}

	t.Setenv("GUARDRAIL_JUDGE_MODEL", "gpt-4o")
	if got := JudgeModelName(); got != "gpt-4o" {
		t.Errorf("with JUDGE_MODEL=gpt-4o: got %q", got)
	}

	t.Setenv("GUARDRAIL_JUDGE_DOWNGRADE_MODEL", "gpt-3.5-turbo")
	if got := JudgeModelName(); got != "gpt-3.5-turbo" {
		t.Errorf("with DOWNGRADE_MODEL set: downgrade should win, got %q", got)
	}

	t.Setenv("GUARDRAIL_JUDGE_DOWNGRADE_MODEL", "  ")
	if got := JudgeModelName(); got != "gpt-4o" {
		t.Errorf("whitespace DOWNGRADE_MODEL: should fall through to JUDGE_MODEL, got %q", got)
	}
}

func TestMaxJudgeCallsPerUserPerHour_EnvOverride(t *testing.T) {
	t.Setenv("GUARDRAIL_JUDGE_MAX_PER_USER_PER_HOUR", "")
	if got := MaxJudgeCallsPerUserPerHour(); got != defaultJudgeMaxPerUserPerHour {
		t.Errorf("default: got %d, want %d", got, defaultJudgeMaxPerUserPerHour)
	}
	t.Setenv("GUARDRAIL_JUDGE_MAX_PER_USER_PER_HOUR", "25")
	if got := MaxJudgeCallsPerUserPerHour(); got != 25 {
		t.Errorf("override=25: got %d", got)
	}
	t.Setenv("GUARDRAIL_JUDGE_MAX_PER_USER_PER_HOUR", "0")
	if got := MaxJudgeCallsPerUserPerHour(); got != 0 {
		t.Errorf("override=0 (unlimited): got %d", got)
	}
	t.Setenv("GUARDRAIL_JUDGE_MAX_PER_USER_PER_HOUR", "abc")
	if got := MaxJudgeCallsPerUserPerHour(); got != defaultJudgeMaxPerUserPerHour {
		t.Errorf("garbage value should fall back to default; got %d", got)
	}
	t.Setenv("GUARDRAIL_JUDGE_MAX_PER_USER_PER_HOUR", "-5")
	if got := MaxJudgeCallsPerUserPerHour(); got != defaultJudgeMaxPerUserPerHour {
		t.Errorf("negative value should fall back to default; got %d", got)
	}
}

// fakeJudgeCounter implements JudgeQuotaCounter for rate-limit tests.
type fakeJudgeCounter struct {
	mu     sync.Mutex
	counts map[string]int // key: site+userID
	err    error
}

func (f *fakeJudgeCounter) CountLLMCallsBySiteAndUserSince(_ context.Context, site string, uid uuid.UUID, _ time.Time) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return 0, f.err
	}
	return f.counts[site+":"+uid.String()], nil
}

func TestCheckJudgeQuota_DisabledPaths(t *testing.T) {
	// Disabled (nil counter)
	SetDefaultJudgeQuotaCounter(nil)
	t.Cleanup(func() { SetDefaultJudgeQuotaCounter(nil) })

	t.Setenv("GUARDRAIL_JUDGE_MAX_PER_USER_PER_HOUR", "100")
	uid := uuid.New()
	ok, err := CheckJudgeQuota(context.Background(), &uid)
	if err != nil || !ok {
		t.Errorf("nil counter: want (true, nil); got (%v, %v)", ok, err)
	}

	// Disabled (nil userID)
	SetDefaultJudgeQuotaCounter(&fakeJudgeCounter{counts: map[string]int{}})
	ok, err = CheckJudgeQuota(context.Background(), nil)
	if err != nil || !ok {
		t.Errorf("nil userID: want (true, nil); got (%v, %v)", ok, err)
	}

	// Disabled (max=0)
	t.Setenv("GUARDRAIL_JUDGE_MAX_PER_USER_PER_HOUR", "0")
	ok, err = CheckJudgeQuota(context.Background(), &uid)
	if err != nil || !ok {
		t.Errorf("max=0 (unlimited): want (true, nil); got (%v, %v)", ok, err)
	}
}

func TestCheckJudgeQuota_BelowAndAboveCap(t *testing.T) {
	t.Setenv("GUARDRAIL_JUDGE_MAX_PER_USER_PER_HOUR", "5")
	uid := uuid.New()
	counter := &fakeJudgeCounter{counts: map[string]int{}}
	SetDefaultJudgeQuotaCounter(counter)
	t.Cleanup(func() { SetDefaultJudgeQuotaCounter(nil) })

	// 0 calls used → allowed
	counter.counts["qa_grounding_judge:"+uid.String()] = 0
	ok, err := CheckJudgeQuota(context.Background(), &uid)
	if err != nil || !ok {
		t.Errorf("0/5: want allowed; got (%v, %v)", ok, err)
	}

	// 4 calls used → allowed (one slot left)
	counter.counts["qa_grounding_judge:"+uid.String()] = 4
	ok, _ = CheckJudgeQuota(context.Background(), &uid)
	if !ok {
		t.Errorf("4/5: want allowed; got %v", ok)
	}

	// 5 calls used → blocked
	counter.counts["qa_grounding_judge:"+uid.String()] = 5
	ok, _ = CheckJudgeQuota(context.Background(), &uid)
	if ok {
		t.Errorf("5/5: want blocked; got %v", ok)
	}

	// 100 calls used → blocked
	counter.counts["qa_grounding_judge:"+uid.String()] = 100
	ok, _ = CheckJudgeQuota(context.Background(), &uid)
	if ok {
		t.Errorf("100/5: want blocked; got %v", ok)
	}
}

func TestCheckJudgeQuota_FailsOpenOnDBError(t *testing.T) {
	t.Setenv("GUARDRAIL_JUDGE_MAX_PER_USER_PER_HOUR", "5")
	counter := &fakeJudgeCounter{err: errors.New("transient db error")}
	SetDefaultJudgeQuotaCounter(counter)
	t.Cleanup(func() { SetDefaultJudgeQuotaCounter(nil) })

	uid := uuid.New()
	ok, err := CheckJudgeQuota(context.Background(), &uid)
	// Fail-open: return true (allowed) but propagate the error so the
	// caller can log it.
	if !ok {
		t.Errorf("DB error: want allowed (fail-open); got %v", ok)
	}
	if err == nil {
		t.Errorf("expected non-nil error to propagate; got nil")
	}
}

func TestFormatJudgeUserPrompt_StructureContainsExpectedSections(t *testing.T) {
	got := formatJudgeUserPrompt(
		"What budget?",
		"Budget is $2.4M.",
		[]JudgeChunk{
			{ChunkID: "c1", SourceType: "transcript", Text: "Proposed $2.4M for APAC."},
			{ChunkID: "c2", SourceType: "material", Text: "Slide 3: budget options."},
		},
	)
	for _, sub := range []string{
		"USER QUESTION:",
		"What budget?",
		"ASSISTANT ANSWER:",
		"Budget is $2.4M.",
		"CITED CHUNKS:",
		"[1] chunk_id=c1 source_type=transcript",
		"Proposed $2.4M for APAC.",
		"[2] chunk_id=c2 source_type=material",
		"Return the JSON verdict",
	} {
		if !strings.Contains(got, sub) {
			t.Errorf("user prompt missing %q\n---\n%s\n---", sub, got)
		}
	}
}

// TestCheckGrounding_FixtureSet drives each fixture case through
// CheckGrounding with a mocked judge response that matches the
// expected verdict. Asserts that:
//   - grounded cases produce Allow=true.
//   - ungrounded + adversarial_ungrounded cases produce a
//     grounding_failed refusal.
//
// The judge call itself is mocked — this verifies the wire-up + parse
// path, not the judge model's accuracy (the live accuracy lives in
// the qa-eval-refresh workflow_dispatch run).
func TestCheckGrounding_FixtureSet(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "grounding_samples.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var fixture struct {
		Cases []struct {
			CaseID      string `json:"case_id"`
			Label       string `json:"label"`
			Question    string `json:"question"`
			Answer      string `json:"answer"`
			CitedChunks []struct {
				ChunkID    string `json:"chunk_id"`
				SourceType string `json:"source_type"`
				Text       string `json:"text"`
			} `json:"cited_chunks"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	if len(fixture.Cases) == 0 {
		t.Fatal("empty fixture")
	}

	// Map label → judge verdict. The mock judge returns this verdict
	// per request — we're testing CheckGrounding's parse + decision
	// path against the contract, not the judge's accuracy.
	verdictForLabel := map[string]judgeVerdict{
		"grounded":               {Grounded: true, Rationale: "all claims supported"},
		"ungrounded":             {Grounded: false, Rationale: "claim not in chunks"},
		"adversarial_ungrounded": {Grounded: false, Rationale: "fabricated specifics absent from chunks"},
	}

	for _, c := range fixture.Cases {
		c := c
		t.Run(c.CaseID, func(t *testing.T) {
			verdict, ok := verdictForLabel[c.Label]
			if !ok {
				t.Fatalf("unknown label %q", c.Label)
			}

			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := json.Marshal(verdict)
				resp := map[string]any{
					"id":      "chatcmpl-test",
					"object":  "chat.completion",
					"created": 0,
					"model":   "gpt-4o-mini",
					"choices": []map[string]any{{
						"index":         0,
						"message":       map[string]any{"role": "assistant", "content": string(body)},
						"finish_reason": "stop",
					}},
					"usage": map[string]any{"prompt_tokens": 50, "completion_tokens": 20, "total_tokens": 70},
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(resp)
			}))
			defer srv.Close()
			t.Setenv("OPENAI_API_KEY", "fake-key")
			t.Setenv("OPENAI_BASE_URL", srv.URL+"/v1/")

			ResetForTest()
			fake := &fakeWriter{}
			Init(fake)
			t.Cleanup(func() { Init(nil); ResetForTest() })

			cited := make([]JudgeChunk, len(c.CitedChunks))
			for i, ch := range c.CitedChunks {
				cited[i] = JudgeChunk{ChunkID: ch.ChunkID, SourceType: ch.SourceType, Text: ch.Text}
			}
			d := CheckGrounding(context.Background(), c.Question, c.Answer, cited)
			FlushNow(context.Background())

			switch c.Label {
			case "grounded":
				if !d.Allow {
					t.Errorf("%s (%s): want Allow, got refusal guardrail=%s detail=%s",
						c.CaseID, c.Label, d.Guardrail, d.Detail)
				}
			case "ungrounded", "adversarial_ungrounded":
				if d.Allow {
					t.Errorf("%s (%s): want refusal, got Allow detail=%s", c.CaseID, c.Label, d.Detail)
				}
				if d.Guardrail != GuardrailGroundingFailed {
					t.Errorf("%s: want grounding_failed, got %s", c.CaseID, d.Guardrail)
				}
				if d.UserMessage != UserMessageGroundingFailed {
					t.Errorf("%s: user_message=%q is not the contract string", c.CaseID, d.UserMessage)
				}
			}

			// LogLLMCall fired for the judge round.
			rows := fake.Rows()
			if len(rows) != 1 {
				t.Fatalf("%s: expected 1 LogLLMCall row (judge), got %d", c.CaseID, len(rows))
			}
			if rows[0].Site != "qa_grounding_judge" {
				t.Errorf("%s: row site=%q, want qa_grounding_judge", c.CaseID, rows[0].Site)
			}
			if rows[0].Decision != "allowed" {
				t.Errorf("%s: row decision=%q, want allowed (the judge call itself is allowed; refusal is recorded by the caller)",
					c.CaseID, rows[0].Decision)
			}
		})
	}
}

// TestCheckGrounding_FailsOpenOnTransportError verifies that a judge
// transport error (network hiccup, 500, etc.) does NOT refuse the
// answer — it falls through to Allow with the error stamped on Detail.
func TestCheckGrounding_FailsOpenOnTransportError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	t.Setenv("OPENAI_API_KEY", "fake-key")
	t.Setenv("OPENAI_BASE_URL", srv.URL+"/v1/")

	d := CheckGrounding(context.Background(), "Q?", "A.", []JudgeChunk{{ChunkID: "c1", Text: "x"}})
	if !d.Allow {
		t.Errorf("transport error: want Allow (fail-open), got refusal %s", d.Guardrail)
	}
	if !strings.Contains(d.Detail, "judge") {
		t.Errorf("Detail should mention judge transport error; got %q", d.Detail)
	}
}

// TestCheckGrounding_FailsOpenOnMalformedJSON verifies the same
// fail-open semantics when the judge returns non-JSON content.
func TestCheckGrounding_FailsOpenOnMalformedJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"id":      "chatcmpl-test",
			"object":  "chat.completion",
			"created": 0,
			"model":   "gpt-4o-mini",
			"choices": []map[string]any{{
				"index":         0,
				"message":       map[string]any{"role": "assistant", "content": "not json at all"},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()
	t.Setenv("OPENAI_API_KEY", "fake-key")
	t.Setenv("OPENAI_BASE_URL", srv.URL+"/v1/")

	ResetForTest()
	fake := &fakeWriter{}
	Init(fake)
	t.Cleanup(func() { Init(nil); ResetForTest() })

	d := CheckGrounding(context.Background(), "Q?", "A.", []JudgeChunk{{ChunkID: "c1", Text: "x"}})
	if !d.Allow {
		t.Errorf("malformed JSON: want Allow (fail-open), got refusal %s", d.Guardrail)
	}
	if !strings.Contains(d.Detail, "parse") {
		t.Errorf("Detail should mention parse error; got %q", d.Detail)
	}
}

// TestCheckGrounding_NoAPIKey_FailsOpen verifies the env-not-set path.
func TestCheckGrounding_NoAPIKey_FailsOpen(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	d := CheckGrounding(context.Background(), "Q?", "A.", []JudgeChunk{{ChunkID: "c1"}})
	if !d.Allow {
		t.Errorf("no API key: want Allow; got refusal %s", d.Guardrail)
	}
	if !strings.Contains(d.Detail, "OPENAI_API_KEY") {
		t.Errorf("Detail should mention OPENAI_API_KEY; got %q", d.Detail)
	}
}

// summarizeForLog is a tiny helper duplicated to avoid leaking test
// state through the package's test-only API.
func summarizeForLog(rows []LLMCallRow) string {
	var b strings.Builder
	for i, r := range rows {
		fmt.Fprintf(&b, "  %d: site=%s decision=%s\n", i, r.Site, r.Decision)
	}
	return b.String()
}

var _ = summarizeForLog // silences unused if a future test grows past the current set
