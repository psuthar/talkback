// Package guardrails is the GenAI input/output validation layer for TalkBack.
// SCRUM-560 epic; this file is Slice 5 (SCRUM-568) — the per-call telemetry
// type + canonical prompt-hash. Contracts:
//   - docs/guardrails/log-shape.md  (LLMCallRow shape + enum values)
//   - docs/guardrails/refusal-shape.md  (decision codes + user_message catalog)
package guardrails

import (
	"crypto/sha256"
	"encoding/hex"
	"time"

	"github.com/google/uuid"
)

// LLMCallRow is the per-LLM-call telemetry row written via LogLLMCall.
// Field semantics + enum values live in docs/guardrails/log-shape.md.
type LLMCallRow struct {
	ID                 uuid.UUID
	TS                 time.Time
	Site               string
	Model              string
	UserID             *uuid.UUID
	SessionID          *uuid.UUID
	PromptHash         string
	InputTokens        *int
	OutputTokens       *int
	LatencyMS          int
	GuardrailsFired    []string
	Decision           string
	RefusalCode        *string
	RefusalUserMessage *string
}

// HashPrompt is the canonical prompt_hash computation per SCRUM-569 Gap 5
// fix in log-shape.md: sha256(site || "\x00" || full_prompt), hex. The
// site-tag salt keeps original + retry rows on the same question from
// colliding while same-question-same-site repeats still de-dup.
func HashPrompt(site, fullPrompt string) string {
	h := sha256.New()
	h.Write([]byte(site))
	h.Write([]byte{0})
	h.Write([]byte(fullPrompt))
	return hex.EncodeToString(h.Sum(nil))
}
