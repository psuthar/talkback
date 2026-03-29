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

// TestBuildIntegrationsNoFactors verifies no "Top risk drivers" section appears
// when the factors slice is empty.
func TestBuildIntegrationsNoFactors(t *testing.T) {
	integ := BuildIntegrations(nil, 0, "origin/main", "", nil, ScoreMath{}, Enforcement{})
	if strings.Contains(integ.PRCommentMarkdown, "Top risk drivers") {
		t.Errorf("expected no risk drivers section for empty factors, got:\n%s", integ.PRCommentMarkdown)
	}
}

// TestBuildIntegrationsFactorsListed verifies factor labels appear in the top risk drivers section.
func TestBuildIntegrationsFactorsListed(t *testing.T) {
	factors := []RiskFactor{
		{ID: "domain_auth", Label: "Auth/session/invite area changed", Points: 14, Detail: "1 file(s)"},
	}
	integ := BuildIntegrations(factors, 14.0, "origin/main", "", nil, ScoreMath{FactorsSubtotal: 14}, Enforcement{})
	md := integ.PRCommentMarkdown
	if !strings.Contains(md, "Top risk drivers") {
		t.Errorf("expected 'Top risk drivers' section, got:\n%s", md)
	}
	if !strings.Contains(md, "Auth/session/invite area changed") {
		t.Errorf("expected factor label in risk drivers section, got:\n%s", md)
	}
}

// TestBuildIntegrationsTopTwoRiskDriversOnly verifies only the top 2 factors are shown
// when more than 2 are present.
func TestBuildIntegrationsTopTwoRiskDriversOnly(t *testing.T) {
	factors := []RiskFactor{
		{ID: "f1", Label: "Factor One", Points: 20},
		{ID: "f2", Label: "Factor Two", Points: 15},
		{ID: "f3", Label: "Factor Three", Points: 10},
	}
	integ := BuildIntegrations(factors, 45.0, "origin/main", "", nil, ScoreMath{FactorsSubtotal: 45}, Enforcement{})
	md := integ.PRCommentMarkdown
	if !strings.Contains(md, "Factor One") {
		t.Errorf("expected first factor, got:\n%s", md)
	}
	if !strings.Contains(md, "Factor Two") {
		t.Errorf("expected second factor, got:\n%s", md)
	}
	if strings.Contains(md, "Factor Three") {
		t.Errorf("expected third factor to be omitted (only top 2), got:\n%s", md)
	}
	if !strings.Contains(md, "and 1 more") {
		t.Errorf("expected overflow note 'and 1 more' for 3 factors, got:\n%s", md)
	}
}

// TestBuildIntegrationsTopTwoRequiredValidationsOnly verifies only the top 2 validations
// are shown when more are present.
func TestBuildIntegrationsTopTwoRequiredValidationsOnly(t *testing.T) {
	enf := Enforcement{
		MergeRecommendation: "warn",
		RequiredValidations: []string{
			"ci: required status checks must pass",
			"test: auth/session/invite flows",
			"test: migrations validation",
			"db: rollback plan",
		},
	}
	integ := BuildIntegrations(nil, 30.0, "origin/main", "", nil, ScoreMath{}, enf)
	md := integ.PRCommentMarkdown
	if !strings.Contains(md, "ci: required status checks") {
		t.Errorf("expected first validation, got:\n%s", md)
	}
	if !strings.Contains(md, "test: auth/session") {
		t.Errorf("expected second validation, got:\n%s", md)
	}
	if strings.Contains(md, "test: migrations") {
		t.Errorf("expected third validation to be omitted (only top 2), got:\n%s", md)
	}
	if !strings.Contains(md, "and 2 more") {
		t.Errorf("expected overflow note 'and 2 more' for 4 validations, got:\n%s", md)
	}
}

// TestBuildIntegrationsTopTwoRoutingHintsOnly verifies only top 2 routing hints are shown.
func TestBuildIntegrationsTopTwoRoutingHintsOnly(t *testing.T) {
	enf := Enforcement{
		RecommendedReview: RecommendedReview{
			RoutingHints: []string{
				"Reviewer familiar with auth flows",
				"Reviewer familiar with database migrations",
				"Reviewer familiar with CI configuration",
			},
		},
	}
	integ := BuildIntegrations(nil, 30.0, "origin/main", "", nil, ScoreMath{}, enf)
	md := integ.PRCommentMarkdown
	if !strings.Contains(md, "auth flows") {
		t.Errorf("expected first routing hint, got:\n%s", md)
	}
	if !strings.Contains(md, "database migrations") {
		t.Errorf("expected second routing hint, got:\n%s", md)
	}
	if strings.Contains(md, "CI configuration") {
		t.Errorf("expected third routing hint to be omitted (only top 2), got:\n%s", md)
	}
	if !strings.Contains(md, "and 1 more") {
		t.Errorf("expected overflow note 'and 1 more' for 3 hints, got:\n%s", md)
	}
}

// TestBuildIntegrationsBaseRefInHeader verifies the base ref appears in the score line.
func TestBuildIntegrationsBaseRefInHeader(t *testing.T) {
	integ := BuildIntegrations(nil, 15.0, "refs/heads/main", "", nil, ScoreMath{}, Enforcement{})
	if !strings.Contains(integ.PRCommentMarkdown, "refs/heads/main") {
		t.Errorf("expected base ref in PR comment header, got:\n%s", integ.PRCommentMarkdown)
	}
}

// TestBuildIntegrationsEnforcementSections verifies merge recommendation, validations,
// routing, and evidence summary appear in the comment.
func TestBuildIntegrationsEnforcementSections(t *testing.T) {
	enf := Enforcement{
		MergeRecommendation: "block",
		Rationale:           "High risk band.",
		RecommendedReview: RecommendedReview{
			RoutingHints: []string{"Include a reviewer familiar with auth flows."},
		},
		RequiredValidations: []string{
			"ci: required status checks must pass before merge",
			"test: auth/session evidence",
		},
		EvidenceSummary: EvidenceSummary{PassCount: 1, MissingCount: 2, UnknownCount: 3, FailCount: 0},
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
	if !strings.Contains(md, "Evidence:") {
		t.Errorf("expected evidence summary line in comment, got:\n%s", md)
	}
	// Review strategy is in the full report (pr_risk.md), not in the PR comment.
	if strings.Contains(md, "Review strategy") {
		t.Errorf("review strategy should not appear in the short PR comment, got:\n%s", md)
	}
}

// TestBuildIntegrationsLinkToFullReport verifies the PR comment includes the pr_risk.md link.
func TestBuildIntegrationsLinkToFullReport(t *testing.T) {
	integ := BuildIntegrations(nil, 10.0, "origin/main", "", nil, ScoreMath{}, Enforcement{})
	if !strings.Contains(integ.PRCommentMarkdown, "pr_risk.md") {
		t.Errorf("expected link to pr_risk.md in PR comment, got:\n%s", integ.PRCommentMarkdown)
	}
}
