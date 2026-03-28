package riskcontext

import "testing"

// helpers

func files(paths ...string) []FileChange {
	fc := make([]FileChange, len(paths))
	for i, p := range paths {
		fc[i] = FileChange{Path: p, Added: 10, Deleted: 0}
	}
	return fc
}

func isTestFlags(flags ...bool) []bool { return flags }

// --- AnalyzeProximity mode detection ---

func TestProximityNANoFiles(t *testing.T) {
	r := AnalyzeProximity(Input{Files: nil, IsTest: nil})
	if r.Mode != "n_a" {
		t.Errorf("expected n_a for empty input, got %q", r.Mode)
	}
}

func TestProximityNAMismatchedArrays(t *testing.T) {
	r := AnalyzeProximity(Input{
		Files:  files("internal/auth/x.go"),
		IsTest: []bool{}, // length mismatch
	})
	if r.Mode != "n_a" {
		t.Errorf("expected n_a when IsTest length != Files length, got %q", r.Mode)
	}
}

func TestProximityNAOnlyTestFiles(t *testing.T) {
	r := AnalyzeProximity(Input{
		Files:  files("internal/auth/x_test.go"),
		IsTest: isTestFlags(true),
	})
	if r.Mode != "n_a" {
		t.Errorf("expected n_a when only test files, got %q", r.Mode)
	}
}

func TestProximityDistantNoTestsInDiff(t *testing.T) {
	r := AnalyzeProximity(Input{
		Files:  files("internal/auth/x.go", "internal/auth/y.go"),
		IsTest: isTestFlags(false, false),
	})
	if r.Mode != "distant" {
		t.Errorf("expected distant when no tests in diff, got %q", r.Mode)
	}
	if r.Ratio != 0 {
		t.Errorf("expected ratio 0, got %v", r.Ratio)
	}
}

func TestProximityNAAllUntestableFiles(t *testing.T) {
	// A diff of only YAML/shell/markdown should not produce a "distant" proximity —
	// these file types have no test convention, so proximity is not applicable.
	r := AnalyzeProximity(Input{
		Files:        files(".github/workflows/ci.yml", "scripts/deploy.sh", "docs/README.md"),
		IsTest:       isTestFlags(false, false, false),
		IsUntestable: []bool{true, true, true},
	})
	if r.Mode != "n_a" {
		t.Errorf("expected n_a for all-untestable diff, got %q", r.Mode)
	}
}

func TestProximityDistantMixedUntestableAndSource(t *testing.T) {
	// A diff mixing source files with config files should still be scored normally.
	r := AnalyzeProximity(Input{
		Files:        files("internal/auth/x.go", ".github/workflows/ci.yml"),
		IsTest:       isTestFlags(false, false),
		IsUntestable: []bool{false, true},
	})
	if r.Mode != "distant" {
		t.Errorf("expected distant when source files present without tests, got %q", r.Mode)
	}
}

func TestProximityCoLocated(t *testing.T) {
	// All prod files have a test in the same directory.
	r := AnalyzeProximity(Input{
		Files: files(
			"internal/auth/x.go",
			"internal/auth/x_test.go",
			"internal/auth/y.go",
			"internal/auth/y_test.go",
		),
		IsTest: isTestFlags(false, true, false, true),
	})
	if r.Mode != "co_located" {
		t.Errorf("expected co_located, got %q (ratio=%.2f)", r.Mode, r.Ratio)
	}
	if r.NonTestFiles != 2 || r.WithNearbyTestInDiff != 2 {
		t.Errorf("expected 2/2 nearby, got %d/%d", r.WithNearbyTestInDiff, r.NonTestFiles)
	}
}

func TestProximityPartial(t *testing.T) {
	// 50% of prod files have a nearby test — falls in [0.35, 0.75).
	r := AnalyzeProximity(Input{
		Files: files(
			"internal/auth/x.go",
			"internal/auth/x_test.go",
			"internal/rag/y.go", // no test nearby
		),
		IsTest: isTestFlags(false, true, false),
	})
	if r.Mode != "partial" {
		t.Errorf("expected partial, got %q (ratio=%.2f)", r.Mode, r.Ratio)
	}
}

func TestProximityDistantLowRatio(t *testing.T) {
	// 1 of 4 prod files has a nearby test — ratio 0.25 < 0.35 → distant.
	r := AnalyzeProximity(Input{
		Files: files(
			"internal/auth/x.go",
			"internal/auth/x_test.go",
			"internal/rag/a.go",
			"internal/rag/b.go",
			"internal/processing/c.go",
		),
		IsTest: isTestFlags(false, true, false, false, false),
	})
	if r.Mode != "distant" {
		t.Errorf("expected distant, got %q (ratio=%.2f)", r.Mode, r.Ratio)
	}
}

// --- hasNearbyTestInDiff heuristic precision ---

func TestProximitySameDir(t *testing.T) {
	if !hasNearbyTestInDiff("internal/auth/x.go", []string{"internal/auth/x_test.go"}) {
		t.Error("expected same-dir to match")
	}
}

func TestProximityOneParentLevelDown(t *testing.T) {
	// Test dir is the direct parent of code dir — should match.
	if !hasNearbyTestInDiff("internal/auth/session.go", []string{"internal/auth_test.go"}) {
		t.Error("expected direct-parent test to match code one level deeper")
	}
}

func TestProximityOneParentLevelUp(t *testing.T) {
	// Code dir is the direct parent of test dir — should match.
	if !hasNearbyTestInDiff("internal/auth.go", []string{"internal/auth/x_test.go"}) {
		t.Error("expected direct-child test dir to match code in parent dir")
	}
}

func TestProximityTwoLevelAncestorNoMatch(t *testing.T) {
	// Test at "internal/" root should NOT match code two levels deep.
	if hasNearbyTestInDiff("internal/auth/handlers/session.go", []string{"internal/root_test.go"}) {
		t.Error("a test two levels above code dir should not be considered nearby")
	}
}

func TestProximitySiblingDirNoMatch(t *testing.T) {
	// Tests in a sibling package should NOT match.
	if hasNearbyTestInDiff("internal/auth/x.go", []string{"internal/rag/rag_test.go"}) {
		t.Error("test in sibling dir should not match")
	}
}

func TestProximityWebE2EMatchesWebCode(t *testing.T) {
	// web/tests/e2e/ tests match any web/ code (broad, intentional).
	if !hasNearbyTestInDiff("web/src/components/Foo.tsx", []string{"web/tests/e2e/creator-access.spec.ts"}) {
		t.Error("e2e test should match web/ production code")
	}
}

func TestProximityWebE2EDoesNotMatchNonWeb(t *testing.T) {
	if hasNearbyTestInDiff("internal/auth/x.go", []string{"web/tests/e2e/creator-access.spec.ts"}) {
		t.Error("e2e test should not match non-web code")
	}
}

// --- StructuralAlignment mirrors Mode ---

func TestProximityStructuralAlignmentMirrorsMode(t *testing.T) {
	cases := []struct {
		name   string
		in     Input
	}{
		{
			name: "n_a",
			in:   Input{Files: nil, IsTest: nil},
		},
		{
			name: "distant",
			in: Input{
				Files:  files("internal/auth/x.go", "internal/auth/y.go"),
				IsTest: isTestFlags(false, false),
			},
		},
		{
			name: "partial",
			in: Input{
				Files:  files("internal/auth/x.go", "internal/auth/x_test.go", "internal/rag/y.go"),
				IsTest: isTestFlags(false, true, false),
			},
		},
		{
			name: "co_located",
			in: Input{
				Files:  files("internal/auth/x.go", "internal/auth/x_test.go", "internal/auth/y.go", "internal/auth/y_test.go"),
				IsTest: isTestFlags(false, true, false, true),
			},
		},
	}
	for _, c := range cases {
		r := AnalyzeProximity(c.in)
		if r.StructuralAlignment != r.Mode {
			t.Errorf("%s: StructuralAlignment=%q, Mode=%q — expected equal", c.name, r.StructuralAlignment, r.Mode)
		}
	}
}

// --- BehavioralCoverage ---

// TestBehavioralCoverageDistantIsUnknown verifies distant structural mode always
// yields unknown behavioral coverage regardless of domain hit maps.
func TestBehavioralCoverageDistantIsUnknown(t *testing.T) {
	r := AnalyzeProximity(Input{
		Files:              files("internal/auth/x.go"),
		IsTest:             isTestFlags(false),
		DomainHits:         map[string]int{"auth": 1},
		TestUnitDomainHits: map[string]int{"auth": 1},
		TestE2EDomainHits:  map[string]int{"auth": 1},
	})
	if r.Mode != "distant" {
		t.Fatalf("expected distant mode for no tests in diff, got %q", r.Mode)
	}
	if r.BehavioralCoverage != "unknown" {
		t.Errorf("distant mode should yield unknown coverage, got %q", r.BehavioralCoverage)
	}
}

// TestBehavioralCoverageNonSensitiveIsAdequate verifies non-sensitive production
// domains produce adequate coverage without needing test domain hits.
func TestBehavioralCoverageNonSensitiveIsAdequate(t *testing.T) {
	r := AnalyzeProximity(Input{
		Files:      files("web/src/Foo.tsx", "web/src/Foo.test.tsx"),
		IsTest:     isTestFlags(false, true),
		DomainHits: map[string]int{"web": 2},
	})
	if r.Mode != "co_located" {
		t.Fatalf("expected co_located, got %q", r.Mode)
	}
	if r.BehavioralCoverage != "adequate" {
		t.Errorf("non-sensitive domain should yield adequate coverage, got %q", r.BehavioralCoverage)
	}
}

// TestBehavioralCoverageE2EOverlapIsAdequate verifies E2E domain hits covering a
// sensitive domain produce adequate coverage even without unit domain hits.
func TestBehavioralCoverageE2EOverlapIsAdequate(t *testing.T) {
	r := AnalyzeProximity(Input{
		Files:             files("internal/auth/x.go", "internal/auth/x_test.go"),
		IsTest:            isTestFlags(false, true),
		DomainHits:        map[string]int{"auth": 1},
		TestE2EDomainHits: map[string]int{"auth": 1},
	})
	if r.Mode != "co_located" {
		t.Fatalf("expected co_located, got %q", r.Mode)
	}
	if r.BehavioralCoverage != "adequate" {
		t.Errorf("E2E overlap with sensitive domain should be adequate, got %q", r.BehavioralCoverage)
	}
}

// TestBehavioralCoverageUnitCoLocatedIsShallow verifies co_located structural mode +
// unit domain overlap (no E2E) on a sensitive domain produces shallow coverage.
func TestBehavioralCoverageUnitCoLocatedIsShallow(t *testing.T) {
	r := AnalyzeProximity(Input{
		Files:              files("internal/auth/x.go", "internal/auth/x_test.go"),
		IsTest:             isTestFlags(false, true),
		DomainHits:         map[string]int{"auth": 1},
		TestUnitDomainHits: map[string]int{"auth": 1},
	})
	if r.Mode != "co_located" {
		t.Fatalf("expected co_located, got %q", r.Mode)
	}
	if r.BehavioralCoverage != "shallow" {
		t.Errorf("unit overlap + co_located + sensitive domain should be shallow, got %q", r.BehavioralCoverage)
	}
}

// TestBehavioralCoverageUnitPartialIsUnknown verifies partial structural mode +
// unit domain overlap (no E2E) on a sensitive domain remains unknown coverage.
func TestBehavioralCoverageUnitPartialIsUnknown(t *testing.T) {
	r := AnalyzeProximity(Input{
		Files:              files("internal/auth/x.go", "internal/auth/x_test.go", "internal/rag/y.go"),
		IsTest:             isTestFlags(false, true, false),
		DomainHits:         map[string]int{"auth": 1},
		TestUnitDomainHits: map[string]int{"auth": 1},
	})
	if r.Mode != "partial" {
		t.Fatalf("expected partial, got %q", r.Mode)
	}
	if r.BehavioralCoverage != "unknown" {
		t.Errorf("unit overlap + partial + sensitive domain should be unknown, got %q", r.BehavioralCoverage)
	}
}

// TestBehavioralCoverageNoTestDomainHitsIsUnknown verifies that structural co_located
// alone is insufficient when no test domain hits are recorded for a sensitive domain.
func TestBehavioralCoverageNoTestDomainHitsIsUnknown(t *testing.T) {
	r := AnalyzeProximity(Input{
		Files:      files("internal/auth/x.go", "internal/auth/x_test.go"),
		IsTest:     isTestFlags(false, true),
		DomainHits: map[string]int{"auth": 1},
		// TestUnitDomainHits and TestE2EDomainHits intentionally nil
	})
	if r.Mode != "co_located" {
		t.Fatalf("expected co_located, got %q", r.Mode)
	}
	if r.BehavioralCoverage != "unknown" {
		t.Errorf("co_located + sensitive domain + nil test domain hits should be unknown, got %q", r.BehavioralCoverage)
	}
}
