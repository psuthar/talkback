package prrisk

import (
	"fmt"

	riskcontext "github.com/psuthar/talkback/internal/prrisk/context"
)

// ComputeEvidenceStatus derives per-action evidence status from repo-local signals.
// It always produces a ci_baseline entry, followed by one entry per RequiredAction.
// All detection is deterministic and requires no LLM or live GitHub API.
func ComputeEvidenceStatus(r Result) ([]ValidationEvidence, EvidenceSummary) {
	var out []ValidationEvidence

	// ci_baseline is always evaluated regardless of required actions.
	out = append(out, evidenceCIBaseline(r.Signals))

	for _, a := range r.RequiredActions {
		out = append(out, evidenceForActionID(a.ID, a.Title, r))
	}

	var summary EvidenceSummary
	for _, e := range out {
		switch e.Status {
		case EvidencePass:
			summary.PassCount++
		case EvidenceMissing:
			summary.MissingCount++
		case EvidenceFail:
			summary.FailCount++
		default:
			summary.UnknownCount++
		}
	}
	return out, summary
}

// evidenceForActionID dispatches to the appropriate per-action detector.
func evidenceForActionID(id, label string, r Result) ValidationEvidence {
	s := r.Signals
	ci := r.ContextInsights
	switch id {
	case "ci_fetch_depth_zero":
		return evidenceCIFetchDepth(label, s)
	case "pr_review_summary":
		return evidencePRReviewSummary(label, ci)
	case "workflow_config_validation":
		return evidenceWorkflowConfig(label, s)
	case "auth_e2e_gate":
		return evidenceTestDomain(id, label, DomainAuth, s)
	case "rag_qna_citations_gate":
		return evidenceTestDomain(id, label, DomainRAG, s)
	case "materials_processing_gate":
		return evidenceTestDomain(id, label, DomainProcessing, s)
	case "migrations_validation_gate":
		return evidenceMigrationsGate(label, s)
	case "add_tests_or_evidence":
		return evidenceAddTests(label, s, ci)
	case "context_align_pr_description":
		return evidenceIntentAlignment(label, ci)
	case "context_scattered_review_plan":
		return evidenceScatteredReviewPlan(label, ci)
	case "context_improve_test_proximity":
		return evidenceTestProximity(label, ci)
	case "context_hotspot_regression_focus":
		return evidenceHotspotRegression(label, s)
	default:
		return ValidationEvidence{
			ID:        id,
			Label:     label,
			Status:    EvidenceUnknown,
			Source:    "none",
			Rationale: "No repo-local evidence detector defined for this action.",
		}
	}
}

// evidenceCIBaseline evaluates baseline CI readiness from git signals.
// FAIL when git history was unavailable; UNKNOWN otherwise (CI pass/fail needs live API).
func evidenceCIBaseline(s Signals) ValidationEvidence {
	if s.GitError != "" {
		return ValidationEvidence{
			ID:        "ci_baseline",
			Label:     "CI: required status checks",
			Status:    EvidenceFail,
			Source:    "git_signals",
			Rationale: "Git diff unavailable: " + s.GitError,
		}
	}
	return ValidationEvidence{
		ID:        "ci_baseline",
		Label:     "CI: required status checks",
		Status:    EvidenceUnknown,
		Source:    "git_signals",
		Rationale: "Git history available; CI check pass/fail cannot be determined from diff alone.",
	}
}

// evidenceCIFetchDepth evaluates whether the git checkout had sufficient history.
// PASS when no git error; FAIL when git error present.
func evidenceCIFetchDepth(label string, s Signals) ValidationEvidence {
	if s.GitError != "" {
		return ValidationEvidence{
			ID:        "ci_fetch_depth_zero",
			Label:     label,
			Status:    EvidenceFail,
			Source:    "git_signals",
			Rationale: "Git error detected: " + s.GitError,
		}
	}
	return ValidationEvidence{
		ID:        "ci_fetch_depth_zero",
		Label:     label,
		Status:    EvidencePass,
		Source:    "git_signals",
		Rationale: "No git error: diff range was computed successfully.",
	}
}

// evidencePRReviewSummary evaluates PR description quality via intent strength.
func evidencePRReviewSummary(label string, ci *riskcontext.ContextInsights) ValidationEvidence {
	id := "pr_review_summary"
	if ci == nil {
		return ValidationEvidence{ID: id, Label: label, Status: EvidenceUnknown, Source: "intent",
			Rationale: "Context insights unavailable."}
	}
	switch ci.Intent.IntentStrength {
	case "strong":
		return ValidationEvidence{ID: id, Label: label, Status: EvidencePass, Source: "intent",
			Rationale: "PR title/body has strong keywords aligned with the diff."}
	case "weak":
		msg := "PR description does not adequately scope the change."
		if ci.Intent.Title == "" {
			msg = "PR description is absent or too short to qualify as an evidence-backed review plan."
		}
		return ValidationEvidence{ID: id, Label: label, Status: EvidenceMissing, Source: "intent",
			Rationale: msg}
	}
	return ValidationEvidence{ID: id, Label: label, Status: EvidenceUnknown, Source: "intent",
		Rationale: "Cannot determine PR description quality from available signals."}
}

// evidenceWorkflowConfig evaluates workflow/config validation via commit validation note.
func evidenceWorkflowConfig(label string, s Signals) ValidationEvidence {
	id := "workflow_config_validation"
	if s.ValidationNoteFound {
		return ValidationEvidence{ID: id, Label: label, Status: EvidencePass, Source: "git_signals",
			Rationale: "Validation note found in commit: " + noteSnippet(s.ValidationNoteSnippet)}
	}
	return ValidationEvidence{ID: id, Label: label, Status: EvidenceUnknown, Source: "git_signals",
		Rationale: "No validation note in commit; CI check result not available from repo-local signals."}
}

// evidenceTestDomain evaluates E2E/unit test coverage for a sensitive domain.
// PASS on E2E hits; UNKNOWN on unit-only (E2E not confirmed); MISSING when no hits at all.
func evidenceTestDomain(id, label, domain string, s Signals) ValidationEvidence {
	if s.TestE2EDomainHits != nil && s.TestE2EDomainHits[domain] > 0 {
		return ValidationEvidence{ID: id, Label: label, Status: EvidencePass, Source: "test_domain_hits",
			Rationale: fmt.Sprintf("E2E test files touching %q domain in this diff (%d file(s)).", domain, s.TestE2EDomainHits[domain])}
	}
	if s.TestUnitDomainHits != nil && s.TestUnitDomainHits[domain] > 0 {
		return ValidationEvidence{ID: id, Label: label, Status: EvidenceUnknown, Source: "test_domain_hits",
			Rationale: fmt.Sprintf("Unit test files touching %q domain in diff (%d file(s)); E2E coverage not confirmed.", domain, s.TestUnitDomainHits[domain])}
	}
	return ValidationEvidence{ID: id, Label: label, Status: EvidenceMissing, Source: "test_domain_hits",
		Rationale: fmt.Sprintf("No test domain hits for %q in this diff.", domain)}
}

// evidenceMigrationsGate evaluates migration validation via E2E hits or validation note.
func evidenceMigrationsGate(label string, s Signals) ValidationEvidence {
	id := "migrations_validation_gate"
	if s.TestE2EDomainHits != nil && s.TestE2EDomainHits[DomainMigrations] > 0 {
		return ValidationEvidence{ID: id, Label: label, Status: EvidencePass, Source: "test_domain_hits",
			Rationale: "E2E test coverage for migrations domain detected in diff."}
	}
	if s.ValidationNoteFound && s.MigrationFiles > 0 {
		return ValidationEvidence{ID: id, Label: label, Status: EvidencePass, Source: "git_signals",
			Rationale: "Validation note present alongside migration file changes: " + noteSnippet(s.ValidationNoteSnippet)}
	}
	if s.MigrationFiles > 0 {
		return ValidationEvidence{ID: id, Label: label, Status: EvidenceMissing, Source: "git_signals",
			Rationale: fmt.Sprintf("%d migration file(s) changed; no validation note or E2E coverage detected.", s.MigrationFiles)}
	}
	return ValidationEvidence{ID: id, Label: label, Status: EvidenceUnknown, Source: "git_signals",
		Rationale: "No migration files detected; validation state unknown."}
}

// evidenceAddTests evaluates test presence via behavioral coverage and test LOC ratio.
func evidenceAddTests(label string, s Signals, ci *riskcontext.ContextInsights) ValidationEvidence {
	id := "add_tests_or_evidence"
	if ci != nil && ci.Proximity.BehavioralCoverage == "adequate" {
		return ValidationEvidence{ID: id, Label: label, Status: EvidencePass, Source: "proximity",
			Rationale: "Behavioral coverage is adequate: E2E or non-sensitive domain with co-located tests."}
	}
	if s.TestLOCRatio >= 0.30 && s.TestFiles > 0 {
		return ValidationEvidence{ID: id, Label: label, Status: EvidencePass, Source: "test_loc_ratio",
			Rationale: fmt.Sprintf("Test LOC ratio is %.0f%% (≥30%%) with %d test file(s) in diff.", s.TestLOCRatio*100, s.TestFiles)}
	}
	if s.TestFiles == 0 {
		return ValidationEvidence{ID: id, Label: label, Status: EvidenceMissing, Source: "test_loc_ratio",
			Rationale: "No test files in this diff."}
	}
	if ci != nil && ci.Proximity.BehavioralCoverage == "shallow" {
		return ValidationEvidence{ID: id, Label: label, Status: EvidenceUnknown, Source: "proximity",
			Rationale: "Behavioral coverage is shallow: unit tests present but E2E coverage for sensitive domains not confirmed."}
	}
	return ValidationEvidence{ID: id, Label: label, Status: EvidenceUnknown, Source: "test_loc_ratio",
		Rationale: fmt.Sprintf("Test files present (%d) but coverage depth unclear.", s.TestFiles)}
}

// evidenceIntentAlignment evaluates PR description ↔ diff domain alignment.
func evidenceIntentAlignment(label string, ci *riskcontext.ContextInsights) ValidationEvidence {
	id := "context_align_pr_description"
	if ci == nil {
		return ValidationEvidence{ID: id, Label: label, Status: EvidenceUnknown, Source: "intent",
			Rationale: "Context insights unavailable."}
	}
	if ci.Intent.Mismatch {
		return ValidationEvidence{ID: id, Label: label, Status: EvidenceFail, Source: "intent",
			Rationale: "PR title/body keywords imply domains not present in diff: " + ci.Intent.Detail}
	}
	if ci.Intent.Aligned {
		return ValidationEvidence{ID: id, Label: label, Status: EvidencePass, Source: "intent",
			Rationale: "PR description keywords are aligned with diff domains."}
	}
	return ValidationEvidence{ID: id, Label: label, Status: EvidenceUnknown, Source: "intent",
		Rationale: "Intent alignment could not be determined (no strong keywords matched)."}
}

// evidenceScatteredReviewPlan evaluates whether a PR description covers a scattered change.
func evidenceScatteredReviewPlan(label string, ci *riskcontext.ContextInsights) ValidationEvidence {
	id := "context_scattered_review_plan"
	if ci == nil {
		return ValidationEvidence{ID: id, Label: label, Status: EvidenceUnknown, Source: "intent",
			Rationale: "Context insights unavailable."}
	}
	if ci.Intent.IntentStrength == "strong" && ci.Intent.Aligned {
		return ValidationEvidence{ID: id, Label: label, Status: EvidencePass, Source: "intent",
			Rationale: "Strong PR description present and aligned with scattered change domains."}
	}
	if ci.Intent.IntentStrength == "weak" {
		return ValidationEvidence{ID: id, Label: label, Status: EvidenceMissing, Source: "intent",
			Rationale: "PR description is weak or generic for a scattered multi-area change."}
	}
	if ci.Intent.IntentStrength == "unknown" {
		return ValidationEvidence{ID: id, Label: label, Status: EvidenceUnknown, Source: "intent",
			Rationale: "No domain-specific keywords matched; cannot confirm review plan covers all areas."}
	}
	return ValidationEvidence{ID: id, Label: label, Status: EvidenceUnknown, Source: "intent",
		Rationale: "Cannot determine whether review plan covers the scattered change."}
}

// evidenceTestProximity evaluates test co-location via proximity/behavioral coverage.
func evidenceTestProximity(label string, ci *riskcontext.ContextInsights) ValidationEvidence {
	id := "context_improve_test_proximity"
	if ci == nil {
		return ValidationEvidence{ID: id, Label: label, Status: EvidenceUnknown, Source: "proximity",
			Rationale: "Context insights unavailable."}
	}
	if ci.Proximity.BehavioralCoverage == "adequate" {
		return ValidationEvidence{ID: id, Label: label, Status: EvidencePass, Source: "proximity",
			Rationale: "Tests are co-located or behaviorally adequate for changed code."}
	}
	if ci.Proximity.Mode == "distant" && ci.Proximity.BehavioralCoverage == "unknown" {
		return ValidationEvidence{ID: id, Label: label, Status: EvidenceMissing, Source: "proximity",
			Rationale: fmt.Sprintf("Structural alignment is %q with no test coverage evidence for this diff.", ci.Proximity.Mode)}
	}
	return ValidationEvidence{ID: id, Label: label, Status: EvidenceUnknown, Source: "proximity",
		Rationale: fmt.Sprintf("Structural alignment is %q; behavioral coverage is %q.", ci.Proximity.Mode, ci.Proximity.BehavioralCoverage)}
}

// evidenceHotspotRegression evaluates targeted regression evidence via validation note.
func evidenceHotspotRegression(label string, s Signals) ValidationEvidence {
	id := "context_hotspot_regression_focus"
	if s.ValidationNoteFound {
		return ValidationEvidence{ID: id, Label: label, Status: EvidencePass, Source: "git_signals",
			Rationale: "Validation note present in commit: " + noteSnippet(s.ValidationNoteSnippet)}
	}
	return ValidationEvidence{ID: id, Label: label, Status: EvidenceUnknown, Source: "git_signals",
		Rationale: "No validation note detected; targeted regression coverage cannot be confirmed from diff alone."}
}

// noteSnippet returns a short display form of a validation note snippet.
func noteSnippet(s string) string {
	if s == "" {
		return "(present)"
	}
	if len(s) > 80 {
		return s[:80] + "…"
	}
	return s
}

// evidenceAwareUpgrade returns an upgraded merge recommendation when evidence confirms
// a HIGH-priority action has a fail or missing status. The recommendation is only
// ever upgraded (never downgraded). MEDIUM/SUPPORTING evidence issues are surfaced
// in blocking reasons but do not affect the recommendation.
func evidenceAwareUpgrade(rec string, evidence []ValidationEvidence, actions []RequiredAction) string {
	highIDs := make(map[string]bool)
	for _, a := range actions {
		prio := a.Priority
		if prio == "" {
			prio = priorityForActionID(a.ID)
		}
		if prio == PriorityHigh {
			highIDs[a.ID] = true
		}
	}

	for _, ev := range evidence {
		if !highIDs[ev.ID] {
			continue
		}
		switch ev.Status {
		case EvidenceFail:
			if rec == "pass" || rec == "warn" {
				return "block"
			}
		case EvidenceMissing:
			if rec == "pass" {
				return "warn"
			}
		}
	}
	return rec
}

// evidenceBlockingReasons builds the subset of evidence entries that should appear
// in BlockingReasons: FAIL for any priority, MISSING for HIGH-priority only.
func evidenceBlockingReasons(evidence []ValidationEvidence, actions []RequiredAction) []string {
	highIDs := make(map[string]bool)
	for _, a := range actions {
		prio := a.Priority
		if prio == "" {
			prio = priorityForActionID(a.ID)
		}
		if prio == PriorityHigh {
			highIDs[a.ID] = true
		}
	}

	var out []string
	for _, ev := range evidence {
		switch ev.Status {
		case EvidenceFail:
			out = append(out, fmt.Sprintf("Evidence FAIL [%s]: %s", ev.ID, ev.Rationale))
		case EvidenceMissing:
			if highIDs[ev.ID] {
				out = append(out, fmt.Sprintf("Evidence MISSING [%s]: %s", ev.ID, ev.Rationale))
			}
		}
	}
	return out
}

// evidenceStatusIcon returns a markdown icon for a ValidationStatus.
func evidenceStatusIcon(status ValidationStatus) string {
	switch status {
	case EvidencePass:
		return "✅"
	case EvidenceMissing:
		return "⚠️"
	case EvidenceFail:
		return "❌"
	default:
		return "❓"
	}
}
