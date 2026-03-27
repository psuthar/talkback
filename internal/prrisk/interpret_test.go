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

func TestInterpretUnknownBandEmpty(t *testing.T) {
	r := Result{RiskScore: 0, RiskBand: ""}
	interp := buildInterpretation(r)
	if interp != "" {
		t.Errorf("expected empty interpretation for unknown band, got %q", interp)
	}
}
