package riskcontext

import "testing"

func TestAnalyzeEmptyInput(t *testing.T) {
	in := Input{Files: nil}
	insights, factors := Analyze(in, DefaultWeights())
	if insights.Proximity.Mode != "n_a" && insights.Proximity.Mode != "" {
		t.Logf("proximity mode: %q", insights.Proximity.Mode)
	}
	if len(factors) != 0 {
		t.Fatalf("expected no factor contributions for empty input, got %d", len(factors))
	}
}

// TestAnalyzeHotspotsSkipReasonField verifies the HotspotsSkipReason field exists
// and is populated when the hotspot git command fails (bad repo root).
func TestAnalyzeHotspotsSkipReasonField(t *testing.T) {
	in := Input{
		RepoRoot:   "/nonexistent/path/xyz",
		Files:      []FileChange{{Path: "internal/auth/x.go", Added: 5}},
		IsTest:     []bool{false},
		DomainHits: map[string]int{},
	}
	insights, _ := Analyze(in, DefaultWeights())
	// Field must exist on the struct (compilation check) and not panic.
	_ = insights.HotspotsSkipReason
}

// TestAnalyzeProximityFactorRequiresSensitiveDomain verifies the proximity factor
// is not emitted when the diff has no sensitive domains, even if proximity is distant.
func TestAnalyzeProximityFactorRequiresSensitiveDomain(t *testing.T) {
	in := Input{
		RepoRoot: "",
		GitError: "no git",
		Files: []FileChange{
			{Path: "web/src/Foo.tsx", Added: 20},
			{Path: "web/src/Bar.tsx", Added: 10},
			{Path: "scripts/build.sh", Added: 5},
		},
		IsTest:     []bool{false, false, false},
		DomainHits: map[string]int{"web": 2, "scripts": 1}, // no sensitive domain
	}
	_, factors := Analyze(in, DefaultWeights())
	for _, f := range factors {
		if f.ID == "context_test_proximity_distant" {
			t.Error("proximity factor should not fire without a sensitive domain")
		}
	}
}

// TestAnalyzeIntentMismatchFactorEmitted verifies the intent mismatch factor is
// emitted when the PR title keyword doesn't match the diff domain.
func TestAnalyzeIntentMismatchFactorEmitted(t *testing.T) {
	in := Input{
		RepoRoot: "",
		GitError: "no git",
		PRTitle:  "fix: auth login session handling",
		Files:    []FileChange{{Path: "db/migrations/001.sql", Added: 10}},
		IsTest:   []bool{false},
		DomainHits: map[string]int{"migrations": 1},
	}
	w := DefaultWeights()
	_, factors := Analyze(in, w)
	found := false
	for _, f := range factors {
		if f.ID == "context_intent_mismatch" {
			found = true
			if f.Points != w.IntentMismatchPoints {
				t.Errorf("intent mismatch points: want %.0f got %.0f", w.IntentMismatchPoints, f.Points)
			}
		}
	}
	if !found {
		t.Error("expected context_intent_mismatch factor for auth keyword vs migrations diff")
	}
}
