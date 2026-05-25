package guardrails

import (
	"strings"
	"testing"
)

// SCRUM-567 (Slice 4c of SCRUM-560): PII scrubber tests. Cover each
// regex pattern with both positive matches and negative-near-misses
// so the redaction can't accidentally over-match (eat real numbers in
// the answer text) or under-match (let an obvious email through).

func TestScrubText_EmailRedactsCommonFormats(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"Contact alex@example.com for details.", "Contact " + RedactedEmail + " for details."},
		{"Email user.name+tag@sub.example.co.uk.", "Email " + RedactedEmail + "."},
		{"Multiple: a@b.com and c@d.org.", "Multiple: " + RedactedEmail + " and " + RedactedEmail + "."},
	}
	for _, c := range cases {
		got, counts := ScrubText(c.in)
		if got != c.want {
			t.Errorf("ScrubText(%q): got %q, want %q", c.in, got, c.want)
		}
		if counts["email"] == 0 {
			t.Errorf("ScrubText(%q): email count should be > 0; got %v", c.in, counts)
		}
	}
}

func TestScrubText_EmailNegativesDontFire(t *testing.T) {
	// "@username" mentions (Twitter-style) and bare TLDs should not match.
	cases := []string{
		"Mention @alice and @bob in the doc.",
		"The TLD .com is short.",
		"Just text with @ at sign here.",
	}
	for _, c := range cases {
		got, counts := ScrubText(c)
		if counts["email"] > 0 {
			t.Errorf("ScrubText(%q) false-positive email; output=%q counts=%v", c, got, counts)
		}
	}
}

func TestScrubText_PhoneUSAndE164(t *testing.T) {
	cases := []struct {
		in   string
		want string // partial substring — phone replacement
	}{
		{"Call (415) 555-1212.", RedactedPhone},
		{"Call 415-555-1212 next week.", RedactedPhone},
		{"Call +1 415 555 1212 anytime.", RedactedPhone},
		{"International: +44 7911 123456.", RedactedPhone},
	}
	for _, c := range cases {
		got, counts := ScrubText(c.in)
		if !strings.Contains(got, c.want) {
			t.Errorf("ScrubText(%q): output should contain %q; got %q", c.in, c.want, got)
		}
		if counts["phone"] == 0 {
			t.Errorf("ScrubText(%q): phone count should be > 0; got %v", c.in, counts)
		}
	}
}

func TestScrubText_PhoneNegativesDontFire(t *testing.T) {
	// Long digit sequences like UUIDs / chunk_ids / dates should not
	// match the phone pattern.
	cases := []string{
		"chunk_id=abc-12345678 referenced.",
		"Year 2024 saw 12 launches.",
		"Score: 3-2-1 across rounds.", // looks vaguely SSN-like but only 3 digits in first segment
		"Order ID: 1234567890.",
	}
	for _, c := range cases {
		_, counts := ScrubText(c)
		if counts["phone"] > 0 {
			t.Errorf("ScrubText(%q) false-positive phone; counts=%v", c, counts)
		}
	}
}

func TestScrubText_SSN(t *testing.T) {
	in := "His SSN is 123-45-6789, for the record."
	got, counts := ScrubText(in)
	if !strings.Contains(got, RedactedSSN) {
		t.Errorf("ScrubText: output should contain %q; got %q", RedactedSSN, got)
	}
	if counts["ssn"] != 1 {
		t.Errorf("counts[ssn]=%d, want 1; got full counts=%v", counts["ssn"], counts)
	}
	// Negative: should not catch other dash-separated 3-2-4 patterns
	// that aren't 3-2-4-of-digits.
	_, c2 := ScrubText("Project AB-CD-EFGH was renamed.")
	if c2["ssn"] > 0 {
		t.Errorf("alphabetic dash pattern should not match SSN regex; counts=%v", c2)
	}
}

func TestScrubText_MixedRedactionsCounted(t *testing.T) {
	in := "Reach alex@example.com or call 415-555-1212. SSN: 999-00-1111."
	got, counts := ScrubText(in)
	if !strings.Contains(got, RedactedEmail) {
		t.Errorf("missing email redaction; got %q", got)
	}
	if !strings.Contains(got, RedactedPhone) {
		t.Errorf("missing phone redaction; got %q", got)
	}
	if !strings.Contains(got, RedactedSSN) {
		t.Errorf("missing ssn redaction; got %q", got)
	}
	if counts["email"] != 1 || counts["phone"] != 1 || counts["ssn"] != 1 {
		t.Errorf("counts unexpected: %v", counts)
	}
	if counts.Total() != 3 {
		t.Errorf("Total()=%d, want 3", counts.Total())
	}
}

func TestScrubText_NoRedactionsReturnsNilCounts(t *testing.T) {
	got, counts := ScrubText("Plain prose with nothing to redact.")
	if got != "Plain prose with nothing to redact." {
		t.Errorf("output should be unchanged; got %q", got)
	}
	if counts != nil {
		t.Errorf("counts should be nil on no-redaction path; got %v", counts)
	}
	if counts.Total() != 0 {
		t.Errorf("Total() on nil should be 0; got %d", counts.Total())
	}
}

func TestScrubText_EnvDisablesScrubbing(t *testing.T) {
	t.Setenv("GUARDRAIL_PII_SCRUB", "off")
	in := "Email alex@example.com and call 415-555-1212."
	got, counts := ScrubText(in)
	if got != in {
		t.Errorf("with GUARDRAIL_PII_SCRUB=off, output should be unchanged; got %q", got)
	}
	if counts != nil {
		t.Errorf("with scrub off, counts should be nil; got %v", counts)
	}
}

func TestScrubText_EnvOnIsTheDefault(t *testing.T) {
	t.Setenv("GUARDRAIL_PII_SCRUB", "") // empty = default = on
	got, _ := ScrubText("email alex@example.com.")
	if !strings.Contains(got, RedactedEmail) {
		t.Errorf("default (empty env) should scrub; got %q", got)
	}
}

func TestScrubText_DoesNotLeakRedactedValues(t *testing.T) {
	// Sanity: the original PII string must NOT appear in the output
	// after scrubbing. This is a tautology over the regex behavior,
	// but worth pinning so a future bug-fix that breaks the
	// replacement-text contract surfaces here.
	in := "Email super.specific.address@private.example.com confidentially."
	got, _ := ScrubText(in)
	if strings.Contains(got, "super.specific.address@private.example.com") {
		t.Errorf("redacted email still present in output: %q", got)
	}
}
