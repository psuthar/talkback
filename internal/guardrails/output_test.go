package guardrails

import (
	"encoding/json"
	"strings"
	"testing"
)

// SCRUM-565 (Slice 4a of SCRUM-560): unit tests for CheckCitations
// + OutputDecision.Refusal() round-trip against the contract.

func TestCheckCitations_AllValidAllowsAnswer(t *testing.T) {
	d := CheckCitations(
		[]string{"chunk-A", "chunk-B"},
		[]string{"chunk-A", "chunk-B", "chunk-C"},
	)
	if !d.Allow {
		t.Fatalf("expected Allow, got refusal guardrail=%s detail=%s", d.Guardrail, d.Detail)
	}
}

func TestCheckCitations_AtLeastOneValidAllowsAnswer(t *testing.T) {
	// Mixed: one good, one hallucinated. CheckCitations is a "must have
	// at least one grounded citation" gate, not "all citations must be
	// grounded" — citation_id normalization elsewhere catches the
	// hallucinated row when it's rendered to the user.
	d := CheckCitations(
		[]string{"chunk-A", "hallucinated-id"},
		[]string{"chunk-A", "chunk-B"},
	)
	if !d.Allow {
		t.Fatalf("expected Allow when ≥1 citation is grounded, got refusal detail=%s", d.Detail)
	}
}

func TestCheckCitations_AllHallucinatedRefuses(t *testing.T) {
	d := CheckCitations(
		[]string{"fake-1", "fake-2"},
		[]string{"chunk-A", "chunk-B"},
	)
	if d.Allow {
		t.Fatalf("expected refusal when all citations are hallucinated, got Allow")
	}
	if d.Guardrail != GuardrailCitationMissing {
		t.Errorf("guardrail=%s, want %s", d.Guardrail, GuardrailCitationMissing)
	}
	if d.UserMessage != UserMessageCitationMissing {
		t.Errorf("user_message=%q is not the contract-locked string", d.UserMessage)
	}
	if !strings.Contains(d.Detail, "0 of 2") {
		t.Errorf("Detail should report 0 of 2 matched; got %q", d.Detail)
	}
}

func TestCheckCitations_NoCitationsRefuses(t *testing.T) {
	d := CheckCitations(
		[]string{},
		[]string{"chunk-A"},
	)
	if d.Allow {
		t.Fatalf("expected refusal on zero citations, got Allow")
	}
	if d.Guardrail != GuardrailCitationMissing {
		t.Errorf("guardrail=%s, want %s", d.Guardrail, GuardrailCitationMissing)
	}
	if d.Detail != "0 valid citations" {
		t.Errorf("Detail=%q, want %q", d.Detail, "0 valid citations")
	}
}

func TestCheckCitations_NoRetrievedAllowsNotCoveredFallthrough(t *testing.T) {
	// not_covered upstream path: 0 retrieved chunks → upstream forces
	// not_covered with empty citations. The guardrail must not refuse
	// in this case (the LLM was never asked to ground anything).
	d := CheckCitations([]string{}, []string{})
	if !d.Allow {
		t.Fatalf("expected Allow when retrieved set is empty (not_covered path), got refusal detail=%s", d.Detail)
	}
}

func TestCheckCitations_EmptyStringsAreIgnored(t *testing.T) {
	// Defensive: a citation row missing chunk_id (filled with "" by
	// the citation-normalization code on its own error path) must not
	// count as grounded just because the retrieved list also has an
	// empty string somewhere.
	d := CheckCitations(
		[]string{""},
		[]string{"chunk-A", ""},
	)
	if d.Allow {
		t.Fatalf("empty-string citations should not match empty-string retrieved ids; got Allow")
	}
}

func TestCheckCitations_DuplicatesInRetrievedDontPanic(t *testing.T) {
	// Defensive: chunkMap dedupe in CheckCitations should tolerate dupes.
	d := CheckCitations(
		[]string{"chunk-A"},
		[]string{"chunk-A", "chunk-A", "chunk-B"},
	)
	if !d.Allow {
		t.Fatalf("expected Allow with duplicated retrieved id, got refusal detail=%s", d.Detail)
	}
}

func TestOutputDecision_Refusal_ShapeMatchesContract(t *testing.T) {
	d := CheckCitations([]string{}, []string{"chunk-A"})
	r := d.Refusal()
	if r.Error != "guardrail_blocked" {
		t.Errorf("error=%q, want guardrail_blocked", r.Error)
	}
	if r.Guardrail != "citation_missing" {
		t.Errorf("guardrail=%q, want citation_missing", r.Guardrail)
	}
	if r.Code != "citation_missing" {
		t.Errorf("code=%q, want citation_missing", r.Code)
	}
	if r.UserMessage != UserMessageCitationMissing {
		t.Errorf("user_message=%q does not match contract verbatim", r.UserMessage)
	}

	raw, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(raw)
	for _, sub := range []string{
		`"error":"guardrail_blocked"`,
		`"guardrail":"citation_missing"`,
		`"code":"citation_missing"`,
		`"user_message":"The answer could not be verified against session content."`,
	} {
		if !strings.Contains(got, sub) {
			t.Errorf("JSON missing %q; got %s", sub, got)
		}
	}
}

func TestOutputDecision_Refusal_ZeroOnAllow(t *testing.T) {
	d := OutputDecision{Allow: true}
	if r := d.Refusal(); (r != RefusalShape{}) {
		t.Errorf("expected zero RefusalShape for Allow=true, got %+v", r)
	}
}

func TestItoa_RoundTripsSmallIntegers(t *testing.T) {
	// itoa is a private helper but worth a smoke test so the Detail
	// formatting doesn't silently regress.
	cases := map[int]string{
		0:    "0",
		1:    "1",
		5:    "5",
		10:   "10",
		99:   "99",
		1234: "1234",
		-7:   "-7",
	}
	for in, want := range cases {
		if got := itoa(in); got != want {
			t.Errorf("itoa(%d)=%q, want %q", in, got, want)
		}
	}
}
