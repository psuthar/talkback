package prrisk

import (
	"strings"
	"testing"
)

func TestInterpretLowBand(t *testing.T) {
	r := Result{RiskScore: 10, RiskBand: "low"}
	interp := buildInterpretation(r)
	if interp == "" {
		t.Error("expected non-empty interpretation for low band")
	}
	if !strings.Contains(strings.ToLower(interp), "low") {
		t.Errorf("expected low-risk language in interpretation: %q", interp)
	}
}

func TestInterpretMediumBand(t *testing.T) {
	r := Result{RiskScore: 30, RiskBand: "medium"}
	interp := buildInterpretation(r)
	if interp == "" {
		t.Error("expected non-empty interpretation for medium band")
	}
	if !strings.Contains(strings.ToLower(interp), "medium") {
		t.Errorf("expected medium-risk language: %q", interp)
	}
}

func TestInterpretHighBand(t *testing.T) {
	r := Result{RiskScore: 60, RiskBand: "high"}
	interp := buildInterpretation(r)
	if !strings.Contains(strings.ToLower(interp), "high") {
		t.Errorf("expected high-risk language: %q", interp)
	}
}

func TestInterpretCriticalBand(t *testing.T) {
	r := Result{RiskScore: 85, RiskBand: "critical"}
	interp := buildInterpretation(r)
	if !strings.Contains(strings.ToLower(interp), "critical") {
		t.Errorf("expected critical-risk language: %q", interp)
	}
}

func TestInterpretScoreEmbedded(t *testing.T) {
	r := Result{RiskScore: 55, RiskBand: "high"}
	interp := buildInterpretation(r)
	if !strings.Contains(interp, "55") {
		t.Errorf("expected score embedded in interpretation: %q", interp)
	}
}

func TestInterpretFloorNoteIncludedWhenApplied(t *testing.T) {
	// When FloorApplied is true, the interpretation for non-low bands must contain
	// a note that mentions the floor raised the score.
	r := Result{
		RiskScore: 20,
		RiskBand:  "medium",
		ScoreMath: ScoreMath{
			FloorApplied:   true,
			NetBeforeFloor: 6,
			FinalScore:     20,
		},
	}
	interp := buildInterpretation(r)
	if !strings.Contains(interp, "floor") {
		t.Errorf("expected floor note in interpretation when FloorApplied=true, got: %q", interp)
	}
	// The note must reference both the pre-floor and final score values.
	if !strings.Contains(interp, "6") {
		t.Errorf("expected pre-floor score (6) in floor note, got: %q", interp)
	}
	if !strings.Contains(interp, "20") {
		t.Errorf("expected final score (20) in floor note, got: %q", interp)
	}
}

func TestInterpretFloorNoteAbsentWhenNotApplied(t *testing.T) {
	r := Result{
		RiskScore: 40,
		RiskBand:  "medium",
		ScoreMath: ScoreMath{
			FloorApplied:   false,
			NetBeforeFloor: 40,
			FinalScore:     40,
		},
	}
	interp := buildInterpretation(r)
	if strings.Contains(interp, "floor") {
		t.Errorf("expected no floor note when FloorApplied=false, got: %q", interp)
	}
}

func TestInterpretFloorNoteHighBand(t *testing.T) {
	r := Result{
		RiskScore: 50,
		RiskBand:  "high",
		ScoreMath: ScoreMath{
			FloorApplied:   true,
			NetBeforeFloor: 15,
			FinalScore:     50,
		},
	}
	interp := buildInterpretation(r)
	if !strings.Contains(strings.ToLower(interp), "high") {
		t.Errorf("expected high-band language: %q", interp)
	}
	if !strings.Contains(interp, "floor") {
		t.Errorf("expected floor note for high band with FloorApplied=true: %q", interp)
	}
}

func TestInterpretUnknownBandEmpty(t *testing.T) {
	r := Result{RiskScore: 0, RiskBand: ""}
	interp := buildInterpretation(r)
	if interp != "" {
		t.Errorf("expected empty interpretation for unknown band, got %q", interp)
	}
}
