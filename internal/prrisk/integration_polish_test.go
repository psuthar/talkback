package prrisk

import "testing"

func TestPolishRequiredValidationLine_TestProximityVsHotspot(t *testing.T) {
	gotProx := polishRequiredValidationLine("test: tests co-located or explicitly linked for changed code")
	wantProx := "Ensure test coverage is present or clearly linked for changed code"
	if gotProx != wantProx {
		t.Fatalf("proximity line: got %q want %q", gotProx, wantProx)
	}
	gotHot := polishRequiredValidationLine("test: targeted regression for high-churn area touched by diff")
	wantHot := "Run targeted regression for high-churn areas touched by this diff"
	if gotHot != wantHot {
		t.Fatalf("hotspot line: got %q want %q", gotHot, wantHot)
	}
}

func TestPolishRequiredValidationLine_CiLine(t *testing.T) {
	got := polishRequiredValidationLine("ci: required status checks must pass before merge")
	if got != "Required status checks must pass before merge" {
		t.Fatalf("got %q", got)
	}
}
