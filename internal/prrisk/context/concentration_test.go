package riskcontext

import (
	"math"
	"testing"
)

func TestConcentrationNoFiles(t *testing.T) {
	r := AnalyzeConcentration(Input{Files: nil})
	if r.Mode != "balanced" {
		t.Errorf("expected balanced for no files, got %q", r.Mode)
	}
}

func TestConcentrationNoLOC(t *testing.T) {
	// Files present but zero churn.
	r := AnalyzeConcentration(Input{
		Files: []FileChange{{Path: "internal/auth/x.go", Added: 0, Deleted: 0}},
	})
	if r.Mode != "balanced" {
		t.Errorf("expected balanced for zero LOC, got %q", r.Mode)
	}
}

func TestConcentrationFocused(t *testing.T) {
	// All churn in one prefix (100% share, 1 unique dir) — focused.
	r := AnalyzeConcentration(Input{
		Files: []FileChange{
			{Path: "internal/auth/x.go", Added: 50, Deleted: 0},
			{Path: "internal/auth/y.go", Added: 30, Deleted: 0},
		},
	})
	if r.Mode != "focused" {
		t.Errorf("expected focused, got %q (topShare=%.2f, dirs=%d)", r.Mode, r.TopShare, r.UniqueDirs)
	}
	if r.TopPrefix != "internal/auth" {
		t.Errorf("expected top prefix internal/auth, got %q", r.TopPrefix)
	}
	if r.TopShare < 0.55 {
		t.Errorf("expected topShare >= 0.55, got %.2f", r.TopShare)
	}
}

func TestConcentrationFocusedMultipleDirs(t *testing.T) {
	// 80% in one dir, 20% in another — still focused (topShare >= 0.55, ≤4 dirs).
	r := AnalyzeConcentration(Input{
		Files: []FileChange{
			{Path: "internal/auth/x.go", Added: 80, Deleted: 0},
			{Path: "internal/rag/y.go", Added: 20, Deleted: 0},
		},
	})
	if r.Mode != "focused" {
		t.Errorf("expected focused for 80/20 split, got %q", r.Mode)
	}
}

func TestConcentrationBalanced(t *testing.T) {
	// Spread across 3 dirs but less than 6 — balanced.
	r := AnalyzeConcentration(Input{
		Files: []FileChange{
			{Path: "internal/auth/x.go", Added: 40, Deleted: 0},
			{Path: "internal/rag/y.go", Added: 35, Deleted: 0},
			{Path: "internal/processing/z.go", Added: 25, Deleted: 0},
		},
	})
	if r.Mode != "balanced" {
		t.Errorf("expected balanced for 3-dir spread, got %q", r.Mode)
	}
}

func TestConcentrationScattered(t *testing.T) {
	// 10 files, 6+ dirs, low HHI → scattered.
	fc := []FileChange{
		{Path: "internal/auth/a.go", Added: 10},
		{Path: "internal/rag/b.go", Added: 10},
		{Path: "internal/processing/c.go", Added: 10},
		{Path: "internal/storage/d.go", Added: 10},
		{Path: "internal/handlers/e.go", Added: 10},
		{Path: "internal/migrations/f.go", Added: 10},
		{Path: "web/src/g.tsx", Added: 10},
		{Path: "scripts/h.py", Added: 10},
		{Path: "ops/config/i.yaml", Added: 10},
		{Path: "cmd/api/j.go", Added: 10},
	}
	r := AnalyzeConcentration(Input{Files: fc})
	if r.Mode != "scattered" {
		t.Errorf("expected scattered for 10 files in 10 dirs, got %q (hhi=%.3f, dirs=%d)", r.Mode, r.HHI, r.UniqueDirs)
	}
	if r.UniqueDirs < 6 {
		t.Errorf("expected UniqueDirs >= 6, got %d", r.UniqueDirs)
	}
}

func TestConcentrationScatteredRequiresTenFiles(t *testing.T) {
	// Same dirs but only 6 files — should not be scattered (requires >= 10 files).
	fc := []FileChange{
		{Path: "internal/auth/a.go", Added: 10},
		{Path: "internal/rag/b.go", Added: 10},
		{Path: "internal/processing/c.go", Added: 10},
		{Path: "internal/storage/d.go", Added: 10},
		{Path: "internal/handlers/e.go", Added: 10},
		{Path: "web/src/f.tsx", Added: 10},
	}
	r := AnalyzeConcentration(Input{Files: fc})
	if r.Mode == "scattered" {
		t.Error("expected non-scattered for fewer than 10 files even with 6+ dirs")
	}
}

func TestConcentrationHHIArithmetic(t *testing.T) {
	// Two equal-weight prefixes → HHI = 0.5^2 + 0.5^2 = 0.5.
	r := AnalyzeConcentration(Input{
		Files: []FileChange{
			{Path: "internal/auth/x.go", Added: 50},
			{Path: "internal/rag/y.go", Added: 50},
		},
	})
	want := 0.5
	if math.Abs(r.HHI-want) > 0.01 {
		t.Errorf("expected HHI≈%.2f for equal split, got %.4f", want, r.HHI)
	}
}

func TestTwoSegmentPrefix(t *testing.T) {
	cases := []struct{ path, want string }{
		{"internal/auth/x.go", "internal/auth"},
		{"web/src/components/Foo.tsx", "web/src"},
		{"go.mod", "go.mod"},
		{"cmd/api/main.go", "cmd/api"},
		{"", ""},  // strings.Split("","/") returns [""], so parts[0]="" is returned
	}
	for _, c := range cases {
		got := twoSegmentPrefix(c.path)
		if got != c.want {
			t.Errorf("twoSegmentPrefix(%q) = %q, want %q", c.path, got, c.want)
		}
	}
}
