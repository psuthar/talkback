package prrisk

import (
	"strings"
	"testing"
)

func TestMergeRecommendationGitErrorBlocks(t *testing.T) {
	r := Result{
		RiskBand:  "low",
		RiskScore: 5,
		Signals:   Signals{GitError: "failed"},
	}
	if mergeRecommendation(r) != "block" {
		t.Fatalf("want block on git error, got %q", mergeRecommendation(r))
	}
}

func TestMergeRecommendationCriticalHighBlock(t *testing.T) {
	for _, band := range []string{"critical", "high"} {
		r := Result{RiskBand: band, RiskScore: 50, Signals: Signals{}}
		if mergeRecommendation(r) != "block" {
			t.Fatalf("band %s: want block, got %q", band, mergeRecommendation(r))
		}
	}
}

func TestMergeRecommendationMediumWarn(t *testing.T) {
	r := Result{RiskBand: "medium", RiskScore: 30, Signals: Signals{}}
	if mergeRecommendation(r) != "warn" {
		t.Fatalf("want warn, got %q", mergeRecommendation(r))
	}
}

func TestMergeRecommendationLowPass(t *testing.T) {
	r := Result{
		RiskBand:  "low",
		RiskScore: 10,
		Signals:   Signals{},
		Factors:   nil,
	}
	if mergeRecommendation(r) != "pass" {
		t.Fatalf("want pass, got %q", mergeRecommendation(r))
	}
}

func TestMergeRecommendationLowTestsMissingWarns(t *testing.T) {
	r := Result{
		RiskBand:  "low",
		RiskScore: 12,
		Signals:   Signals{},
		Factors:   []RiskFactor{{ID: "tests_missing", Points: 18}},
	}
	if mergeRecommendation(r) != "warn" {
		t.Fatalf("want warn when tests_missing in low band, got %q", mergeRecommendation(r))
	}
}

func TestReviewStrategyForBlock(t *testing.T) {
	s := reviewStrategyFor("block", "high")
	if !strings.Contains(s, "Do not merge") {
		t.Errorf("block strategy should say 'Do not merge', got %q", s)
	}
}

func TestReviewStrategyForWarnMedium(t *testing.T) {
	s := reviewStrategyFor("warn", "medium")
	if !strings.Contains(s, "checklist") {
		t.Errorf("warn+medium strategy should reference checklist, got %q", s)
	}
}

func TestReviewStrategyForPass(t *testing.T) {
	s := reviewStrategyFor("pass", "low")
	if !strings.Contains(s, "Single-pass") {
		t.Errorf("pass strategy should say 'Single-pass', got %q", s)
	}
}

func TestReviewStrategyForWarnNonMedium(t *testing.T) {
	s := reviewStrategyFor("warn", "low")
	if strings.Contains(s, "checklist") {
		t.Errorf("warn+non-medium should not reference checklist-driven review, got %q", s)
	}
}

func TestReviewRequirementsBlockAddsSignOff(t *testing.T) {
	reqs := reviewRequirements("block", "high", 55)
	found := false
	for _, r := range reqs {
		if strings.Contains(r, "sign-off") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("block recommendation should require explicit sign-off, got %v", reqs)
	}
}

func TestReviewRequirementsHighAddsReviewerPref(t *testing.T) {
	reqs := reviewRequirements("block", "high", 55)
	found := false
	for _, r := range reqs {
		if strings.Contains(r, "reviewer familiar") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("high band should prefer familiar reviewer, got %v", reqs)
	}
}

func TestReviewRequirementsScoreAbove45AddsCIGreen(t *testing.T) {
	reqs := reviewRequirements("warn", "medium", 50)
	found := false
	for _, r := range reqs {
		if strings.Contains(r, "CI is green") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("score>=45 non-pass should require CI green, got %v", reqs)
	}
}

func TestReviewRequirementsScoreBelow45NoCI(t *testing.T) {
	// Low-risk pass — no CI green requirement.
	reqs := reviewRequirements("pass", "low", 10)
	for _, r := range reqs {
		if strings.Contains(r, "CI is green") {
			t.Errorf("score<45 pass should not add CI green requirement, got %v", reqs)
		}
	}
}

func TestMergeRationaleIncludesBandAndScore(t *testing.T) {
	r := Result{RiskBand: "high", RiskScore: 55, Signals: Signals{}}
	rat := mergeRationale(r, "block")
	if !strings.Contains(rat, "high") {
		t.Errorf("rationale should mention band, got %q", rat)
	}
	if !strings.Contains(rat, "55") {
		t.Errorf("rationale should mention score, got %q", rat)
	}
}

func TestMergeRationaleGitError(t *testing.T) {
	r := Result{RiskBand: "low", RiskScore: 5, Signals: Signals{GitError: "no base"}}
	rat := mergeRationale(r, "block")
	if !strings.Contains(rat, "Git diff") {
		t.Errorf("git error rationale should mention 'Git diff', got %q", rat)
	}
}

func TestMergeRationaleWarnIncludesChecklist(t *testing.T) {
	r := Result{RiskBand: "medium", RiskScore: 30, Signals: Signals{}}
	rat := mergeRationale(r, "warn")
	if !strings.Contains(rat, "checklist") {
		t.Errorf("warn rationale should reference checklist, got %q", rat)
	}
}

func TestComputeEnforcementHasValidationsAndRouting(t *testing.T) {
	r := Result{
		RiskBand:  "medium",
		RiskScore: 30,
		Signals: Signals{
			DomainHits: map[string]int{DomainAuth: 1},
			FileCount:  3,
		},
		RequiredActions: []RequiredAction{
			{ID: "auth_e2e_gate", Title: "Auth"},
		},
		Factors: []RiskFactor{{ID: "domain_auth", Points: 14}},
	}
	e := ComputeEnforcement(r)
	if e.MergeRecommendation != "warn" {
		t.Fatalf("merge rec: %q", e.MergeRecommendation)
	}
	if len(e.RequiredValidations) < 2 {
		t.Fatalf("expected baseline + action validations, got %#v", e.RequiredValidations)
	}
	found := false
	for _, v := range e.RequiredValidations {
		if v == "ci: required status checks must pass before merge" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected baseline CI validation")
	}
	if len(e.RoutingHints) < 1 {
		t.Fatal("expected at least one routing hint for auth domain")
	}
}
