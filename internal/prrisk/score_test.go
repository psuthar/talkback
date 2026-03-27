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
