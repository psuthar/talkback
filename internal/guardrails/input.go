// SCRUM-564 (Slice 3 of SCRUM-560): input guardrails. Runs before any
// LLM call on ask_session_question (HTTP + MCP). Three checks fire in
// order with first-match wins so the most specific reason surfaces:
//
//  1. length cap (input_too_long)
//  2. prompt-injection patterns (input_injection)
//  3. off-scope / non-session-question heuristics (input_off_scope)
//
// On any non-allow, the caller emits the structured refusal shape from
// docs/guardrails/refusal-shape.md and a guardrails.LogLLMCall row with
// site=qa_ask, decision=refused, refusal_code=<the slug>. The refusal
// user_message for input_injection is contract-locked verbatim by
// refusal-shape.md.
//
// The off-scope heuristic intentionally lets ambiguous strings through
// — it targets the obvious wins (shell commands, SQL/XSS payloads,
// bare greetings, "translate this", arithmetic questions) and trusts
// the downstream RAG path to return not_covered for fuzzier off-topic
// questions. The qa-eval refusal_when_oos_rate metric tracks the
// false-positive rate against eval/qa/fixture_input_guardrail.json
// so over-blocking shows up as a baseline regression.
package guardrails

import (
	"regexp"
	"strings"
)

// MaxQuestionLengthBytes is the input-too-long cap. 2 KiB matches the
// "default 2 KiB" specified in the SCRUM-564 description; tunable via
// CheckQuestionWithLimit for the rare caller that needs a different cap.
const MaxQuestionLengthBytes = 2048

// Refusal slugs / codes. Kept in sync with the guardrails_fired enum in
// docs/guardrails/log-shape.md and the catalog in
// docs/guardrails/refusal-shape.md.
const (
	GuardrailInputInjection = "input_injection"
	GuardrailInputOffScope  = "input_off_scope"
	GuardrailInputTooLong   = "input_too_long"
)

// Contract-locked user messages from docs/guardrails/refusal-shape.md.
// input_injection is verbatim per planning round — do not paraphrase.
const (
	UserMessageInputInjection = "Question detected to have unsafe content — not processed."
	UserMessageInputOffScope  = "Question is outside the scope of session content. Please ask about meeting topics, decisions, or action items."
	UserMessageInputTooLong   = "Question is too long. Please shorten it to under 2 KB."
)

// InputDecision is the internal result type for CheckQuestion. Callers
// translate Allow=false into a RefusalShape via Refusal().
type InputDecision struct {
	// Allow is true when the question may proceed to the LLM. When false,
	// the other fields describe which guardrail fired.
	Allow bool

	// Guardrail is the slug (input_injection / input_off_scope /
	// input_too_long). Empty when Allow=true.
	Guardrail string

	// Code is the stable refusal code (1:1 with Guardrail today; reserved
	// as a separate field for future sub-codes per refusal-shape.md).
	// Empty when Allow=true.
	Code string

	// UserMessage is the locale-neutral string the UI shows. Empty when
	// Allow=true.
	UserMessage string

	// Detail is a short human-readable reason for telemetry (e.g. the
	// pattern label that matched). Not surfaced to the user. Empty when
	// Allow=true.
	Detail string
}

// RefusalShape is the JSON body returned to the caller when a guardrail
// blocks. Mirrors the contract in docs/guardrails/refusal-shape.md:
// {"error":"guardrail_blocked","guardrail":...,"code":...,"user_message":...}.
// HTTP status is 200 (deliberate refusal != request error); MCP returns
// the same JSON as tool-result content.
type RefusalShape struct {
	Error       string `json:"error"`
	Guardrail   string `json:"guardrail"`
	Code        string `json:"code"`
	UserMessage string `json:"user_message"`
}

// Refusal converts a non-Allow decision into the wire-format refusal.
// Calling Refusal on an Allow=true decision returns the zero value.
func (d InputDecision) Refusal() RefusalShape {
	if d.Allow {
		return RefusalShape{}
	}
	return RefusalShape{
		Error:       "guardrail_blocked",
		Guardrail:   d.Guardrail,
		Code:        d.Code,
		UserMessage: d.UserMessage,
	}
}

// CheckQuestion is the public entry point. Runs the three checks
// described in the package comment with first-match-wins. The input is
// trimmed of leading/trailing whitespace before checks run (callers
// typically pre-trim, but we re-trim to be defensive).
func CheckQuestion(q string) InputDecision {
	return CheckQuestionWithLimit(q, MaxQuestionLengthBytes)
}

// CheckQuestionWithLimit is CheckQuestion with an explicit byte cap.
// Exported for tests + the rare caller (none today) that needs a
// non-default limit.
func CheckQuestionWithLimit(q string, maxBytes int) InputDecision {
	trimmed := strings.TrimSpace(q)

	if maxBytes > 0 && len(trimmed) > maxBytes {
		return InputDecision{
			Guardrail:   GuardrailInputTooLong,
			Code:        GuardrailInputTooLong,
			UserMessage: UserMessageInputTooLong,
			Detail:      "length cap exceeded",
		}
	}

	if label := detectInjection(trimmed); label != "" {
		return InputDecision{
			Guardrail:   GuardrailInputInjection,
			Code:        GuardrailInputInjection,
			UserMessage: UserMessageInputInjection,
			Detail:      label,
		}
	}

	if label := detectOffScope(trimmed); label != "" {
		return InputDecision{
			Guardrail:   GuardrailInputOffScope,
			Code:        GuardrailInputOffScope,
			UserMessage: UserMessageInputOffScope,
			Detail:      label,
		}
	}

	return InputDecision{Allow: true}
}

// injectionPatterns are curated from docs/guardrails/threat-model.md
// mode 1 and the labeled injection set in
// eval/qa/fixture_input_guardrail.json. Two design rules:
//
//   - Patterns require an "instruction-shaped" noun next to the trigger
//     verb. Plain English with the same verb ("ignore previous
//     deadlines and reset the timeline" — IG-041 / legitimate) must
//     pass. "Ignore previous instructions" must not.
//   - Patterns are case-insensitive and operate on the trimmed string;
//     they do not anchor to start/end so an injection placed mid-string
//     still fires.
//
// New pattern? Add a row here, add a fixture case to
// eval/qa/fixture_input_guardrail.json under "injection" with a
// distinctive case_id, and re-run the input-guardrail eval to confirm
// no regression on the legitimate set.
var injectionPatterns = []struct {
	label string
	re    *regexp.Regexp
}{
	{
		label: "ignore-previous-instructions",
		re: regexp.MustCompile(
			`(?i)\b(?:ignore|disregard|forget|override|bypass)\b[\s\S]{0,40}?\b(?:all|the|any)?[\s\S]{0,8}?\b(?:previous|prior|above|earlier|safety|prompt|system)\b[\s\S]{0,40}?\b(?:instructions?|prompts?|rules?|orders?|messages?|context|directives?|guidelines?|policies)\b`,
		),
	},
	{
		label: "system-role-spoof",
		re: regexp.MustCompile(
			`(?i)\bsystem\s*:\s*you\s+are\s+now\b`,
		),
	},
	{
		label: "system-override-marker",
		re: regexp.MustCompile(
			`(?i)#{2,}\s*system\s*override\s*#{0,}`,
		),
	},
	{
		label: "reveal-system-prompt",
		re: regexp.MustCompile(
			`(?i)\b(?:reveal|dump|print|show|output|leak|expose)\b[\s\S]{0,40}?\b(?:the\s+|your\s+)?(?:full\s+|raw\s+|verbatim\s+)?system\s+prompt\b`,
		),
	},
	{
		label: "developer-impersonation",
		re: regexp.MustCompile(
			`(?i)\bi\s+am\s+(?:the\s+)?(?:developer|admin|root|owner|sysadmin)\b[\s\S]{0,60}?\b(?:show|reveal|print|dump|output)\b[\s\S]{0,40}?\b(?:system\s+prompt|hidden|internal)\b`,
		),
	},
	{
		label: "you-are-now-mode",
		re: regexp.MustCompile(
			`(?i)\byou\s+are\s+now\s+in\s+(?:debug|admin|root|developer|jailbreak|dan|unrestricted)\s+mode\b`,
		),
	},
	{
		label: "user-content-marker-spoof",
		re: regexp.MustCompile(
			`(?i)<{2,}\s*(?:user_content|end_user_content|system_content|tool_content)\b`,
		),
	},
	{
		label: "start-your-reply-with",
		re: regexp.MustCompile(
			`(?i)\bstart\s+(?:your|the)\s+(?:reply|response|answer|output)\s+with\b`,
		),
	},
	{
		label: "override-print-output",
		re: regexp.MustCompile(
			`(?i)\b(?:override|debug|admin)\s*:\s*(?:print|output|reveal|show|dump|leak)\b`,
		),
	},
	{
		label: "list-your-tools",
		re: regexp.MustCompile(
			`(?i)\b(?:tell\s+me|reveal|show\s+me|list)\b[\s\S]{0,20}?\bwhat\b[\s\S]{0,20}?\btools\s+you\s+(?:have|can|access)\b`,
		),
	},
	{
		label: "read-passwd",
		re: regexp.MustCompile(
			`(?i)\bprint\s+(?:the\s+)?contents?\s+of\s+/etc/passwd\b`,
		),
	},
}

// offScopePatterns target obvious non-session inputs. Each predicate
// returns a non-empty label when it matches. Falls through to allow
// when nothing matches — the RAG path will surface not_covered for
// fuzzier off-topic questions.
type offScopeRule struct {
	label string
	match func(trimmed string) bool
}

var offScopeRules = []offScopeRule{
	{
		label: "bare-greeting",
		match: func(s string) bool {
			low := strings.ToLower(s)
			switch low {
			case "hello", "hi", "hey", "yo", "sup", "test", "testing":
				return true
			}
			return false
		},
	},
	{
		label: "shell-command",
		match: regexpMatch(`(?i)^\s*(?:sudo\s+)?(?:rm|cd|ls|cat|chmod|chown|kill|mkdir|touch|sh|bash|zsh|nc|curl|wget|ssh|scp|dd|mount|umount)\s+(?:-[a-zA-Z]|/|\.\.|~)`),
	},
	{
		label: "path-traversal",
		match: regexpMatch(`(?i)(?:\.\./){2,}|\b/etc/passwd\b|\b/etc/shadow\b`),
	},
	{
		label: "sql-injection",
		match: regexpMatch(`(?i)(?:'\s*or\s*'?\d+'?\s*=\s*'?\d+|\bunion\s+select\b|\bselect\s+\*\s+from\b|\bdrop\s+table\b|--\s*$)`),
	},
	{
		label: "xss-payload",
		match: regexpMatch(`(?i)<\s*script\b|\bjavascript\s*:|<\s*iframe\b|on(?:error|load|click)\s*=`),
	},
	{
		label: "code-generation-request",
		match: regexpMatch(`(?i)\bwrite\s+(?:me\s+)?(?:a|the)\s+(?:python|bash|shell|sql|javascript|js|typescript|ts|ruby|go|rust|java|c\+\+|c#|php)\s+(?:script|code|program|function|class|snippet)\b`),
	},
	{
		label: "translate-request",
		match: regexpMatch(`(?i)^\s*translate\b[\s\S]{0,40}?(?::|to\s+(?:french|spanish|german|chinese|japanese|korean|italian|portuguese))`),
	},
	{
		label: "arithmetic-question",
		// "what is 2+2", "what is 5 * 7", optional trailing ?. Narrow on
		// purpose — "what is 2+2 of the proposed metrics?" wouldn't match.
		match: regexpMatch(`(?i)^\s*what\s+is\s+\d+\s*[+\-*/x]\s*\d+\s*\??\s*$`),
	},
	{
		label: "weather-question",
		match: regexpMatch(`(?i)^\s*what(?:'s|\s+is)\s+the\s+weather\b`),
	},
	{
		label: "tell-me-a-joke",
		match: regexpMatch(`(?i)^\s*tell\s+me\s+(?:a|another)\s+joke\b`),
	},
	{
		label: "db-enumeration",
		match: regexpMatch(`(?i)\bshow\s+me\s+(?:a\s+)?list\s+of\s+all\s+\w+\s+in\s+the\s+database\b`),
	},
}

func detectInjection(s string) string {
	if s == "" {
		return ""
	}
	for _, p := range injectionPatterns {
		if p.re.MatchString(s) {
			return p.label
		}
	}
	return ""
}

func detectOffScope(s string) string {
	if s == "" {
		return ""
	}
	for _, r := range offScopeRules {
		if r.match(s) {
			return r.label
		}
	}
	return ""
}

// regexpMatch compiles `pat` at init time (via MustCompile) and returns
// a closure suitable for offScopeRule.match. Keeps the rule table
// readable without a per-package init() block.
func regexpMatch(pat string) func(string) bool {
	re := regexp.MustCompile(pat)
	return func(s string) bool { return re.MatchString(s) }
}
