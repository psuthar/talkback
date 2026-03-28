package prrisk

import (
	"strings"
	"testing"

	riskcontext "github.com/psuthar/talkback/internal/prrisk/context"
)

// TestActionsAuthGateAtHighRisk verifies auth_e2e_gate fires at high risk.
func TestActionsAuthGateAtHighRisk(t *testing.T) {
	s := Signals{DomainHits: map[string]int{DomainAuth: 1}}
	factors := []RiskFactor{{ID: "domain_auth", Points: 14}}
	acts := ComputeRequiredActions(s, factors, nil, 50, "high", nil)
	if !hasAction(acts, "auth_e2e_gate") {
		t.Error("expected auth_e2e_gate at high risk with auth domain")
	}
}

// TestActionsAuthGateNotFiredAtLowRisk verifies auth_e2e_gate is suppressed at low risk.
func TestActionsAuthGateNotFiredAtLowRisk(t *testing.T) {
	s := Signals{DomainHits: map[string]int{DomainAuth: 1}}
	factors := []RiskFactor{{ID: "domain_auth", Points: 14}}
	acts := ComputeRequiredActions(s, factors, nil, 10, "low", nil)
	if hasAction(acts, "auth_e2e_gate") {
		t.Error("auth_e2e_gate should not fire at low risk score (< 45)")
	}
}

// TestActionsMigrationsGate verifies migrations_validation_gate fires at high risk.
func TestActionsMigrationsGate(t *testing.T) {
	s := Signals{DomainHits: map[string]int{DomainMigrations: 1}}
	factors := []RiskFactor{{ID: "domain_migrations", Points: 22}}
	acts := ComputeRequiredActions(s, factors, nil, 50, "high", nil)
	if !hasAction(acts, "migrations_validation_gate") {
		t.Error("expected migrations_validation_gate action")
	}
}

// TestActionsRAGGate verifies rag_qna_citations_gate fires at high risk.
func TestActionsRAGGate(t *testing.T) {
	s := Signals{DomainHits: map[string]int{DomainRAG: 1}}
	factors := []RiskFactor{{ID: "domain_rag", Points: 10}}
	acts := ComputeRequiredActions(s, factors, nil, 50, "high", nil)
	if !hasAction(acts, "rag_qna_citations_gate") {
		t.Error("expected rag_qna_citations_gate action")
	}
}

// TestActionsProcessingGate verifies materials_processing_gate fires at high risk.
func TestActionsProcessingGate(t *testing.T) {
	s := Signals{DomainHits: map[string]int{DomainProcessing: 1}}
	factors := []RiskFactor{{ID: "domain_processing", Points: 10}}
	acts := ComputeRequiredActions(s, factors, nil, 50, "high", nil)
	if !hasAction(acts, "materials_processing_gate") {
		t.Error("expected materials_processing_gate action")
	}
}

// TestActionsWorkflowConfigValidation verifies workflow_config_validation fires at high risk.
func TestActionsWorkflowConfigValidation(t *testing.T) {
	s := Signals{DomainHits: map[string]int{DomainWorkflows: 1}}
	factors := []RiskFactor{{ID: "ci_workflows", Points: 12}}
	acts := ComputeRequiredActions(s, factors, nil, 50, "high", nil)
	if !hasAction(acts, "workflow_config_validation") {
		t.Error("expected workflow_config_validation action")
	}
}

// TestActionsValidationNoteAdjustsWorkflowMessage verifies the checklist wording
// changes when a validation note is present.
func TestActionsValidationNoteAdjustsWorkflowMessage(t *testing.T) {
	s := Signals{
		DomainHits:          map[string]int{DomainWorkflows: 1},
		ValidationNoteFound: true,
	}
	factors := []RiskFactor{{ID: "ci_workflows", Points: 12}}
	acts := ComputeRequiredActions(s, factors, nil, 50, "high", nil)
	a := findAction(acts, "workflow_config_validation")
	if a == nil {
		t.Fatal("expected workflow_config_validation action")
	}
	if len(a.Checklist) == 0 {
		t.Fatal("expected non-empty checklist")
	}
	if a.Checklist[0] == "Confirm required checks and env parity before merge." {
		t.Error("checklist should acknowledge existing validation note")
	}
}

// TestActionsTestsMissingAtHighRisk verifies add_tests_or_evidence fires at high risk.
func TestActionsTestsMissingAtHighRisk(t *testing.T) {
	s := Signals{DomainHits: map[string]int{DomainAuth: 1}}
	factors := []RiskFactor{
		{ID: "domain_auth", Points: 14},
		{ID: "tests_missing", Points: 18},
	}
	acts := ComputeRequiredActions(s, factors, nil, 50, "high", nil)
	if !hasAction(acts, "add_tests_or_evidence") {
		t.Error("expected add_tests_or_evidence at high risk")
	}
}

// TestActionsTestsMissingAtMediumRisk verifies add_tests_or_evidence still fires
// at medium risk (the non-gateHigh path).
func TestActionsTestsMissingAtMediumRisk(t *testing.T) {
	s := Signals{DomainHits: map[string]int{}}
	factors := []RiskFactor{{ID: "tests_missing", Points: 18}}
	acts := ComputeRequiredActions(s, factors, nil, 30, "medium", nil)
	if !hasAction(acts, "add_tests_or_evidence") {
		t.Error("expected add_tests_or_evidence at medium risk")
	}
}

// TestActionsTestsMissingNotDuplicated verifies add_tests_or_evidence appears
// exactly once even when both the high and medium paths could produce it.
func TestActionsTestsMissingNotDuplicated(t *testing.T) {
	s := Signals{DomainHits: map[string]int{DomainAuth: 1}}
	factors := []RiskFactor{
		{ID: "domain_auth", Points: 14},
		{ID: "tests_missing", Points: 18},
	}
	acts := ComputeRequiredActions(s, factors, nil, 50, "high", nil)
	count := 0
	for _, a := range acts {
		if a.ID == "add_tests_or_evidence" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected exactly 1 add_tests_or_evidence, got %d", count)
	}
}

// TestActionsGitUnavailable verifies ci_fetch_depth_zero fires on git error.
func TestActionsGitUnavailable(t *testing.T) {
	s := Signals{GitError: "no merge base", DomainHits: map[string]int{}}
	factors := []RiskFactor{{ID: "git_unavailable", Points: 25}}
	acts := ComputeRequiredActions(s, factors, nil, 25, "medium", nil)
	if !hasAction(acts, "ci_fetch_depth_zero") {
		t.Error("expected ci_fetch_depth_zero action for git error")
	}
}

// TestActionsPRReviewSummaryForLargeDiff verifies pr_review_summary fires whenever a
// large-diff factor is present — unconditionally, regardless of risk band.
func TestActionsPRReviewSummaryForLargeDiff(t *testing.T) {
	s := Signals{DomainHits: map[string]int{}}
	factors := []RiskFactor{{ID: "diff_very_large", Points: 22}}
	acts := ComputeRequiredActions(s, factors, nil, 50, "high", nil)
	if !hasAction(acts, "pr_review_summary") {
		t.Error("expected pr_review_summary for large diff at high risk")
	}
}

// TestActionsPRReviewSummaryUnconditional verifies pr_review_summary also fires at low
// risk — the action is not gated on risk band, only on the presence of the factor.
func TestActionsPRReviewSummaryUnconditional(t *testing.T) {
	s := Signals{DomainHits: map[string]int{}}
	factors := []RiskFactor{{ID: "diff_large", Points: 12}}
	acts := ComputeRequiredActions(s, factors, nil, 5, "low", nil)
	if !hasAction(acts, "pr_review_summary") {
		t.Error("pr_review_summary should fire for large diff even at low risk band")
	}
}

// TestActionsNoneForEmptyDiff verifies no required actions for a clean empty diff.
func TestActionsNoneForEmptyDiff(t *testing.T) {
	s := Signals{DomainHits: map[string]int{}}
	acts := ComputeRequiredActions(s, nil, nil, 0, "low", nil)
	if len(acts) != 0 {
		t.Errorf("expected no actions for empty diff, got %d", len(acts))
	}
}

// TestActionsContextIntentMismatch verifies context_align_pr_description fires on mismatch.
func TestActionsContextIntentMismatch(t *testing.T) {
	s := Signals{DomainHits: map[string]int{}}
	ci := &riskcontext.ContextInsights{
		Intent: riskcontext.IntentInsight{Mismatch: true, DomainsExpected: []string{"auth"}},
	}
	acts := ComputeRequiredActions(s, nil, nil, 10, "low", ci)
	if !hasAction(acts, "context_align_pr_description") {
		t.Error("expected context_align_pr_description for intent mismatch")
	}
}

// TestActionsContextScatteredWithEnoughFiles verifies context_scattered_review_plan fires
// when concentration is scattered and FileCount >= 10.
func TestActionsContextScatteredWithEnoughFiles(t *testing.T) {
	s := Signals{FileCount: 10, DomainHits: map[string]int{}}
	ci := &riskcontext.ContextInsights{
		Concentration: riskcontext.ConcentrationInsight{Mode: "scattered"},
	}
	acts := ComputeRequiredActions(s, nil, nil, 10, "low", ci)
	if !hasAction(acts, "context_scattered_review_plan") {
		t.Error("expected context_scattered_review_plan for scattered change with 10+ files")
	}
}

// TestActionsContextScatteredRequiresTenFiles verifies context_scattered_review_plan does
// not fire when FileCount < 10, even when concentration is scattered.
func TestActionsContextScatteredRequiresTenFiles(t *testing.T) {
	s := Signals{FileCount: 9, DomainHits: map[string]int{}}
	ci := &riskcontext.ContextInsights{
		Concentration: riskcontext.ConcentrationInsight{Mode: "scattered"},
	}
	acts := ComputeRequiredActions(s, nil, nil, 10, "low", ci)
	if hasAction(acts, "context_scattered_review_plan") {
		t.Error("context_scattered_review_plan should not fire with fewer than 10 files")
	}
}

// TestActionsContextProximityDistantSensitiveDomain verifies context_improve_test_proximity
// fires when proximity is distant, NonTestFiles >= 2, and a sensitive domain is present.
func TestActionsContextProximityDistantSensitiveDomain(t *testing.T) {
	s := Signals{DomainHits: map[string]int{DomainAuth: 1}}
	ci := &riskcontext.ContextInsights{
		Proximity: riskcontext.ProximityInsight{Mode: "distant", NonTestFiles: 3},
	}
	acts := ComputeRequiredActions(s, nil, nil, 10, "low", ci)
	if !hasAction(acts, "context_improve_test_proximity") {
		t.Error("expected context_improve_test_proximity for distant proximity with sensitive domain")
	}
}

// TestActionsContextProximityDistantNonSensitiveDomain verifies context_improve_test_proximity
// does NOT fire when no sensitive domain is present, matching the factor's own guard.
func TestActionsContextProximityDistantNonSensitiveDomain(t *testing.T) {
	s := Signals{DomainHits: map[string]int{DomainWeb: 2, DomainScripts: 1}}
	ci := &riskcontext.ContextInsights{
		Proximity: riskcontext.ProximityInsight{Mode: "distant", NonTestFiles: 3},
	}
	acts := ComputeRequiredActions(s, nil, nil, 10, "low", ci)
	if hasAction(acts, "context_improve_test_proximity") {
		t.Error("context_improve_test_proximity should not fire when no sensitive domain is present")
	}
}

// TestActionsContextHotspot verifies context_hotspot_regression_focus fires when hotspots exist.
func TestActionsContextHotspot(t *testing.T) {
	s := Signals{DomainHits: map[string]int{}}
	ci := &riskcontext.ContextInsights{
		Hotspots: []riskcontext.HotspotInsight{{Prefix: "internal/auth", RecentCount: 8}},
	}
	acts := ComputeRequiredActions(s, nil, nil, 10, "low", ci)
	if !hasAction(acts, "context_hotspot_regression_focus") {
		t.Error("expected context_hotspot_regression_focus when hotspots are present")
	}
}

// TestActionsContextHotspotChecklist verifies the hotspot action checklist names the prefix.
func TestActionsContextHotspotChecklist(t *testing.T) {
	s := Signals{DomainHits: map[string]int{}}
	ci := &riskcontext.ContextInsights{
		Hotspots: []riskcontext.HotspotInsight{{Prefix: "internal/rag", RecentCount: 7}},
	}
	acts := ComputeRequiredActions(s, nil, nil, 10, "low", ci)
	a := findAction(acts, "context_hotspot_regression_focus")
	if a == nil {
		t.Fatal("expected context_hotspot_regression_focus action")
	}
	if len(a.Checklist) == 0 || !strings.Contains(a.Checklist[0], "internal/rag") {
		t.Errorf("expected checklist to reference the hotspot prefix, got %v", a.Checklist)
	}
}

// TestActionsContextNilInsightsNoContextActions verifies no context_ actions fire when insights is nil.
func TestActionsContextNilInsightsNoContextActions(t *testing.T) {
	s := Signals{DomainHits: map[string]int{}}
	acts := ComputeRequiredActions(s, nil, nil, 10, "low", nil)
	for _, a := range acts {
		if strings.HasPrefix(a.ID, "context_") {
			t.Errorf("no context actions expected with nil insights, got %s", a.ID)
		}
	}
}

func hasAction(acts []RequiredAction, id string) bool {
	for _, a := range acts {
		if a.ID == id {
			return true
		}
	}
	return false
}

func findAction(acts []RequiredAction, id string) *RequiredAction {
	for i := range acts {
		if acts[i].ID == id {
			return &acts[i]
		}
	}
	return nil
}
