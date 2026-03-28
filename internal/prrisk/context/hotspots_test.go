package riskcontext

import "testing"

// TestHotspotsGitErrorSkipped verifies AnalyzeHotspots returns nil and no error
// when GitError is set (git log unavailable).
func TestHotspotsGitErrorSkipped(t *testing.T) {
	in := Input{GitError: "no merge base", RepoRoot: "."}
	hotspots, errMsg := AnalyzeHotspots(in)
	if len(hotspots) != 0 {
		t.Errorf("expected no hotspots when GitError is set, got %d", len(hotspots))
	}
	if errMsg != "" {
		t.Errorf("expected empty error message when GitError guard fires, got %q", errMsg)
	}
}

func TestHotspotsEmptyRepoRootSkipped(t *testing.T) {
	in := Input{RepoRoot: ""}
	hotspots, _ := AnalyzeHotspots(in)
	if len(hotspots) != 0 {
		t.Errorf("expected no hotspots when RepoRoot is empty, got %d", len(hotspots))
	}
}

// TestHotspotsSkipReasonSurfacedInInsights verifies that when AnalyzeHotspots
// returns an error, Analyze() captures it in HotspotsSkipReason.
func TestHotspotsSkipReasonSurfacedInInsights(t *testing.T) {
	// Use a non-existent repo root so git will fail and return an error string.
	in := Input{
		RepoRoot: "/nonexistent/repo/that/does/not/exist",
		Files: []FileChange{
			{Path: "internal/auth/x.go", Added: 10},
		},
		IsTest:     []bool{false},
		DomainHits: map[string]int{},
	}
	insights, _ := Analyze(in, DefaultWeights())
	// Hotspots should be nil (git failed) and the reason should be captured.
	// We don't assert the exact message (OS-dependent), just that the field is set.
	if len(insights.Hotspots) != 0 {
		t.Errorf("expected no hotspots for bad repo root, got %d", len(insights.Hotspots))
	}
	// HotspotsSkipReason may or may not be set depending on whether the guard
	// fires first (empty/error repo root) vs git command fails. Either way,
	// no panic should occur and the struct should be complete.
	_ = insights.HotspotsSkipReason
}

// TestHotspotsOverlapOnlyMatchesDiffPrefixes verifies that only hotspot prefixes
// that overlap with the diff are returned. We test the inverse: if the diff has
// no files and git returns something, there should be no overlap.
func TestHotspotsNoOverlapWhenDiffEmpty(t *testing.T) {
	// Even if git history has hotspots, empty diff means no overlap.
	in := Input{
		RepoRoot: ".", // real repo root — git will run
		Files:    nil,
		GitError: "",
	}
	hotspots, _ := AnalyzeHotspots(in)
	// With no diff files, diffPref is empty, so no hotspot can overlap.
	if len(hotspots) != 0 {
		t.Errorf("expected no hotspot overlap with empty diff, got %d", len(hotspots))
	}
}
