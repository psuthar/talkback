package prrisk

import "testing"

func TestPolishRequiredValidationLine_TestProximityVsHotspot(t *testing.T) {
	gotProx := polishRequiredValidationLine("test: tests co-located or explicitly linked for changed code")
	wantProx := "Ensure test coverage is present or clearly linked for changed code"
	if gotProx != wantProx {
		t.Fatalf("proximity line: got %q want %q", gotProx, wantProx)
	}
	wantHot := "Run targeted regression for path prefixes with sustained recent commit activity"
	for _, raw := range []string{
		"test: targeted regression for high-churn area touched by diff",
		"test: targeted regression for path prefixes with several recent commits overlapping this diff",
	} {
		if got := polishRequiredValidationLine(raw); got != wantHot {
			t.Fatalf("hotspot line %q: got %q want %q", raw, got, wantHot)
		}
	}
}

func TestPolishRequiredValidationLine_CiLine(t *testing.T) {
	got := polishRequiredValidationLine("ci: required status checks must pass before merge")
	if got != "Required status checks must pass before merge" {
		t.Fatalf("got %q", got)
	}
}
