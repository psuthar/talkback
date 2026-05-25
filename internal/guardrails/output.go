// SCRUM-565 (Slice 4a of SCRUM-560): output guardrails — citation
// enforcement on RAG answers from ask_session_question. The cheapest
// output-side defense: an `answered` response that cites zero retrieved
// chunks is ungrounded by definition. On a citation-missing decision
// the caller does **one automatic retry** with a stricter system-prompt
// addendum (site=qa_ask_retry_citation), and only if the retry also
// returns zero valid citations does the response refuse with the
// shape from docs/guardrails/refusal-shape.md
// (guardrail=citation_missing, contract-locked user_message).
//
// `not_covered` answers are exempt — the LLM is explicitly allowed to
// say "I don't know" and the upstream pipeline already clears citations
// on not_covered. CheckCitations returns Allow=true for empty input
// when retrievedChunkIDs is also empty, so a 0-chunk session that
// produces a not_covered response doesn't trip the guardrail.
//
// SCRUM-566 (Slice 4b — grounding LLM-as-judge) will add an
// OutputDecisionGroundingFailed alongside this one; the package leaves
// room in the catalog below.
package guardrails

// Output guardrail slugs / codes. Kept in sync with the
// guardrails_fired enum in docs/guardrails/log-shape.md and the catalog
// in docs/guardrails/refusal-shape.md.
const (
	GuardrailCitationMissing = "citation_missing"
	GuardrailGroundingFailed = "grounding_failed" // reserved for SCRUM-566 (Slice 4b)
)

// Contract-locked output user messages from refusal-shape.md.
// citation_missing and grounding_failed deliberately share the same
// surface ("answer not verifiable") because the user-actionable
// response is identical; the `code` field preserves the distinction
// for telemetry.
const (
	UserMessageCitationMissing = "The answer could not be verified against session content."
	UserMessageGroundingFailed = "The answer could not be verified against session content."
)

// OutputDecision mirrors InputDecision for the output-side guardrails.
// Refusal() converts a non-Allow decision into the wire-format
// RefusalShape; Allow=true returns the zero value.
type OutputDecision struct {
	// Allow is true when the answer may be returned to the caller. When
	// false, the other fields describe which guardrail fired.
	Allow bool

	// Guardrail is the slug (citation_missing / grounding_failed).
	// Empty when Allow=true.
	Guardrail string

	// Code is the stable refusal code (1:1 with Guardrail today;
	// reserved as a separate field for future sub-codes per
	// refusal-shape.md). Empty when Allow=true.
	Code string

	// UserMessage is the locale-neutral string the UI shows. Empty when
	// Allow=true.
	UserMessage string

	// Detail is a short human-readable reason for telemetry (e.g.
	// "0 of 3 citations matched retrieved set"). Not surfaced to the
	// user. Empty when Allow=true.
	Detail string
}

// Refusal converts a non-Allow decision into the wire-format refusal.
// Calling Refusal on an Allow=true decision returns the zero value.
func (d OutputDecision) Refusal() RefusalShape {
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

// CheckCitations returns Allow=true when at least one element of
// citationChunkIDs is also in retrievedChunkIDs. Otherwise returns a
// citation_missing decision. Empty `citationChunkIDs` is always !Allow
// (no citation == ungrounded) unless `retrievedChunkIDs` is also empty
// (an empty-chunks RAG path produces not_covered upstream; treating
// that as Allow keeps the not_covered case symmetric).
//
// Per the SCRUM-565 description: "Input is the parsed citations[]
// field from the structured LLM response — not an answer-text parse,
// which qa.go doesn't produce." Callers pull citation IDs from
// `QAResponse.Citations[].ChunkID` before calling this.
func CheckCitations(citationChunkIDs []string, retrievedChunkIDs []string) OutputDecision {
	// Empty retrieved set → the upstream RAG path produced no candidates
	// to cite. The system path forces answer_status=not_covered with
	// empty citations in that case (see internal/utils/qa.go), so we
	// pass through here so the not_covered response isn't itself
	// refused. Callers that want strict no-empty-retrieval semantics
	// should check `len(retrievedChunkIDs) == 0` themselves before
	// invoking GenerateAnswer.
	if len(retrievedChunkIDs) == 0 {
		return OutputDecision{Allow: true}
	}

	retrieved := make(map[string]struct{}, len(retrievedChunkIDs))
	for _, id := range retrievedChunkIDs {
		if id == "" {
			continue
		}
		retrieved[id] = struct{}{}
	}

	for _, id := range citationChunkIDs {
		if id == "" {
			continue
		}
		if _, ok := retrieved[id]; ok {
			return OutputDecision{Allow: true}
		}
	}

	detail := "0 valid citations"
	if n := len(citationChunkIDs); n > 0 {
		detail = "0 of " + itoa(n) + " citations matched retrieved set"
	}
	return OutputDecision{
		Guardrail:   GuardrailCitationMissing,
		Code:        GuardrailCitationMissing,
		UserMessage: UserMessageCitationMissing,
		Detail:      detail,
	}
}

// itoa is a tiny local helper so output.go does not pull in strconv
// just for a single one-call usage in Detail.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
