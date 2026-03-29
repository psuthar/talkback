package prrisk

import (
	"strings"
	"testing"

	riskcontext "github.com/psuthar/talkback/internal/prrisk/context"
)

// --- evidenceCIBaseline ---

func TestEvidenceCIBaselineUnknownWhenNoGitError(t *testing.T) {
	// CI pass/fail cannot be determined from the diff alone; UNKNOWN is correct.
	ev := evidenceCIBaseline(Signals{})
	if ev.Status != EvidenceUnknown {
		t.Errorf("want unknown (CI not detectable from diff), got %q (rationale: %s)", ev.Status, ev.Rationale)
	}
	if ev.ID != "ci_baseline" {
		t.Errorf("expected id=ci_baseline, got %q", ev.ID)
	}
}

func TestEvidenceCIBaselineFailOnGitError(t *testing.T) {
	ev := evidenceCIBaseline(Signals{GitError: "no base ref"})
	if ev.Status != EvidenceFail {
		t.Errorf("want fail, got %q", ev.Status)
	}
}

// --- evidenceCIFetchDepth ---

func TestEvidenceCIFetchDepthPassNoError(t *testing.T) {
	ev := evidenceCIFetchDepth("label", Signals{})
	if ev.Status != EvidencePass {
		t.Errorf("want pass, got %q", ev.Status)
	}
}

func TestEvidenceCIFetchDepthFailOnGitError(t *testing.T) {
	ev := evidenceCIFetchDepth("label", Signals{GitError: "shallow checkout"})
	if ev.Status != EvidenceFail {
		t.Errorf("want fail, got %q", ev.Status)
	}
}

// --- evidencePRReviewSummary ---

func TestEvidencePRReviewSummaryPassOnStrong(t *testing.T) {
	ci := &riskcontext.ContextInsights{
		Intent: riskcontext.IntentInsight{IntentStrength: "strong"},
	}
	ev := evidencePRReviewSummary("label", ci)
	if ev.Status != EvidencePass {
		t.Errorf("want pass, got %q", ev.Status)
	}
}

func TestEvidencePRReviewSummaryMissingOnWeakWithTitle(t *testing.T) {
	ci := &riskcontext.ContextInsights{
		Intent: riskcontext.IntentInsight{IntentStrength: "weak", Title: "wip"},
	}
	ev := evidencePRReviewSummary("label", ci)
	if ev.Status != EvidenceMissing {
		t.Errorf("want missing, got %q", ev.Status)
	}
}

func TestEvidencePRReviewSummaryMissingOnWeakNoTitle(t *testing.T) {
	ci := &riskcontext.ContextInsights{
		Intent: riskcontext.IntentInsight{IntentStrength: "weak"},
	}
	ev := evidencePRReviewSummary("label", ci)
	if ev.Status != EvidenceMissing {
		t.Errorf("want missing for weak intent + no title, got %q", ev.Status)
	}
}

func TestEvidencePRReviewSummaryNotEvaluatedOnUnknownStrength(t *testing.T) {
	ci := &riskcontext.ContextInsights{
		Intent: riskcontext.IntentInsight{IntentStrength: "unknown"},
	}
	ev := evidencePRReviewSummary("label", ci)
	if ev.Status != EvidenceNotEvaluated {
		t.Errorf("want not_evaluated (quality indeterminate, human review required), got %q", ev.Status)
	}
}

func TestEvidencePRReviewSummaryUnknownOnNilInsights(t *testing.T) {
	ev := evidencePRReviewSummary("label", nil)
	if ev.Status != EvidenceUnknown {
		t.Errorf("want unknown when context insights nil, got %q", ev.Status)
	}
}

// --- evidenceWorkflowConfig ---

func TestEvidenceWorkflowConfigPassOnValidationNote(t *testing.T) {
	ev := evidenceWorkflowConfig("label", Signals{ValidationNoteFound: true, ValidationNoteSnippet: "Validation: ran staging"})
	if ev.Status != EvidencePass {
		t.Errorf("want pass, got %q", ev.Status)
	}
}

func TestEvidenceWorkflowConfigNotEvaluatedWithoutNote(t *testing.T) {
	ev := evidenceWorkflowConfig("label", Signals{})
	if ev.Status != EvidenceNotEvaluated {
		t.Errorf("want not_evaluated (CI result not confirmable without note), got %q", ev.Status)
	}
}

// --- evidenceTestDomain ---

func TestEvidenceTestDomainPassOnE2EHit(t *testing.T) {
	ev := evidenceTestDomain("auth_e2e_gate", "label", DomainAuth,
		Signals{TestE2EDomainHits: map[string]int{DomainAuth: 2}})
	if ev.Status != EvidencePass {
		t.Errorf("want pass on E2E hit, got %q", ev.Status)
	}
}

func TestEvidenceTestDomainNotEvaluatedOnUnitOnlyHit(t *testing.T) {
	ev := evidenceTestDomain("auth_e2e_gate", "label", DomainAuth,
		Signals{
			TestUnitDomainHits: map[string]int{DomainAuth: 1},
		})
	if ev.Status != EvidenceNotEvaluated {
		t.Errorf("want not_evaluated for unit-only hit (E2E not confirmed), got %q", ev.Status)
	}
}

func TestEvidenceTestDomainMissingOnNoHits(t *testing.T) {
	ev := evidenceTestDomain("auth_e2e_gate", "label", DomainAuth, Signals{})
	if ev.Status != EvidenceMissing {
		t.Errorf("want missing when no test domain hits, got %q", ev.Status)
	}
}

func TestEvidenceTestDomainE2ETakesPriorityOverUnit(t *testing.T) {
	// Both unit and E2E hits present — E2E wins (pass, not unknown).
	ev := evidenceTestDomain("auth_e2e_gate", "label", DomainAuth,
		Signals{
			TestUnitDomainHits: map[string]int{DomainAuth: 1},
			TestE2EDomainHits:  map[string]int{DomainAuth: 3},
		})
	if ev.Status != EvidencePass {
		t.Errorf("want pass when E2E hit present regardless of unit, got %q", ev.Status)
	}
}

// --- evidenceMigrationsGate ---

func TestEvidenceMigrationsGatePassOnE2EHit(t *testing.T) {
	ev := evidenceMigrationsGate("label",
		Signals{TestE2EDomainHits: map[string]int{DomainMigrations: 1}})
	if ev.Status != EvidencePass {
		t.Errorf("want pass on E2E migrations hit, got %q", ev.Status)
	}
}

func TestEvidenceMigrationsGatePassOnNoteAndMigrationFiles(t *testing.T) {
	ev := evidenceMigrationsGate("label",
		Signals{ValidationNoteFound: true, MigrationFiles: 2})
	if ev.Status != EvidencePass {
		t.Errorf("want pass on validation note + migration files, got %q", ev.Status)
	}
}

func TestEvidenceMigrationsGateMissingOnFilesNoNote(t *testing.T) {
	ev := evidenceMigrationsGate("label", Signals{MigrationFiles: 1})
	if ev.Status != EvidenceMissing {
		t.Errorf("want missing when migration files present but no note or E2E, got %q", ev.Status)
	}
}

func TestEvidenceMigrationsGateUnknownNoMigrationFiles(t *testing.T) {
	ev := evidenceMigrationsGate("label", Signals{})
	if ev.Status != EvidenceUnknown {
		t.Errorf("want unknown when no migration files, got %q", ev.Status)
	}
}

// --- evidenceAddTests ---

func TestEvidenceAddTestsPassOnAdequateBehavioralCoverage(t *testing.T) {
	ci := &riskcontext.ContextInsights{
		Proximity: riskcontext.ProximityInsight{BehavioralCoverage: "adequate"},
	}
	ev := evidenceAddTests("label", Signals{TestFiles: 2}, ci)
	if ev.Status != EvidencePass {
		t.Errorf("want pass on adequate behavioral coverage, got %q", ev.Status)
	}
}

func TestEvidenceAddTestsPassOnHighTestLOCRatio(t *testing.T) {
	ev := evidenceAddTests("label", Signals{TestLOCRatio: 0.40, TestFiles: 3}, nil)
	if ev.Status != EvidencePass {
		t.Errorf("want pass on test LOC ratio >= 0.30, got %q", ev.Status)
	}
}

func TestEvidenceAddTestsMissingOnNoTestFiles(t *testing.T) {
	ev := evidenceAddTests("label", Signals{TestFiles: 0}, nil)
	if ev.Status != EvidenceMissing {
		t.Errorf("want missing when no test files, got %q", ev.Status)
	}
}

func TestEvidenceAddTestsNotEvaluatedOnShallowCoverage(t *testing.T) {
	ci := &riskcontext.ContextInsights{
		Proximity: riskcontext.ProximityInsight{BehavioralCoverage: "shallow"},
	}
	ev := evidenceAddTests("label", Signals{TestFiles: 1, TestLOCRatio: 0.10}, ci)
	if ev.Status != EvidenceNotEvaluated {
		t.Errorf("want not_evaluated on shallow coverage (E2E not confirmed), got %q", ev.Status)
	}
}

// --- evidenceIntentAlignment ---

func TestEvidenceIntentAlignmentPassWhenAligned(t *testing.T) {
	ci := &riskcontext.ContextInsights{
		Intent: riskcontext.IntentInsight{Aligned: true},
	}
	ev := evidenceIntentAlignment("label", ci)
	if ev.Status != EvidencePass {
		t.Errorf("want pass when aligned, got %q", ev.Status)
	}
}

func TestEvidenceIntentAlignmentFailOnMismatch(t *testing.T) {
	ci := &riskcontext.ContextInsights{
		Intent: riskcontext.IntentInsight{Mismatch: true, Detail: "auth expected but not in diff"},
	}
	ev := evidenceIntentAlignment("label", ci)
	if ev.Status != EvidenceFail {
		t.Errorf("want fail on mismatch, got %q", ev.Status)
	}
}

func TestEvidenceIntentAlignmentNotEvaluatedWhenNeitherAlignedNorMismatch(t *testing.T) {
	ci := &riskcontext.ContextInsights{
		Intent: riskcontext.IntentInsight{},
	}
	ev := evidenceIntentAlignment("label", ci)
	if ev.Status != EvidenceNotEvaluated {
		t.Errorf("want not_evaluated when no alignment verdict, got %q", ev.Status)
	}
}

func TestEvidenceIntentAlignmentUnknownOnNilInsights(t *testing.T) {
	ev := evidenceIntentAlignment("label", nil)
	if ev.Status != EvidenceUnknown {
		t.Errorf("want unknown on nil insights, got %q", ev.Status)
	}
}

// --- evidenceTestProximity ---

func TestEvidenceTestProximityPassOnAdequate(t *testing.T) {
	ci := &riskcontext.ContextInsights{
		Proximity: riskcontext.ProximityInsight{BehavioralCoverage: "adequate", Mode: "co_located"},
	}
	ev := evidenceTestProximity("label", ci)
	if ev.Status != EvidencePass {
		t.Errorf("want pass on adequate coverage, got %q", ev.Status)
	}
}

func TestEvidenceTestProximityMissingOnDistantUnknown(t *testing.T) {
	ci := &riskcontext.ContextInsights{
		Proximity: riskcontext.ProximityInsight{Mode: "distant", BehavioralCoverage: "unknown"},
	}
	ev := evidenceTestProximity("label", ci)
	if ev.Status != EvidenceMissing {
		t.Errorf("want missing for distant+unknown, got %q", ev.Status)
	}
}

func TestEvidenceTestProximityNotEvaluatedOnPartialShallow(t *testing.T) {
	ci := &riskcontext.ContextInsights{
		Proximity: riskcontext.ProximityInsight{Mode: "partial", BehavioralCoverage: "shallow"},
	}
	ev := evidenceTestProximity("label", ci)
	if ev.Status != EvidenceNotEvaluated {
		t.Errorf("want not_evaluated for partial+shallow (requires human review), got %q", ev.Status)
	}
}

// --- evidenceHotspotRegression ---

func TestEvidenceHotspotRegressionPassOnValidationNote(t *testing.T) {
	ev := evidenceHotspotRegression("label", Signals{ValidationNoteFound: true})
	if ev.Status != EvidencePass {
		t.Errorf("want pass on validation note, got %q", ev.Status)
	}
}

func TestEvidenceHotspotRegressionNotEvaluatedWithoutNote(t *testing.T) {
	ev := evidenceHotspotRegression("label", Signals{})
	if ev.Status != EvidenceNotEvaluated {
		t.Errorf("want not_evaluated without validation note (requires human review), got %q", ev.Status)
	}
}

// --- evidenceScatteredReviewPlan ---

func TestEvidenceScatteredReviewPlanPassOnStrongAligned(t *testing.T) {
	ci := &riskcontext.ContextInsights{
		Intent: riskcontext.IntentInsight{IntentStrength: "strong", Aligned: true},
	}
	ev := evidenceScatteredReviewPlan("label", ci)
	if ev.Status != EvidencePass {
		t.Errorf("want pass on strong+aligned, got %q", ev.Status)
	}
}

func TestEvidenceScatteredReviewPlanMissingOnWeakStrength(t *testing.T) {
	ci := &riskcontext.ContextInsights{
		Intent: riskcontext.IntentInsight{IntentStrength: "weak"},
	}
	ev := evidenceScatteredReviewPlan("label", ci)
	if ev.Status != EvidenceMissing {
		t.Errorf("want missing on weak strength, got %q", ev.Status)
	}
}

func TestEvidenceScatteredReviewPlanNotEvaluatedOnUnknownStrength(t *testing.T) {
	// "unknown" means no domain keywords matched — we cannot confirm the review plan covers all areas.
	// This is NOT_EVALUATED (requires human review), not MISSING (which would overclaim absence).
	ci := &riskcontext.ContextInsights{
		Intent: riskcontext.IntentInsight{IntentStrength: "unknown"},
	}
	ev := evidenceScatteredReviewPlan("label", ci)
	if ev.Status != EvidenceNotEvaluated {
		t.Errorf("want not_evaluated on unknown strength (no keywords matched), got %q", ev.Status)
	}
}

func TestEvidenceScatteredReviewPlanUnknownOnNilInsights(t *testing.T) {
	ev := evidenceScatteredReviewPlan("label", nil)
	if ev.Status != EvidenceUnknown {
		t.Errorf("want unknown on nil insights, got %q", ev.Status)
	}
}

// --- evidenceAwareUpgrade ---

func TestEvidenceAwareUpgradeNoOpOnUnknown(t *testing.T) {
	evidence := []ValidationEvidence{
		{ID: "auth_e2e_gate", Status: EvidenceUnknown},
	}
	actions := []RequiredAction{
		{ID: "auth_e2e_gate", Priority: PriorityHigh},
	}
	got := evidenceAwareUpgrade("pass", evidence, actions)
	if got != "pass" {
		t.Errorf("unknown evidence should not upgrade, got %q", got)
	}
}

func TestEvidenceAwareUpgradeNoOpOnNotEvaluated(t *testing.T) {
	// not_evaluated items require human review but should not trigger machine upgrades.
	evidence := []ValidationEvidence{
		{ID: "auth_e2e_gate", Status: EvidenceNotEvaluated},
	}
	actions := []RequiredAction{
		{ID: "auth_e2e_gate", Priority: PriorityHigh},
	}
	got := evidenceAwareUpgrade("pass", evidence, actions)
	if got != "pass" {
		t.Errorf("not_evaluated evidence should not upgrade (requires human review, not machine block), got %q", got)
	}
}

func TestEvidenceAwareUpgradePassToWarnOnHighMissing(t *testing.T) {
	evidence := []ValidationEvidence{
		{ID: "auth_e2e_gate", Status: EvidenceMissing},
	}
	actions := []RequiredAction{
		{ID: "auth_e2e_gate", Priority: PriorityHigh},
	}
	got := evidenceAwareUpgrade("pass", evidence, actions)
	if got != "warn" {
		t.Errorf("want warn upgrade on high-priority missing, got %q", got)
	}
}

func TestEvidenceAwareUpgradeWarnToBlockOnHighFail(t *testing.T) {
	evidence := []ValidationEvidence{
		{ID: "auth_e2e_gate", Status: EvidenceFail},
	}
	actions := []RequiredAction{
		{ID: "auth_e2e_gate", Priority: PriorityHigh},
	}
	got := evidenceAwareUpgrade("warn", evidence, actions)
	if got != "block" {
		t.Errorf("want block upgrade on high-priority fail, got %q", got)
	}
}

func TestEvidenceAwareUpgradePassToBlockOnHighFail(t *testing.T) {
	evidence := []ValidationEvidence{
		{ID: "ci_fetch_depth_zero", Status: EvidenceFail},
	}
	actions := []RequiredAction{
		{ID: "ci_fetch_depth_zero", Priority: PriorityHigh},
	}
	got := evidenceAwareUpgrade("pass", evidence, actions)
	if got != "block" {
		t.Errorf("want block from pass when high-priority fail, got %q", got)
	}
}

func TestEvidenceAwareUpgradeMediumMissingDoesNotUpgrade(t *testing.T) {
	// MEDIUM-priority missing should not change the recommendation.
	evidence := []ValidationEvidence{
		{ID: "workflow_config_validation", Status: EvidenceMissing},
	}
	actions := []RequiredAction{
		{ID: "workflow_config_validation", Priority: PriorityMedium},
	}
	got := evidenceAwareUpgrade("pass", evidence, actions)
	if got != "pass" {
		t.Errorf("medium-priority missing should not upgrade, got %q", got)
	}
}

func TestEvidenceAwareUpgradeBlockUnchanged(t *testing.T) {
	// Already block — no change regardless of evidence.
	evidence := []ValidationEvidence{
		{ID: "auth_e2e_gate", Status: EvidenceFail},
	}
	actions := []RequiredAction{
		{ID: "auth_e2e_gate", Priority: PriorityHigh},
	}
	got := evidenceAwareUpgrade("block", evidence, actions)
	if got != "block" {
		t.Errorf("block should stay block, got %q", got)
	}
}

func TestEvidenceAwareUpgradePriorityInferredFromID(t *testing.T) {
	// Priority not set explicitly — should be inferred from action ID.
	evidence := []ValidationEvidence{
		{ID: "add_tests_or_evidence", Status: EvidenceMissing},
	}
	actions := []RequiredAction{
		{ID: "add_tests_or_evidence"}, // Priority intentionally empty
	}
	got := evidenceAwareUpgrade("pass", evidence, actions)
	if got != "warn" {
		t.Errorf("want warn: add_tests_or_evidence is HIGH priority by default, got %q", got)
	}
}

// --- ComputeEvidenceStatus summary counts ---

func TestComputeEvidenceStatusSummaryCountsAreCorrect(t *testing.T) {
	r := Result{
		Signals: Signals{GitError: ""},
		RequiredActions: []RequiredAction{
			{ID: "auth_e2e_gate", Title: "Auth E2E gate", Priority: PriorityHigh},
		},
	}
	evs, summary := ComputeEvidenceStatus(r)

	// ci_baseline is suppressed when git is healthy — always-unknown is noise.
	// Only auth_e2e_gate (missing: no test domain hits) = 1 entry.
	if len(evs) != 1 {
		t.Fatalf("expected 1 entry (auth_e2e_gate); ci_baseline suppressed when git healthy, got %d", len(evs))
	}
	if evs[0].ID != "auth_e2e_gate" {
		t.Errorf("first entry should be auth_e2e_gate, got %q", evs[0].ID)
	}
	if evs[0].Status != EvidenceMissing {
		t.Errorf("auth_e2e_gate should be missing with no test hits, got %q", evs[0].Status)
	}
	if summary.UnknownCount != 0 {
		t.Errorf("want 0 unknown (ci_baseline suppressed), got %d", summary.UnknownCount)
	}
	if summary.NotEvaluatedCount != 0 {
		t.Errorf("want 0 not_evaluated for this simple case, got %d", summary.NotEvaluatedCount)
	}
	if summary.MissingCount != 1 {
		t.Errorf("want 1 missing (auth_e2e_gate), got %d", summary.MissingCount)
	}
	if summary.PassCount != 0 || summary.FailCount != 0 {
		t.Errorf("want 0 pass and 0 fail, got pass=%d fail=%d", summary.PassCount, summary.FailCount)
	}
}

func TestComputeEvidenceStatusNotEvaluatedCountedInSummary(t *testing.T) {
	// workflow_config_validation without a validation note → not_evaluated
	r := Result{
		Signals: Signals{GitError: ""},
		RequiredActions: []RequiredAction{
			{ID: "workflow_config_validation", Title: "Workflow config validation"},
		},
	}
	evs, summary := ComputeEvidenceStatus(r)
	if len(evs) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(evs))
	}
	if evs[0].Status != EvidenceNotEvaluated {
		t.Errorf("want not_evaluated for workflow_config_validation without note, got %q", evs[0].Status)
	}
	if summary.NotEvaluatedCount != 1 {
		t.Errorf("want not_evaluated_count=1, got %d", summary.NotEvaluatedCount)
	}
	if summary.UnknownCount != 0 {
		t.Errorf("want unknown_count=0, got %d", summary.UnknownCount)
	}
}

func TestComputeEvidenceStatusEveryRequiredActionHasEvidenceEntry(t *testing.T) {
	// Every required action should produce exactly one evidence entry.
	r := Result{
		Signals: Signals{GitError: ""},
		RequiredActions: []RequiredAction{
			{ID: "auth_e2e_gate", Title: "Auth E2E gate"},
			{ID: "workflow_config_validation", Title: "Workflow config"},
			{ID: "context_hotspot_regression_focus", Title: "Hotspot regression"},
		},
	}
	evs, _ := ComputeEvidenceStatus(r)
	if len(evs) != 3 {
		t.Fatalf("expected 1 evidence entry per required action (3 total), got %d", len(evs))
	}
	ids := make(map[string]bool)
	for _, ev := range evs {
		ids[ev.ID] = true
	}
	for _, a := range r.RequiredActions {
		if !ids[a.ID] {
			t.Errorf("required action %q has no evidence entry", a.ID)
		}
	}
}

func TestComputeEvidenceStatusGitErrorProducesFailCIBaseline(t *testing.T) {
	r := Result{
		Signals:         Signals{GitError: "fetch-depth: 1"},
		RequiredActions: nil,
	}
	evs, summary := ComputeEvidenceStatus(r)
	if len(evs) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(evs))
	}
	if evs[0].Status != EvidenceFail {
		t.Errorf("ci_baseline should be fail on git error, got %q", evs[0].Status)
	}
	if summary.FailCount != 1 {
		t.Errorf("want fail_count=1, got %d", summary.FailCount)
	}
}

// --- evidenceBlockingReasons ---

func TestEvidenceBlockingReasonsFailAlwaysIncluded(t *testing.T) {
	evidence := []ValidationEvidence{
		{ID: "context_align_pr_description", Status: EvidenceFail, Rationale: "mismatch"},
	}
	// context_align_pr_description is MEDIUM — FAIL still surfaces regardless of priority.
	actions := []RequiredAction{
		{ID: "context_align_pr_description", Priority: PriorityMedium},
	}
	reasons := evidenceBlockingReasons(evidence, actions)
	if len(reasons) != 1 {
		t.Fatalf("expected 1 reason for FAIL, got %d: %v", len(reasons), reasons)
	}
	if !strings.Contains(reasons[0], "context_align_pr_description") {
		t.Errorf("reason should mention action ID, got %q", reasons[0])
	}
}

func TestEvidenceBlockingReasonsHighMissingIncluded(t *testing.T) {
	evidence := []ValidationEvidence{
		{ID: "auth_e2e_gate", Status: EvidenceMissing, Rationale: "no E2E hits"},
	}
	actions := []RequiredAction{
		{ID: "auth_e2e_gate", Priority: PriorityHigh},
	}
	reasons := evidenceBlockingReasons(evidence, actions)
	if len(reasons) != 1 {
		t.Fatalf("expected 1 reason for HIGH missing, got %d: %v", len(reasons), reasons)
	}
}

func TestEvidenceBlockingReasonsMediumMissingExcluded(t *testing.T) {
	evidence := []ValidationEvidence{
		{ID: "workflow_config_validation", Status: EvidenceMissing, Rationale: "no note"},
	}
	actions := []RequiredAction{
		{ID: "workflow_config_validation", Priority: PriorityMedium},
	}
	reasons := evidenceBlockingReasons(evidence, actions)
	if len(reasons) != 0 {
		t.Errorf("medium-priority missing should not produce a blocking reason, got %v", reasons)
	}
}

func TestEvidenceBlockingReasonsUnknownExcluded(t *testing.T) {
	evidence := []ValidationEvidence{
		{ID: "auth_e2e_gate", Status: EvidenceUnknown},
	}
	actions := []RequiredAction{
		{ID: "auth_e2e_gate", Priority: PriorityHigh},
	}
	reasons := evidenceBlockingReasons(evidence, actions)
	if len(reasons) != 0 {
		t.Errorf("unknown status should produce no blocking reason, got %v", reasons)
	}
}

func TestEvidenceBlockingReasonsNotEvaluatedExcluded(t *testing.T) {
	// not_evaluated items require human review but must not produce machine blocking reasons.
	evidence := []ValidationEvidence{
		{ID: "auth_e2e_gate", Status: EvidenceNotEvaluated},
	}
	actions := []RequiredAction{
		{ID: "auth_e2e_gate", Priority: PriorityHigh},
	}
	reasons := evidenceBlockingReasons(evidence, actions)
	if len(reasons) != 0 {
		t.Errorf("not_evaluated status should produce no blocking reason (human review required), got %v", reasons)
	}
}

// --- evidenceStatusIcon ---

func TestEvidenceStatusIconCoversAllStatuses(t *testing.T) {
	cases := []struct {
		status string
		want   string
	}{
		{EvidencePass, "✅"},
		{EvidenceMissing, "⚠️"},
		{EvidenceFail, "❌"},
		{EvidenceNotEvaluated, "📋"},
		{EvidenceUnknown, "❓"},
		{"other", "❓"},
	}
	for _, c := range cases {
		got := evidenceStatusIcon(c.status)
		if got != c.want {
			t.Errorf("icon(%q) = %q, want %q", c.status, got, c.want)
		}
	}
}
