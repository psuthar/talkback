package prrisk

import "testing"

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

// TestActionsPRReviewSummaryAtHighRisk verifies pr_review_summary fires for large diffs.
func TestActionsPRReviewSummaryAtHighRisk(t *testing.T) {
	s := Signals{DomainHits: map[string]int{}}
	factors := []RiskFactor{{ID: "diff_very_large", Points: 22}}
	acts := ComputeRequiredActions(s, factors, nil, 50, "high", nil)
	if !hasAction(acts, "pr_review_summary") {
		t.Error("expected pr_review_summary for large diff at high risk")
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
