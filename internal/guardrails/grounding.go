// SCRUM-566 (Slice 4b of SCRUM-560): grounding LLM-as-judge. Citation
// enforcement (Slice 4a) catches answers that cite nothing, but a
// hallucinated answer can still cite a real chunk while making claims
// not actually supported by it. CheckGrounding sends a cheap judge
// model the question + answer + cited chunk texts and asks for a
// strict yes/no on whether every factual claim is supported.
//
// Cost model: one extra cheap-model call per QA request when the
// citation check passes (the judge isn't invoked when an answer is
// already refused by CheckCitations or marked not_covered). The
// per-user rate limit (CheckJudgeQuota below) is the operational
// safety net against an adversarial-question amplifier — when the
// quota is exhausted the request degrades to the citation-enforced
// path without the judge call, and the user still gets an answer.
//
// Env vars:
//
//   - GUARDRAIL_JUDGE_MODEL — override the default judge model
//     (default: gpt-4o-mini, same model qa.go uses for the main call;
//     keeps the cost predictable until SCRUM-566 ships measurement).
//   - GUARDRAIL_JUDGE_DOWNGRADE_MODEL — when set, all judge calls use
//     this model instead of GUARDRAIL_JUDGE_MODEL. Lets on-call shift
//     to a cheaper model under load without a deploy.
//   - GUARDRAIL_JUDGE_MAX_PER_USER_PER_HOUR — per-user quota window
//     (default: 100). 0 = unlimited.
package guardrails

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/shared"
)

// Default judge model. gpt-4o-mini matches the main qa.go call so
// cost stays predictable. Tunable via GUARDRAIL_JUDGE_MODEL; can be
// hot-shifted at on-call via GUARDRAIL_JUDGE_DOWNGRADE_MODEL.
const defaultJudgeModel = "gpt-4o-mini"

// defaultJudgeMaxPerUserPerHour is the per-user judge-call cap. 100
// gives a daily ceiling of ~2400 judge calls per user — enough for
// even an aggressive analytics session, low enough to bound the
// adversarial-question amplifier.
const defaultJudgeMaxPerUserPerHour = 100

// JudgeGuardrailsFiredOnRateLimit is the value stamped on
// LLMCallRow.GuardrailsFired when the rate limit forced the judge to
// be skipped. The QA call's decision stays "allowed" — the user still
// receives the citation-enforced answer; this is purely a degradation
// signal for admin telemetry. Enum addition tracked in
// docs/guardrails/log-shape.md.
const JudgeGuardrailsFiredOnRateLimit = "grounding_judge_rate_limited"

// judgeVerdict is the parsed shape returned by the judge model.
// Defined inline as a JSON schema in the system prompt below.
type judgeVerdict struct {
	Grounded  bool   `json:"grounded"`
	Rationale string `json:"rationale"`
}

// JudgeQuotaCounter abstracts the Postgres-backed per-user judge-call
// counter so the guardrails package doesn't import the database
// package directly. internal/database/llm_call_log.go's
// `CountLLMCallsBySiteAndUserSince` implements this interface; main.go
// wires it via SetDefaultJudgeQuotaCounter at startup.
type JudgeQuotaCounter interface {
	CountLLMCallsBySiteAndUserSince(ctx context.Context, site string, userID uuid.UUID, since time.Time) (int, error)
}

// defaultJudgeQuotaCounter is the package-level counter resolved by
// CheckJudgeQuota when no per-call counter is supplied. main.go sets
// this at startup; tests can override it. nil = quota disabled (the
// safe default for non-DB-aware test harnesses).
var defaultJudgeQuotaCounter JudgeQuotaCounter

// SetDefaultJudgeQuotaCounter wires the package-level counter. Called
// from main.go once at startup with the DB instance.
func SetDefaultJudgeQuotaCounter(c JudgeQuotaCounter) {
	defaultJudgeQuotaCounter = c
}

// JudgeModelName resolves the active judge model name. Env override
// order: GUARDRAIL_JUDGE_DOWNGRADE_MODEL > GUARDRAIL_JUDGE_MODEL >
// defaultJudgeModel. Public so tests + admin telemetry can observe
// which model is in force without re-implementing the precedence.
func JudgeModelName() string {
	if v := strings.TrimSpace(os.Getenv("GUARDRAIL_JUDGE_DOWNGRADE_MODEL")); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("GUARDRAIL_JUDGE_MODEL")); v != "" {
		return v
	}
	return defaultJudgeModel
}

// MaxJudgeCallsPerUserPerHour resolves the active quota from
// GUARDRAIL_JUDGE_MAX_PER_USER_PER_HOUR (default 100). 0 disables.
// Negative or non-numeric values fall back to the default.
func MaxJudgeCallsPerUserPerHour() int {
	v := strings.TrimSpace(os.Getenv("GUARDRAIL_JUDGE_MAX_PER_USER_PER_HOUR"))
	if v == "" {
		return defaultJudgeMaxPerUserPerHour
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return defaultJudgeMaxPerUserPerHour
	}
	return n
}

// CheckJudgeQuota returns true when the user has remaining judge-call
// quota in the current hour. nil userID, nil counter, or a quota of 0
// all return true (quota disabled). DB errors fail open with an error
// — qa.go logs the error and proceeds with the judge call rather than
// blocking the QA path on transient DB issues.
func CheckJudgeQuota(ctx context.Context, userID *uuid.UUID) (bool, error) {
	max := MaxJudgeCallsPerUserPerHour()
	if max <= 0 || userID == nil || defaultJudgeQuotaCounter == nil {
		return true, nil
	}
	since := time.Now().Add(-1 * time.Hour)
	count, err := defaultJudgeQuotaCounter.CountLLMCallsBySiteAndUserSince(ctx, "qa_grounding_judge", *userID, since)
	if err != nil {
		// Fail open on counter errors — better to over-judge than to
		// block the QA path on transient DB hiccups.
		return true, fmt.Errorf("CheckJudgeQuota: %w", err)
	}
	return count < max, nil
}

// JudgeChunk is the minimal per-cited-chunk shape CheckGrounding hands
// to the judge prompt. Defined here (rather than reusing
// models.Citation) so the guardrails package keeps its dependency
// surface lean — qa.go translates citations into JudgeChunks before
// calling.
type JudgeChunk struct {
	ChunkID    string
	SourceType string
	Text       string
}

// groundingSystemPrompt is the judge directive. The verdict shape is
// JSON-locked so parsing is deterministic; the rationale field gives
// admin telemetry / debugging a one-line "why" without unstructured
// prose drift. Kept inline in this file so a regex-search for the
// prompt finds its consumer in the same place.
const groundingSystemPrompt = `You are a strict grounding judge. You will be shown a user question, an assistant answer, and the cited chunks the answer claims to be derived from.

Your job: decide whether EVERY factual claim in the assistant's answer is supported by the content of at least one cited chunk.

Treat as UNGROUNDED:
- Numbers, dates, or quantities not present verbatim or as a directly entailed paraphrase in the cited chunks (e.g. claiming "$2.4M was approved" when the chunks only say "$2.4M was proposed").
- Named entities (people, projects, organizations) not present in the cited chunks.
- Causal claims, motivations, or outcomes the chunks do not state.
- Specific dates / quotes the chunks do not contain.

Treat as GROUNDED:
- Restating, summarizing, or paraphrasing what the chunks say without adding new facts.
- Reasonable surface-level inference where every premise is in the chunks.

Return EXACTLY this JSON shape — no markdown, no surrounding prose:
{
  "grounded": true | false,
  "rationale": "one short sentence; for ungrounded answers, name the unsupported claim"
}`

// formatJudgeUserPrompt builds the user message the judge sees. Kept
// as a separate function so it's pure / testable and easy to tweak
// without touching CheckGrounding.
func formatJudgeUserPrompt(question, answer string, citedChunks []JudgeChunk) string {
	var b strings.Builder
	b.WriteString("USER QUESTION:\n")
	b.WriteString(question)
	b.WriteString("\n\nASSISTANT ANSWER:\n")
	b.WriteString(answer)
	b.WriteString("\n\nCITED CHUNKS:\n")
	for i, c := range citedChunks {
		fmt.Fprintf(&b, "[%d] chunk_id=%s source_type=%s\n%s\n\n",
			i+1, c.ChunkID, c.SourceType, c.Text)
	}
	b.WriteString("Return the JSON verdict for whether every factual claim in the assistant answer is supported by the cited chunks.")
	return b.String()
}

// CheckGrounding invokes the judge model and returns Allow=true when
// the verdict is grounded, or a grounding_failed refusal when it's
// not. Network errors fail open — return Allow=true with the error
// stamped on Detail so qa.go can decide whether to surface it. (Failing
// open on judge transport errors is the deliberate cost-vs-safety
// tradeoff per planning round: a transient outage on the judge model
// should not refuse legitimate answers.)
//
// LogLLMCall fires inside this function with site=qa_grounding_judge
// so the judge call is independently visible in admin telemetry. The
// caller does NOT need to log on a successful judge round.
func CheckGrounding(ctx context.Context, question, answer string, citedChunks []JudgeChunk) OutputDecision {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		// No key configured = no judge = fail open. Same behavior as
		// the main qa.go path when the key is missing.
		return OutputDecision{Allow: true, Detail: "OPENAI_API_KEY not set; judge skipped"}
	}

	baseURL := strings.TrimSpace(os.Getenv("OPENAI_BASE_URL"))
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1/"
	}
	if baseURL[len(baseURL)-1] != '/' {
		baseURL += "/"
	}
	client := openai.NewClient(option.WithAPIKey(apiKey), option.WithBaseURL(baseURL))

	model := JudgeModelName()
	userPrompt := formatJudgeUserPrompt(question, answer, citedChunks)

	params := openai.ChatCompletionNewParams{
		Model: openai.ChatModel(model),
		Messages: []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(groundingSystemPrompt),
			openai.UserMessage(userPrompt),
		},
	}
	rf := shared.NewResponseFormatJSONObjectParam()
	params.ResponseFormat = openai.ChatCompletionNewParamsResponseFormatUnion{OfJSONObject: &rf}

	llmStart := time.Now()
	resp, err := client.Chat.Completions.New(ctx, params)
	if err != nil {
		// Fail open on judge transport error.
		return OutputDecision{Allow: true, Detail: fmt.Sprintf("judge transport error: %v", err)}
	}
	if len(resp.Choices) == 0 {
		return OutputDecision{Allow: true, Detail: "judge returned no choices"}
	}

	LogLLMCall(ctx, LLMCallRow{
		Site:         "qa_grounding_judge",
		Model:        model,
		PromptHash:   HashPrompt("qa_grounding_judge", groundingSystemPrompt+"\x00"+userPrompt),
		InputTokens:  IntPtr(int(resp.Usage.PromptTokens)),
		OutputTokens: IntPtr(int(resp.Usage.CompletionTokens)),
		LatencyMS:    int(time.Since(llmStart).Milliseconds()),
		Decision:     "allowed",
	})

	content := strings.TrimSpace(resp.Choices[0].Message.Content)
	// Tolerate markdown fences in case the model adds them despite the
	// json_object response_format hint.
	if strings.HasPrefix(content, "```json") {
		content = strings.TrimPrefix(content, "```json")
		content = strings.TrimSuffix(content, "```")
		content = strings.TrimSpace(content)
	} else if strings.HasPrefix(content, "```") {
		content = strings.TrimPrefix(content, "```")
		content = strings.TrimSuffix(content, "```")
		content = strings.TrimSpace(content)
	}

	var verdict judgeVerdict
	if err := json.Unmarshal([]byte(content), &verdict); err != nil {
		// Fail open on parse error: the judge returned malformed JSON,
		// which is a judge-model issue not an answer issue. Telemetry
		// already captured the call via LogLLMCall above.
		return OutputDecision{Allow: true, Detail: fmt.Sprintf("judge parse error: %v", err)}
	}

	if verdict.Grounded {
		return OutputDecision{Allow: true, Detail: verdict.Rationale}
	}
	return OutputDecision{
		Guardrail:   GuardrailGroundingFailed,
		Code:        GuardrailGroundingFailed,
		UserMessage: UserMessageGroundingFailed,
		Detail:      verdict.Rationale,
	}
}
