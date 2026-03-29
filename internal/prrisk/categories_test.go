package prrisk

import (
	"testing"

	riskcontext "github.com/psuthar/talkback/internal/prrisk/context"
)

// TestCategoriesCodeFactorsInCodeLane verifies code-domain factors land in the code lane.
func TestCategoriesCodeFactorsInCodeLane(t *testing.T) {
	s := Signals{DomainHits: map[string]int{DomainAuth: 1}}
	factors := []RiskFactor{
		{ID: "domain_auth", Points: 14},
		{ID: "diff_large", Points: 12},
	}
	cats := ComputeCategories(s, factors, nil, nil)
	code := findCategory(cats, CategoryCode)
	if code == nil {
		t.Fatal("expected code category")
	}
	if code.RiskScore <= 0 {
		t.Error("expected positive code risk score")
	}
	if !containsStr(code.Factors, "domain_auth") {
		t.Error("expected domain_auth in code factors")
	}
	if !containsStr(code.Factors, "diff_large") {
		t.Error("expected diff_large in code factors")
	}
}

// TestCategoriesWorkflowFactorsInWorkflowLane verifies CI/deploy factors land in the workflow lane.
func TestCategoriesWorkflowFactorsInWorkflowLane(t *testing.T) {
	s := Signals{DomainHits: map[string]int{DomainWorkflows: 1}}
	factors := []RiskFactor{{ID: "ci_workflows", Points: 12}}
	cats := ComputeCategories(s, factors, nil, nil)
	wf := findCategory(cats, CategoryWorkflow)
	if wf == nil {
		t.Fatal("expected workflow category")
	}
	if wf.RiskScore <= 0 {
		t.Error("expected positive workflow risk score")
	}
	if !containsStr(wf.Factors, "ci_workflows") {
		t.Error("expected ci_workflows in workflow factors")
	}
}

// TestCategoriesWorkflowReducerLowersLaneScore verifies a workflow-lane reducer
// subtracts from the workflow lane score independently of the overall score.
func TestCategoriesWorkflowReducerLowersLaneScore(t *testing.T) {
	s := Signals{DomainHits: map[string]int{DomainWorkflows: 1}}
	factors := []RiskFactor{{ID: "ci_workflows", Points: 12}}
	reducers := []RiskReducer{
		{ID: "validation_note_present", Points: 10, CategoryKey: CategoryWorkflow},
	}
	cats := ComputeCategories(s, factors, reducers, nil)
	wf := findCategory(cats, CategoryWorkflow)
	if wf == nil {
		t.Fatal("expected workflow category")
	}
	// 12 points − 10 reducer = 2
	if wf.RiskScore != 2 {
		t.Errorf("expected workflow risk 2 after reducer, got %.1f", wf.RiskScore)
	}
	if !containsStr(wf.Reducers, "validation_note_present") {
		t.Error("expected validation_note_present in workflow reducers")
	}
}

// TestCategoriesTestConfidenceSensitiveNoTests verifies low confidence (25)
// when sensitive domains changed with no test files in the diff.
func TestCategoriesTestConfidenceSensitiveNoTests(t *testing.T) {
	s := Signals{
		DomainHits: map[string]int{DomainAuth: 1},
		TestFiles:  0,
	}
	cats := ComputeCategories(s, nil, nil, nil)
	tc := findCategory(cats, CategoryTestConfidence)
	if tc == nil {
		t.Fatal("expected test_confidence category")
	}
	// testConfidenceScore: sensitiveChanged=true, no tests → 25
	// risk = 100 - 25 = 75
	if tc.Confidence != 25 {
		t.Errorf("expected confidence 25, got %.0f", tc.Confidence)
	}
	if tc.RiskScore != 75 {
		t.Errorf("expected risk 75, got %.1f", tc.RiskScore)
	}
}

// TestCategoriesTestConfidenceSensitiveWithE2E verifies higher confidence (80)
// when E2E tests are present alongside sensitive domain changes.
func TestCategoriesTestConfidenceSensitiveWithE2E(t *testing.T) {
	s := Signals{
		DomainHits:   map[string]int{DomainAuth: 1},
		E2ETestFiles: 1,
		TestFiles:    1,
	}
	cats := ComputeCategories(s, nil, nil, nil)
	tc := findCategory(cats, CategoryTestConfidence)
	if tc == nil {
		t.Fatal("expected test_confidence category")
	}
	// testConfidenceScore: sensitiveChanged=true, E2ETestFiles > 0 → 80
	if tc.Confidence != 80 {
		t.Errorf("expected confidence 80, got %.0f", tc.Confidence)
	}
}

// TestCategoriesTestConfidenceNotSensitive verifies high confidence (85)
// when no sensitive domains were changed.
func TestCategoriesTestConfidenceNotSensitive(t *testing.T) {
	s := Signals{DomainHits: map[string]int{DomainWeb: 1}}
	cats := ComputeCategories(s, nil, nil, nil)
	tc := findCategory(cats, CategoryTestConfidence)
	if tc == nil {
		t.Fatal("expected test_confidence category")
	}
	// testConfidenceScore: sensitiveChanged=false → 85; risk = 15
	if tc.Confidence != 85 {
		t.Errorf("expected confidence 85, got %.0f", tc.Confidence)
	}
	if tc.RiskScore != 15 {
		t.Errorf("expected risk 15, got %.1f", tc.RiskScore)
	}
}

// TestCategoriesAllThreeLanesPresent verifies all three category lanes are always emitted.
func TestCategoriesAllThreeLanesPresent(t *testing.T) {
	s := Signals{DomainHits: map[string]int{}}
	cats := ComputeCategories(s, nil, nil, nil)
	for _, key := range []string{CategoryCode, CategoryWorkflow, CategoryTestConfidence} {
		if findCategory(cats, key) == nil {
			t.Errorf("expected category %s to be present", key)
		}
	}
}

// TestCategoriesNoDuplicateFactors verifies uniqueStrings deduplication works.
func TestCategoriesNoDuplicateFactors(t *testing.T) {
	s := Signals{DomainHits: map[string]int{DomainAuth: 2}}
	factors := []RiskFactor{
		{ID: "domain_auth", Points: 14},
		{ID: "domain_auth", Points: 14}, // duplicate
	}
	cats := ComputeCategories(s, factors, nil, nil)
	code := findCategory(cats, CategoryCode)
	if code == nil {
		t.Fatal("expected code category")
	}
	count := 0
	for _, f := range code.Factors {
		if f == "domain_auth" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected domain_auth deduped to 1 entry, got %d", count)
	}
}

// TestCategoriesTestConfidenceBreakdownPresent verifies ConfidenceBreakdown is populated
// for the test_confidence category.
func TestCategoriesTestConfidenceBreakdownPresent(t *testing.T) {
	s := Signals{
		DomainHits:   map[string]int{DomainAuth: 1},
		E2ETestFiles: 1,
		TestFiles:    1,
	}
	cats := ComputeCategories(s, nil, nil, nil)
	tc := findCategory(cats, CategoryTestConfidence)
	if tc == nil {
		t.Fatal("expected test_confidence category")
	}
	if tc.Breakdown == nil {
		t.Fatal("expected ConfidenceBreakdown to be populated")
	}
	if tc.Breakdown.FinalScore != tc.Confidence {
		t.Errorf("breakdown FinalScore %.0f != Confidence %.0f", tc.Breakdown.FinalScore, tc.Confidence)
	}
	if len(tc.Breakdown.Adjustments) == 0 {
		t.Error("expected at least one adjustment in breakdown")
	}
}

// TestCategoriesTestConfidenceBreakdownBaseScore verifies the base score is 50.
func TestCategoriesTestConfidenceBreakdownBaseScore(t *testing.T) {
	s := Signals{DomainHits: map[string]int{DomainWeb: 1}}
	cats := ComputeCategories(s, nil, nil, nil)
	tc := findCategory(cats, CategoryTestConfidence)
	if tc == nil {
		t.Fatal("expected test_confidence category")
	}
	if tc.Breakdown == nil {
		t.Fatal("expected ConfidenceBreakdown")
	}
	if tc.Breakdown.BaseScore != 50 {
		t.Errorf("expected base score 50, got %.0f", tc.Breakdown.BaseScore)
	}
}

// TestCategoriesTestConfidenceDistantProximityLowersConfidence verifies that when tests
// are structurally distant from changed code, confidence is reduced even for non-sensitive diffs.
// The behavioral coverage penalty does NOT fire for non-sensitive diffs (see comment in categories.go).
func TestCategoriesTestConfidenceDistantProximityLowersConfidence(t *testing.T) {
	s := Signals{
		DomainHits: map[string]int{DomainWeb: 1},
		TestFiles:  1,
	}
	ci := &riskcontext.ContextInsights{
		Proximity: riskcontext.ProximityInsight{
			Mode:                "distant",
			StructuralAlignment: "distant",
			BehavioralCoverage:  "unknown",
		},
	}
	cats := ComputeCategories(s, nil, nil, ci)
	tc := findCategory(cats, CategoryTestConfidence)
	if tc == nil {
		t.Fatal("expected test_confidence category")
	}
	// Base non-sensitive: 85, -15 (distant). Behavioral penalty does not fire: sensitiveChanged=false.
	if tc.Confidence != 70 {
		t.Errorf("expected confidence 70 with distant penalty only (non-sensitive), got %.0f", tc.Confidence)
	}
	if tc.RiskScore != 30 {
		t.Errorf("expected risk 30, got %.1f", tc.RiskScore)
	}
	// Breakdown: non-sensitive (+35) and distant (-15) = 2 adjustments minimum
	if tc.Breakdown == nil || len(tc.Breakdown.Adjustments) < 2 {
		t.Errorf("expected at least 2 adjustments in breakdown, got %v", tc.Breakdown)
	}
}

// TestCategoriesTestConfidenceSensitiveDistantUnknownBehavioralPenalty verifies that the
// behavioral coverage penalty fires for sensitive domain diffs with unknown behavioral coverage.
func TestCategoriesTestConfidenceSensitiveDistantUnknownBehavioralPenalty(t *testing.T) {
	s := Signals{
		DomainHits:    map[string]int{DomainAuth: 1},
		UnitTestFiles: 1,
		TestFiles:     1,
	}
	ci := &riskcontext.ContextInsights{
		Proximity: riskcontext.ProximityInsight{
			Mode:                "distant",
			StructuralAlignment: "distant",
			BehavioralCoverage:  "unknown",
		},
	}
	cats := ComputeCategories(s, nil, nil, ci)
	tc := findCategory(cats, CategoryTestConfidence)
	if tc == nil {
		t.Fatal("expected test_confidence category")
	}
	// Sensitive + unit tests: 40 + 20 = 60; -15 (distant) -10 (unknown behavioral, sensitive) = 35
	if tc.Confidence != 35 {
		t.Errorf("expected confidence 35 for sensitive+distant+unknown, got %.0f", tc.Confidence)
	}
}

// TestCategoriesTestConfidenceNAProximityNoProximityPenalty verifies that n_a proximity mode
// (config-only / untestable files) does not apply distance penalties.
func TestCategoriesTestConfidenceNAProximityNoProximityPenalty(t *testing.T) {
	s := Signals{
		DomainHits: map[string]int{DomainWorkflows: 1},
		TestFiles:  1,
	}
	ci := &riskcontext.ContextInsights{
		Proximity: riskcontext.ProximityInsight{
			Mode:                "n_a",
			StructuralAlignment: "n_a",
			BehavioralCoverage:  "unknown",
		},
	}
	cats := ComputeCategories(s, nil, nil, ci)
	tc := findCategory(cats, CategoryTestConfidence)
	if tc == nil {
		t.Fatal("expected test_confidence category")
	}
	// n_a mode: no proximity penalties; non-sensitive: 85
	if tc.Confidence != 85 {
		t.Errorf("expected confidence 85 for n_a proximity mode, got %.0f", tc.Confidence)
	}
}

// TestCategoriesTestConfidenceNoTestFilesNoProximityPenalty verifies that the proximity
// penalties do not fire when there are no test files (the "no tests" penalty already applied).
func TestCategoriesTestConfidenceNoTestFilesNoProximityPenalty(t *testing.T) {
	s := Signals{
		DomainHits: map[string]int{DomainWeb: 1},
		TestFiles:  0,
	}
	ci := &riskcontext.ContextInsights{
		Proximity: riskcontext.ProximityInsight{
			Mode:                "distant",
			StructuralAlignment: "distant",
			BehavioralCoverage:  "unknown",
		},
	}
	cats := ComputeCategories(s, nil, nil, ci)
	tc := findCategory(cats, CategoryTestConfidence)
	if tc == nil {
		t.Fatal("expected test_confidence category")
	}
	// Non-sensitive, TestFiles=0: no proximity penalty (not applicable); confidence = 85
	if tc.Confidence != 85 {
		t.Errorf("expected confidence 85 when no test files (proximity penalty not applicable), got %.0f", tc.Confidence)
	}
}

// TestCategoriesTestConfidenceDistantOnlySensitive is superseded by
// TestCategoriesTestConfidenceSensitiveDistantUnknownBehavioralPenalty above.

func findCategory(cats []RiskCategory, key string) *RiskCategory {
	for i := range cats {
		if cats[i].Key == key {
			return &cats[i]
		}
	}
	return nil
}

func containsStr(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}
