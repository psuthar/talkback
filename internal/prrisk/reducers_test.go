package prrisk

import "testing"

// TestReducerValidationNoteModerateForWorkflowDispatch verifies the moderate tier
// fires for a "workflow dispatch" snippet.
func TestReducerValidationNoteModerateForWorkflowDispatch(t *testing.T) {
	s := Signals{
		ValidationNoteFound:   true,
		ValidationNoteSnippet: "Validation: ran workflow dispatch",
		DomainHits:            map[string]int{},
	}
	for _, factorID := range []string{"ci_workflows", "deploy_config", "go_mod_deps"} {
		factors := []RiskFactor{{ID: factorID, Points: 12}}
		reducers := DetectReducers(s, factors, DefaultWeights())
		found := findReducer(reducers, "validation_note_moderate")
		if found == nil {
			t.Errorf("expected validation_note_moderate reducer for factor %s", factorID)
			continue
		}
		if found.Points != DefaultWeights().WorkflowPartialReducerPoints {
			t.Errorf("[%s] points: want %v got %v", factorID, DefaultWeights().WorkflowPartialReducerPoints, found.Points)
		}
		if found.CategoryKey != CategoryWorkflow {
			t.Errorf("[%s] category: want %s got %s", factorID, CategoryWorkflow, found.CategoryKey)
		}
	}
}

// TestReducerValidationNoteStrongForE2EEvidence verifies the strong tier fires
// when the snippet mentions E2E or smoke testing.
func TestReducerValidationNoteStrongForE2EEvidence(t *testing.T) {
	s := Signals{
		ValidationNoteFound:   true,
		ValidationNoteSnippet: "Validation: ran E2E smoke suite, all tests passed",
		DomainHits:            map[string]int{},
	}
	factors := []RiskFactor{{ID: "ci_workflows", Points: 12}}
	w := DefaultWeights()
	reducers := DetectReducers(s, factors, w)
	found := findReducer(reducers, "validation_note_strong")
	if found == nil {
		t.Fatal("expected validation_note_strong for E2E evidence snippet")
	}
	if found.Points != w.ValidationNoteReducerPoints {
		t.Errorf("points: want %v got %v", w.ValidationNoteReducerPoints, found.Points)
	}
}

// TestReducerValidationNoteBasicForGenericNote verifies the basic tier fires
// when the snippet has no staging/CI/E2E keywords.
func TestReducerValidationNoteBasicForGenericNote(t *testing.T) {
	s := Signals{
		ValidationNoteFound:   true,
		ValidationNoteSnippet: "Validation: manually verified the change looks correct",
		DomainHits:            map[string]int{},
	}
	factors := []RiskFactor{{ID: "go_mod_deps", Points: 8}}
	w := DefaultWeights()
	reducers := DetectReducers(s, factors, w)
	found := findReducer(reducers, "validation_note_basic")
	if found == nil {
		t.Fatal("expected validation_note_basic for generic note")
	}
	if found.Points != w.WorkflowPartialReducerPoints/2 {
		t.Errorf("points: want %v got %v", w.WorkflowPartialReducerPoints/2, found.Points)
	}
}

// TestReducerValidationNoteAbsentWithoutWorkflowFactor verifies no tiered note reducer
// fires when only non-workflow factors are present.
func TestReducerValidationNoteAbsentWithoutWorkflowFactor(t *testing.T) {
	s := Signals{
		ValidationNoteFound: true,
		DomainHits:          map[string]int{DomainAuth: 1},
	}
	factors := []RiskFactor{{ID: "domain_auth", Points: 14}}
	reducers := DetectReducers(s, factors, DefaultWeights())
	for _, id := range []string{"validation_note_strong", "validation_note_moderate", "validation_note_basic"} {
		if findReducer(reducers, id) != nil {
			t.Errorf("%s should not fire without a workflow/deploy/gomod factor", id)
		}
	}
}

// TestReducerE2EEvidenceLowersAuthRisk verifies E2E evidence reduces auth risk.
func TestReducerE2EEvidenceLowersAuthRisk(t *testing.T) {
	s := Signals{
		DomainHits:        map[string]int{DomainAuth: 1},
		TestE2EDomainHits: map[string]int{DomainAuth: 1},
	}
	factors := []RiskFactor{{ID: "domain_auth", Points: 14}}
	w := DefaultWeights()
	reducers := DetectReducers(s, factors, w)
	found := findReducer(reducers, "domain_auth_e2e_evidence")
	if found == nil {
		t.Fatal("expected domain_auth_e2e_evidence reducer")
	}
	if found.Points != w.E2ETestEvidenceReducerPoints {
		t.Errorf("points: want %v got %v", w.E2ETestEvidenceReducerPoints, found.Points)
	}
	if found.CategoryKey != CategoryTestConfidence {
		t.Errorf("category: want %s got %s", CategoryTestConfidence, found.CategoryKey)
	}
}

// TestReducerUnitEvidenceFallsBackWhenNoE2E verifies unit evidence fires when
// E2E evidence is absent.
func TestReducerUnitEvidenceFallsBackWhenNoE2E(t *testing.T) {
	s := Signals{
		DomainHits:         map[string]int{DomainAuth: 1},
		TestUnitDomainHits: map[string]int{DomainAuth: 1},
		TestE2EDomainHits:  map[string]int{}, // no E2E hits
	}
	factors := []RiskFactor{{ID: "domain_auth", Points: 14}}
	w := DefaultWeights()
	reducers := DetectReducers(s, factors, w)
	if findReducer(reducers, "domain_auth_e2e_evidence") != nil {
		t.Error("e2e reducer should not fire without e2e hits")
	}
	found := findReducer(reducers, "domain_auth_unit_evidence")
	if found == nil {
		t.Fatal("expected domain_auth_unit_evidence reducer")
	}
	if found.Points != w.UnitTestEvidenceReducerPoints {
		t.Errorf("points: want %v got %v", w.UnitTestEvidenceReducerPoints, found.Points)
	}
}

// TestReducerE2ETakesPriorityOverUnit verifies E2E wins when both are present.
func TestReducerE2ETakesPriorityOverUnit(t *testing.T) {
	s := Signals{
		DomainHits:         map[string]int{DomainAuth: 1},
		TestE2EDomainHits:  map[string]int{DomainAuth: 1},
		TestUnitDomainHits: map[string]int{DomainAuth: 1},
	}
	factors := []RiskFactor{{ID: "domain_auth", Points: 14}}
	reducers := DetectReducers(s, factors, DefaultWeights())
	if findReducer(reducers, "domain_auth_e2e_evidence") == nil {
		t.Error("expected e2e reducer when both e2e and unit present")
	}
	if findReducer(reducers, "domain_auth_unit_evidence") != nil {
		t.Error("unit reducer should not fire when e2e reducer fires")
	}
}

// TestReducerTestOnlyDiff verifies the reducer fires when all changed files are tests.
func TestReducerTestOnlyDiff(t *testing.T) {
	s := Signals{
		FileCount:  2,
		TestFiles:  2,
		DomainHits: map[string]int{DomainTests: 2},
		Files: []FileChange{
			{Path: "internal/foo/bar_test.go"},
			{Path: "internal/baz/qux_test.go"},
		},
	}
	w := DefaultWeights()
	reducers := DetectReducers(s, []RiskFactor{}, w)
	found := findReducer(reducers, "test_only_diff")
	if found == nil {
		t.Fatal("expected test_only_diff reducer")
	}
	if found.Points != w.TestOnlyDiffReducerPoints {
		t.Errorf("points: want %v got %v", w.TestOnlyDiffReducerPoints, found.Points)
	}
}

// TestReducerTestOnlyDiffNotFiredWithSensitiveDomain verifies the reducer does
// not fire when sensitive domain hits are present alongside test files.
func TestReducerTestOnlyDiffNotFiredWithSensitiveDomain(t *testing.T) {
	s := Signals{
		FileCount:  2,
		TestFiles:  1,
		DomainHits: map[string]int{DomainAuth: 1, DomainTests: 1},
	}
	reducers := DetectReducers(s, []RiskFactor{}, DefaultWeights())
	if findReducer(reducers, "test_only_diff") != nil {
		t.Error("test_only_diff should not fire when sensitive domain hits present")
	}
}

// TestReducerTestOnlyDiffNotFiredWhenMixed verifies it doesn't fire when only
// some files are tests.
func TestReducerTestOnlyDiffNotFiredWhenMixed(t *testing.T) {
	s := Signals{
		FileCount:  2,
		TestFiles:  1, // only 1 of 2 files is a test
		DomainHits: map[string]int{DomainWeb: 1, DomainTests: 1},
	}
	reducers := DetectReducers(s, []RiskFactor{}, DefaultWeights())
	if findReducer(reducers, "test_only_diff") != nil {
		t.Error("test_only_diff should not fire when not all files are tests")
	}
}

// TestReducerLowersScoreViaScoreFunction verifies that E2E evidence for an auth
// change produces a lower final score than the same change without test evidence.
func TestReducerLowersScoreViaScoreFunction(t *testing.T) {
	base := Signals{
		BaseRef:    "origin/main",
		Files:      []FileChange{{Path: "internal/auth/x.go", Added: 10}},
		FileCount:  1,
		TotalLOC:   10,
		DomainHits: map[string]int{DomainAuth: 1},
	}
	withE2E := base
	withE2E.TestFiles = 1
	withE2E.E2ETestFiles = 1
	withE2E.TestE2EDomainHits = map[string]int{DomainAuth: 1}

	w := DefaultWeights()
	without := Score(base, w, "").RiskScore
	with := Score(withE2E, w, "").RiskScore

	if with >= without {
		t.Errorf("E2E evidence should lower score: with=%.1f without=%.1f", with, without)
	}
}

// TestReducerTestHeavyDiff verifies test_heavy_diff fires when test LOC ratio is high
// but the diff includes non-test files.
func TestReducerTestHeavyDiff(t *testing.T) {
	s := Signals{
		FileCount:    5,
		TestFiles:    3,
		TotalLOC:     100,
		TestLOCRatio: 0.60,
		DomainHits:   map[string]int{DomainWeb: 2, DomainTests: 3},
	}
	w := DefaultWeights()
	reducers := DetectReducers(s, []RiskFactor{}, w)
	found := findReducer(reducers, "test_heavy_diff")
	if found == nil {
		t.Fatal("expected test_heavy_diff reducer when test LOC ratio >= threshold")
	}
	if found.Points != w.TestHeavyReducerPoints {
		t.Errorf("points: want %v got %v", w.TestHeavyReducerPoints, found.Points)
	}
	if found.CategoryKey != CategoryTestConfidence {
		t.Errorf("category: want %s got %s", CategoryTestConfidence, found.CategoryKey)
	}
}

// TestReducerTestHeavyDiffNotFiredWhenAllTests verifies test_heavy_diff is suppressed
// when all files are tests (test_only_diff takes priority).
func TestReducerTestHeavyDiffNotFiredWhenAllTests(t *testing.T) {
	s := Signals{
		FileCount:    3,
		TestFiles:    3,
		TotalLOC:     60,
		TestLOCRatio: 1.0,
		DomainHits:   map[string]int{DomainTests: 3},
	}
	reducers := DetectReducers(s, []RiskFactor{}, DefaultWeights())
	if findReducer(reducers, "test_heavy_diff") != nil {
		t.Error("test_heavy_diff should not fire when all files are tests")
	}
}

// TestReducerTestHeavyDiffNotFiredBelowThreshold verifies test_heavy_diff does not
// fire when test LOC ratio is below the threshold.
func TestReducerTestHeavyDiffBelowThreshold(t *testing.T) {
	s := Signals{
		FileCount:    5,
		TestFiles:    1,
		TotalLOC:     100,
		TestLOCRatio: 0.10, // below 0.40 threshold
		DomainHits:   map[string]int{DomainWeb: 4, DomainTests: 1},
	}
	reducers := DetectReducers(s, []RiskFactor{}, DefaultWeights())
	if findReducer(reducers, "test_heavy_diff") != nil {
		t.Error("test_heavy_diff should not fire below LOC ratio threshold")
	}
}

func findReducer(reducers []RiskReducer, id string) *RiskReducer {
	for i := range reducers {
		if reducers[i].ID == id {
			return &reducers[i]
		}
	}
	return nil
}
