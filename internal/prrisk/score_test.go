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
	if r.ScoreMath.FinalScore != r.RiskScore || r.ScoreMath.FinalBand != r.RiskBand {
		t.Fatalf("score_math mismatch: %+v vs risk %v/%s", r.ScoreMath, r.RiskScore, r.RiskBand)
	}
	if r.ScoreMath.FactorsSubtotal != 0 || r.ScoreMath.FloorApplied {
		t.Fatalf("unexpected score_math for empty diff: %+v", r.ScoreMath)
	}
	if r.Enforcement.MergeRecommendation != "pass" {
		t.Fatalf("empty diff should merge-recommend pass, got %q (%+v)", r.Enforcement.MergeRecommendation, r.Enforcement)
	}
	if len(r.Enforcement.RequiredValidations) == 0 {
		t.Fatal("expected at least baseline required validations")
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
		BaseRef:     "origin/main",
		Files:       []FileChange{{Path: "go.mod", Added: 2, Deleted: 1}},
		FileCount:   1,
		TotalLOC:    3,
		DomainHits:  map[string]int{DomainOther: 1},
		ConfigFiles: 1,
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

func TestScoreWorkflowFloorNotLowBand(t *testing.T) {
	w := DefaultWeights()
	s := Signals{
		BaseRef: "origin/main",
		Files: []FileChange{
			{Path: ".github/workflows/ci.yml", Added: 1},
		},
		FileCount:             1,
		TotalLOC:              1,
		DomainHits:            map[string]int{DomainWorkflows: 1},
		ConfigFiles:           1,
		ValidationNoteFound:   true,
		ValidationNoteSnippet: "Validation: e2e smoke passed",
	}
	r := Score(s, w, "")
	if r.RiskBand == "low" {
		t.Fatalf("workflow change must not end as low band, got %s score=%v", r.RiskBand, r.RiskScore)
	}
	if !r.ScoreMath.FloorApplied {
		t.Fatal("expected risk floor to apply (reducers would otherwise allow very low net)")
	}
	if r.RiskScore < floorMinNotLowBand {
		t.Fatalf("score %v below floor %v", r.RiskScore, floorMinNotLowBand)
	}
	found := false
	for _, a := range r.RequiredActions {
		if a.ID == "workflow_config_validation" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected workflow_config_validation required action")
	}
}

func TestScoreVersionMinor(t *testing.T) {
	s := Signals{BaseRef: "origin/main", DomainHits: map[string]int{}}
	r := Score(s, DefaultWeights(), "")
	if r.VersionMinor != VersionMinor {
		t.Fatalf("expected VersionMinor=%d on Result, got %d", VersionMinor, r.VersionMinor)
	}
	if r.Version != Version {
		t.Fatalf("expected Version=%d on Result, got %d", Version, r.Version)
	}
}

func TestScoreMathConsistency(t *testing.T) {
	// Auth + migrations diff with unit test evidence — reducers fire, floor does not.
	s := Signals{
		BaseRef: "origin/main",
		Files: []FileChange{
			{Path: "internal/auth/x.go", Added: 10},
		},
		FileCount:          1,
		TotalLOC:           10,
		DomainHits:         map[string]int{DomainAuth: 1},
		TestUnitDomainHits: map[string]int{DomainAuth: 1},
	}
	r := Score(s, DefaultWeights(), "")
	m := r.ScoreMath

	// FinalScore and RiskScore must agree.
	if m.FinalScore != r.RiskScore {
		t.Errorf("ScoreMath.FinalScore=%.1f != RiskScore=%.1f", m.FinalScore, r.RiskScore)
	}
	if m.FinalBand != r.RiskBand {
		t.Errorf("ScoreMath.FinalBand=%s != RiskBand=%s", m.FinalBand, r.RiskBand)
	}

	// ReducersSubtotal must be positive (unit evidence fired).
	if m.ReducersSubtotal <= 0 {
		t.Errorf("expected positive ReducersSubtotal, got %.1f", m.ReducersSubtotal)
	}

	// NetBeforeFloor == clamp100(FactorsSubtotal - ReducersSubtotal).
	want := clamp100(m.FactorsSubtotal - m.ReducersSubtotal)
	if m.NetBeforeFloor != want {
		t.Errorf("NetBeforeFloor=%.1f, want clamp100(%.1f-%.1f)=%.1f",
			m.NetBeforeFloor, m.FactorsSubtotal, m.ReducersSubtotal, want)
	}

	// No floor factors in this diff (only domain_auth), so floor must not apply.
	if m.FloorApplied {
		t.Error("expected FloorApplied=false for auth-only diff without floor factors")
	}
	if m.FloorMinScore != 0 {
		t.Errorf("expected FloorMinScore=0 when no floor rule fires, got %.1f", m.FloorMinScore)
	}
}

func TestScoreMathFloorAppliedPopulatesFields(t *testing.T) {
	// Workflow change with strong validation note: reducers may bring net below 20,
	// triggering the floor.
	w := DefaultWeights()
	s := Signals{
		BaseRef: "origin/main",
		Files: []FileChange{
			{Path: ".github/workflows/ci.yml", Added: 1},
		},
		FileCount:             1,
		TotalLOC:              1,
		DomainHits:            map[string]int{DomainWorkflows: 1},
		ValidationNoteFound:   true,
		ValidationNoteSnippet: "Validation: e2e smoke passed",
	}
	r := Score(s, w, "")
	m := r.ScoreMath
	if !m.FloorApplied {
		t.Fatal("expected FloorApplied=true for workflow floor scenario")
	}
	if m.FloorMinScore <= 0 {
		t.Errorf("expected positive FloorMinScore, got %.1f", m.FloorMinScore)
	}
	if len(m.FloorReasons) == 0 {
		t.Error("expected at least one FloorReason when floor applied")
	}
	// FinalScore must equal FloorMinScore when raised.
	if m.FinalScore != m.FloorMinScore {
		t.Errorf("FinalScore=%.1f, want FloorMinScore=%.1f when floor raised the score",
			m.FinalScore, m.FloorMinScore)
	}
}

func TestScoreLargeDiffRequiredAction(t *testing.T) {
	w := DefaultWeights()
	s := Signals{
		BaseRef:    "origin/main",
		TotalLOC:   w.LargeDiffLOC + 1,
		DomainHits: map[string]int{},
	}
	r := Score(s, w, "")
	if r.RiskBand == "low" {
		t.Fatalf("large diff must not end as low band, got %s", r.RiskBand)
	}
	found := false
	for _, a := range r.RequiredActions {
		if a.ID == "pr_review_summary" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected pr_review_summary for large diff")
	}
}
