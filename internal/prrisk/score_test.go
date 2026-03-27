package prrisk

import "testing"

func TestScoreEmptyDiff(t *testing.T) {
	s := Signals{BaseRef: "origin/main", DomainHits: map[string]int{}}
	r := Score(s, DefaultWeights(), "")
	if r.RiskScore != 0 {
		t.Fatalf("expected 0 score, got %v", r.RiskScore)
	}
	if r.RiskBand != "low" {
		t.Fatalf("expected low band, got %s", r.RiskBand)
	}
}

func TestScoreAuthAndMigrations(t *testing.T) {
	s := Signals{
		BaseRef: "origin/main",
		Files: []FileChange{
			{Path: "internal/auth/x.go", Added: 10, Deleted: 0},
			{Path: "db/migrations/1.up.sql", Added: 5, Deleted: 0},
			{Path: "internal/foo/bar_test.go", Added: 20, Deleted: 0},
		},
		FileCount:      3,
		TotalAdded:     35,
		TotalDeleted:   0,
		TotalLOC:       35,
		DomainHits:     map[string]int{DomainAuth: 1, DomainMigrations: 1, DomainTests: 1},
		TestFiles:      1,
		MigrationFiles: 1,
	}
	r := Score(s, DefaultWeights(), "JIRA-1")
	if r.RiskScore <= 0 {
		t.Fatal("expected positive score")
	}
	if r.Integrations.JiraIssueKey != "JIRA-1" {
		t.Fatal("expected jira key")
	}
	if len(r.Mitigations) == 0 {
		t.Fatal("expected mitigations")
	}
}

func TestScoreGitError(t *testing.T) {
	s := Signals{BaseRef: "x", GitError: "no merge base", DomainHits: map[string]int{}}
	r := Score(s, DefaultWeights(), "")
	if r.RiskScore < 20 {
		t.Fatalf("expected git error to add risk, got %v", r.RiskScore)
	}
}

func TestScoreSpecificFactors(t *testing.T) {
	s := Signals{
		BaseRef: "origin/main",
		Files: []FileChange{
			{Path: "internal/auth/x.go", Added: 10},
			{Path: "db/migrations/1.up.sql", Added: 5},
		},
		FileCount:      2,
		TotalLOC:       15,
		DomainHits:     map[string]int{DomainAuth: 1, DomainMigrations: 1},
		MigrationFiles: 1,
	}
	r := Score(s, DefaultWeights(), "")
	if !hasFactorID(r.Factors, "domain_auth") {
		t.Error("expected domain_auth factor")
	}
	if !hasFactorID(r.Factors, "domain_migrations") {
		t.Error("expected domain_migrations factor")
	}
	if r.RiskBand != "high" && r.RiskBand != "critical" {
		t.Errorf("expected high or critical risk band, got %s", r.RiskBand)
	}
}

func TestScoreGoModTriggersFactor(t *testing.T) {
	s := Signals{
		BaseRef:      "origin/main",
		Files:        []FileChange{{Path: "go.mod", Added: 2, Deleted: 1}},
		FileCount:    1,
		TotalLOC:     3,
		DomainHits:   map[string]int{DomainOther: 1},
		ConfigFiles:  1,
	}
	r := Score(s, DefaultWeights(), "")
	if !hasFactorID(r.Factors, "go_mod_deps") {
		t.Error("expected go_mod_deps factor for go.mod change")
	}
}

func TestScoreLargeDiff(t *testing.T) {
	w := DefaultWeights()
	s := Signals{
		BaseRef:    "origin/main",
		TotalLOC:   w.LargeDiffLOC + 1,
		DomainHits: map[string]int{},
	}
	r := Score(s, w, "")
	if !hasFactorID(r.Factors, "diff_large") {
		t.Error("expected diff_large factor")
	}
	if hasFactorID(r.Factors, "diff_very_large") {
		t.Error("diff_large and diff_very_large are mutually exclusive")
	}
}

func TestScoreVeryLargeDiff(t *testing.T) {
	w := DefaultWeights()
	s := Signals{
		BaseRef:    "origin/main",
		TotalLOC:   w.VeryLargeDiffLOC + 1,
		DomainHits: map[string]int{},
	}
	r := Score(s, w, "")
	if !hasFactorID(r.Factors, "diff_very_large") {
		t.Error("expected diff_very_large factor")
	}
	if hasFactorID(r.Factors, "diff_large") {
		t.Error("diff_large should not fire when diff_very_large fires")
	}
}

func TestScoreManyFiles(t *testing.T) {
	w := DefaultWeights()
	s := Signals{
		BaseRef:    "origin/main",
		FileCount:  w.ManyFilesThreshold,
		DomainHits: map[string]int{},
	}
	r := Score(s, w, "")
	if !hasFactorID(r.Factors, "many_files") {
		t.Error("expected many_files factor at threshold")
	}
}

func hasFactorID(factors []RiskFactor, id string) bool {
	for _, f := range factors {
		if f.ID == id {
			return true
		}
	}
	return false
}
