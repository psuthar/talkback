// SCRUM-567 (Slice 4c of SCRUM-560): PII scrubber. Runs over the
// free-text answer field from qa.go AFTER the grounding judge
// (Slice 4b) allows the response. Silent redaction by design — the
// user still gets an answer; the redacted substrings are replaced
// with `[redacted-<type>]` markers. Refusal is not appropriate here:
// a chunk that legitimately contains a participant's email shouldn't
// block the answer, just sanitize it.
//
// Logging: the caller (qa.go) emits a single LogLLMCall row with
// `guardrails_fired=[pii_redacted]` and `decision=allowed` — counts
// per type are surfaced via the returned ScrubResult map, but
// **values are never logged** (that would re-create the PII leak the
// scrubber exists to prevent).
//
// Env: `GUARDRAIL_PII_SCRUB` (default `on`). When `off`, ScrubText is
// a no-op that returns the input unchanged. Operational escape hatch
// for the rare case where a deliberate PII echo is needed (e.g.
// debugging session-templates pipelines).
package guardrails

import (
	"os"
	"regexp"
	"strings"
)

// Redaction marker constants. Kept stable so admin tooling /
// regression tests can grep for them.
const (
	RedactedEmail = "[redacted-email]"
	RedactedPhone = "[redacted-phone]"
	RedactedSSN   = "[redacted-ssn]"
)

// PIIGuardrailsFiredSlug is the value stamped on
// LLMCallRow.GuardrailsFired when any redaction fired. Enum addition
// tracked in docs/guardrails/log-shape.md.
const PIIGuardrailsFiredSlug = "pii_redacted"

// SchemaValidationFailedSlug is stamped on
// LLMCallRow.GuardrailsFired when the action-items schema validation
// failed twice (the SCRUM-567 retry-then-drop path). The dropped
// record is replaced with an empty / low_signal extraction; the
// caller does NOT crash. Decision stays "allowed" — this is a silent
// quality degradation, not a refusal.
const SchemaValidationFailedSlug = "schema_validation_failed"

// piiEmail is an RFC-5322-simplified email regex. Avoids being so
// strict that it misses participant addresses while still rejecting
// trailing punctuation (`mailto:alex@x.com.`-style false-positives
// where the period belongs to the surrounding sentence).
var piiEmail = regexp.MustCompile(`[a-zA-Z0-9_.+\-]+@[a-zA-Z0-9\-]+(?:\.[a-zA-Z0-9\-]+)+`)

// piiPhone matches:
//   - US 10-digit with optional country code: +1 (415) 555-1212, 415-555-1212, (415) 555 1212
//   - E.164: +44 7911 123456 etc.
//
// Designed to be tight: requires non-digit boundary and at least one
// punctuation separator so we don't mangle session-IDs or chunk_ids
// (which are often long digit-runs).
var piiPhone = regexp.MustCompile(`(?:\+?1[\s\-.]?)?\(?\b\d{3}\b\)?[\s\-.]\d{3}[\s\-.]\d{4}\b|\+\d{1,3}[\s\-.]\d{2,4}[\s\-.]\d{3,4}[\s\-.]?\d{0,4}`)

// piiSSN matches NNN-NN-NNNN. Tight on the boundary; will not match
// 123456789 by accident (which would be a generic 9-digit ID).
var piiSSN = regexp.MustCompile(`\b\d{3}-\d{2}-\d{4}\b`)

// ScrubResult is the per-type tally of redactions ScrubText performed.
// Caller logs the counts (NOT the values) via LogLLMCall.GuardrailsFired.
// Empty map = no redactions.
type ScrubResult map[string]int

// Total returns the sum of all redactions across types — a convenience
// for callers that just want a single number for the "did anything
// fire?" decision.
func (r ScrubResult) Total() int {
	t := 0
	for _, v := range r {
		t += v
	}
	return t
}

// ScrubText replaces all matches of the supported PII regexes in `s`
// with the `[redacted-<type>]` markers above. Returns the scrubbed
// string and a ScrubResult tallying matches per type.
//
// When GUARDRAIL_PII_SCRUB=off, returns (s, nil) unchanged.
//
// Pure-Go regex, no I/O, no LLM. Cheap enough to run on every QA
// response without observable latency impact.
func ScrubText(s string) (string, ScrubResult) {
	if strings.EqualFold(os.Getenv("GUARDRAIL_PII_SCRUB"), "off") {
		return s, nil
	}
	counts := ScrubResult{}

	if matches := piiEmail.FindAllStringIndex(s, -1); len(matches) > 0 {
		counts["email"] = len(matches)
		s = piiEmail.ReplaceAllString(s, RedactedEmail)
	}
	if matches := piiPhone.FindAllStringIndex(s, -1); len(matches) > 0 {
		counts["phone"] = len(matches)
		s = piiPhone.ReplaceAllString(s, RedactedPhone)
	}
	if matches := piiSSN.FindAllStringIndex(s, -1); len(matches) > 0 {
		counts["ssn"] = len(matches)
		s = piiSSN.ReplaceAllString(s, RedactedSSN)
	}

	if len(counts) == 0 {
		return s, nil
	}
	return s, counts
}
