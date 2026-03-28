package prrisk

import (
	"strings"
	"testing"
)

// TestBuildIntegrationsVersionHeader verifies the PR comment markdown contains
// the correct version header matching ReportVersionString().
func TestBuildIntegrationsVersionHeader(t *testing.T) {
	integ := BuildIntegrations(nil, 0, "origin/main", "", nil, ScoreMath{}, Enforcement{})
	want := ReportVersionString()
	if !strings.Contains(integ.PRCommentMarkdown, want) {
		t.Errorf("expected %s in PR comment header, got:\n%s", want, integ.PRCommentMarkdown)
	}
}

// TestBuildIntegrationsScoreMathBlock verifies the score math line is rendered
// when FactorsSubtotal or ReducersSubtotal is non-zero.
func TestBuildIntegrationsScoreMathBlock(t *testing.T) {
	math := ScoreMath{
		FactorsSubtotal:  36.0,
		ReducersSubtotal: 10.0,
		NetBeforeFloor:   26.0,
		FloorMinScore:    0,
		FloorApplied:     false,
		FinalScore:       26.0,
		FinalBand:        "medium",
	}
	integ := BuildIntegrations(nil, 26.0, "origin/main", "", nil, math, Enforcement{})
	md := integ.PRCommentMarkdown
	if !strings.Contains(md, "36.0") {
		t.Errorf("expected FactorsSubtotal 36.0 in markdown, got:\n%s", md)
	}
	if !strings.Contains(md, "10.0") {
		t.Errorf("expected ReducersSubtotal 10.0 in markdown, got:\n%s", md)
	}
	if !strings.Contains(md, "26.0") {
		t.Errorf("expected NetBeforeFloor/FinalScore 26.0 in markdown, got:\n%s", md)
	}
}

// TestBuildIntegrationsScoreMathFloorApplied verifies floor applied notation
// is included in the PR comment markdown when FloorApplied is true.
func TestBuildIntegrationsScoreMathFloorApplied(t *testing.T) {
	math := ScoreMath{
		FactorsSubtotal:  12.0,
		ReducersSubtotal: 6.0,
		NetBeforeFloor:   6.0,
		FloorMinScore:    20.0,
		FloorApplied:     true,
		FinalScore:       20.0,
		FinalBand:        "medium",
	}
	integ := BuildIntegrations(nil, 20.0, "origin/main", "", nil, math, Enforcement{})
	md := integ.PRCommentMarkdown
	if !strings.Contains(md, "20") {
		t.Errorf("expected floor value 20 in markdown, got:\n%s", md)
	}
	if !strings.Contains(md, "applied") {
		t.Errorf("expected 'applied' in floor notation when FloorApplied=true, got:\n%s", md)
	}
}

// TestBuildIntegrationsScoreMathBlockAbsentWhenZero verifies the score math block
// is omitted when all math fields are zero (empty diff).
func TestBuildIntegrationsScoreMathBlockAbsentWhenZero(t *testing.T) {
	integ := BuildIntegrations(nil, 0, "origin/main", "", nil, ScoreMath{}, Enforcement{})
	md := integ.PRCommentMarkdown
	if strings.Contains(md, "Score math:") {
		t.Errorf("expected no score math block for zero math, got:\n%s", md)
	}
}

// TestBuildIntegrationsJiraKey verifies the Jira issue key appears in the
// markdown and is echoed on the Integrations struct.
func TestBuildIntegrationsJiraKey(t *testing.T) {
	integ := BuildIntegrations(nil, 30.0, "origin/main", "PROJ-42", nil, ScoreMath{}, Enforcement{})
	if integ.JiraIssueKey != "PROJ-42" {
		t.Errorf("expected JiraIssueKey=PROJ-42, got %q", integ.JiraIssueKey)
	}
	if !strings.Contains(integ.PRCommentMarkdown, "PROJ-42") {
		t.Errorf("expected PROJ-42 in PR comment markdown, got:\n%s", integ.PRCommentMarkdown)
	}
}

// TestBuildIntegrationsNoFactors verifies the "no factors" placeholder appears
// when the factors slice is empty.
func TestBuildIntegrationsNoFactors(t *testing.T) {
	integ := BuildIntegrations(nil, 0, "origin/main", "", nil, ScoreMath{}, Enforcement{})
	if !strings.Contains(integ.PRCommentMarkdown, "No specific risk factors matched") {
		t.Errorf("expected no-factors placeholder, got:\n%s", integ.PRCommentMarkdown)
	}
}

// TestBuildIntegrationsFactorsListed verifies factor labels appear in the PR comment.
func TestBuildIntegrationsFactorsListed(t *testing.T) {
	factors := []RiskFactor{
		{ID: "domain_auth", Label: "Auth/session/invite area changed", Points: 14, Detail: "1 file(s)"},
	}
	integ := BuildIntegrations(factors, 14.0, "origin/main", "", nil, ScoreMath{FactorsSubtotal: 14}, Enforcement{})
	if !strings.Contains(integ.PRCommentMarkdown, "Auth/session/invite area changed") {
		t.Errorf("expected factor label in PR comment, got:\n%s", integ.PRCommentMarkdown)
	}
}

// TestBuildIntegrationsRequiredActionsListed verifies required action titles
// appear in the PR comment markdown.
func TestBuildIntegrationsRequiredActionsListed(t *testing.T) {
	actions := []RequiredAction{
		{ID: "auth_e2e_gate", Title: "Run auth E2E gate", Checklist: []string{"Run smoke suite"}},
	}
	integ := BuildIntegrations(nil, 50.0, "origin/main", "", actions, ScoreMath{}, Enforcement{})
	if !strings.Contains(integ.PRCommentMarkdown, "Run auth E2E gate") {
		t.Errorf("expected action title in PR comment, got:\n%s", integ.PRCommentMarkdown)
	}
	if !strings.Contains(integ.PRCommentMarkdown, "Run smoke suite") {
		t.Errorf("expected checklist item in PR comment, got:\n%s", integ.PRCommentMarkdown)
	}
}

// TestBuildIntegrationsRequiredActionsNone verifies the "None" placeholder appears
// when no required actions are provided.
func TestBuildIntegrationsRequiredActionsNone(t *testing.T) {
	integ := BuildIntegrations(nil, 10.0, "origin/main", "", nil, ScoreMath{}, Enforcement{})
	if !strings.Contains(integ.PRCommentMarkdown, "_None._") {
		t.Errorf("expected _None._ placeholder for empty actions, got:\n%s", integ.PRCommentMarkdown)
	}
}

// TestBuildIntegrationsMoreThanFiveActions verifies that when more than 5 actions
// are present, the overflow count is shown and only 5 are printed.
func TestBuildIntegrationsMoreThanFiveActions(t *testing.T) {
	actions := make([]RequiredAction, 7)
	for i := range actions {
		actions[i] = RequiredAction{ID: "a", Title: "Action item"}
	}
	integ := BuildIntegrations(nil, 60.0, "origin/main", "", actions, ScoreMath{}, Enforcement{})
	md := integ.PRCommentMarkdown
	if !strings.Contains(md, "and 2 more") {
		t.Errorf("expected overflow note 'and 2 more', got:\n%s", md)
	}
}

// TestBuildIntegrationsBaseRefInHeader verifies the base ref appears in the score line.
func TestBuildIntegrationsBaseRefInHeader(t *testing.T) {
	integ := BuildIntegrations(nil, 15.0, "refs/heads/main", "", nil, ScoreMath{}, Enforcement{})
	if !strings.Contains(integ.PRCommentMarkdown, "refs/heads/main") {
		t.Errorf("expected base ref in PR comment header, got:\n%s", integ.PRCommentMarkdown)
	}
}

func TestBuildIntegrationsEnforcementSections(t *testing.T) {
	enf := Enforcement{
		MergeRecommendation: "block",
		Rationale:           "High risk band.",
		RecommendedReview: RecommendedReview{
			Strategy:     "Do not merge until checklist is complete.",
			RoutingHints: []string{"Include a reviewer familiar with auth flows."},
		},
		RequiredValidations: []string{
			"ci: required status checks must pass before merge",
			"test: auth/session evidence",
		},
	}
	integ := BuildIntegrations(nil, 72.0, "origin/main", "", nil, ScoreMath{}, enf)
	md := integ.PRCommentMarkdown
	if !strings.Contains(md, "Merge recommendation") || !strings.Contains(md, "BLOCK") {
		t.Errorf("expected merge recommendation block, got:\n%s", md)
	}
	if !strings.Contains(md, "ci: required status checks") {
		t.Errorf("expected validation line in comment, got:\n%s", md)
	}
	if !strings.Contains(md, "Review routing") || !strings.Contains(md, "auth flows") {
		t.Errorf("expected routing hint in comment, got:\n%s", md)
	}
	if !strings.Contains(md, "Review strategy") {
		t.Errorf("expected review strategy in comment, got:\n%s", md)
	}
}
