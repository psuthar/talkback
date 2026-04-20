package prrisk

import (
	"fmt"
	"strings"
)

func factorIDs(factors []RiskFactor) map[string]struct{} {
	m := make(map[string]struct{}, len(factors))
	for _, f := range factors {
		m[f.ID] = struct{}{}
	}
	return m
}

// DetectReducers returns deterministic reducers that lower risk.
// Reducers are only emitted when their corresponding risky factors are present.
func DetectReducers(s Signals, factors []RiskFactor, w ScoreWeights) []RiskReducer {
	has := factorIDs(factors)
	var reducers []RiskReducer

	// Validation note reducer — tiered by evidence quality.
	if s.ValidationNoteFound {
		if okBool(has, "ci_workflows") || okBool(has, "deploy_config") || okBool(has, "go_mod_deps") {
			strength := validationNoteStrength(s.ValidationNoteSnippet)
			var id, label string
			var pts float64
			switch strength {
			case "strong":
				id, label = "validation_note_strong", "Strong validation note (E2E/smoke evidence)"
				pts = w.ValidationNoteReducerPoints
			case "moderate":
				id, label = "validation_note_moderate", "Moderate validation note (staging/CI evidence)"
				pts = w.WorkflowPartialReducerPoints
			default:
				id, label = "validation_note_basic", "Basic validation note"
				pts = w.WorkflowPartialReducerPoints / 2
			}
			reducers = append(reducers, RiskReducer{
				ID:          id,
				Label:       label,
				Points:      pts,
				Evidence:    shortSnippet(s.ValidationNoteSnippet),
				CategoryKey: CategoryWorkflow,
			})
		}
	}

	// Domain-specific test evidence reducers (unit vs E2E).
	type domSpec struct {
		factorID string
		domain   string
		unitKey  string
		e2eKey   string
		unitPts  float64
		e2ePts   float64
	}
	// Note: domain strings here intentionally match product domain labels.
	domains := []domSpec{
		{factorID: "domain_auth", domain: DomainAuth, unitPts: w.UnitTestEvidenceReducerPoints, e2ePts: w.E2ETestEvidenceReducerPoints},
		{factorID: "domain_rag", domain: DomainRAG, unitPts: w.UnitTestEvidenceReducerPoints, e2ePts: w.E2ETestEvidenceReducerPoints},
		{factorID: "domain_processing", domain: DomainProcessing, unitPts: w.UnitTestEvidenceReducerPoints, e2ePts: w.E2ETestEvidenceReducerPoints},
		{factorID: "domain_orchestration", domain: DomainOrchestration, unitPts: w.UnitTestEvidenceReducerPoints, e2ePts: w.E2ETestEvidenceReducerPoints},
		{factorID: "domain_migrations", domain: DomainMigrations, unitPts: w.UnitTestEvidenceReducerPoints, e2ePts: w.E2ETestEvidenceReducerPoints},
	}

	for _, ds := range domains {
		if !okBool(has, ds.factorID) {
			continue
		}

		if s.TestE2EDomainHits != nil && s.TestE2EDomainHits[ds.domain] > 0 {
			reducers = append(reducers, RiskReducer{
				ID:          ds.factorID + "_e2e_evidence",
				Label:       "E2E test evidence present",
				Points:      ds.e2ePts,
				Evidence:    "Found E2E tests targeting the domain in the diff",
				CategoryKey: CategoryTestConfidence,
			})
			continue
		}

		if s.TestUnitDomainHits != nil && s.TestUnitDomainHits[ds.domain] > 0 {
			reducers = append(reducers, RiskReducer{
				ID:          ds.factorID + "_unit_evidence",
				Label:       "Unit test evidence present",
				Points:      ds.unitPts,
				Evidence:    "Found unit tests targeting the domain in the diff",
				CategoryKey: CategoryTestConfidence,
			})
		}
	}

	// Style-only frontend change: commit declares "Style-only:" and no backend domains touched.
	if s.StyleOnlyNoteFound && w.StyleOnlyReducerPoints > 0 {
		backendHit := s.DomainHits[DomainAuth] > 0 ||
			s.DomainHits[DomainAPI] > 0 ||
			s.DomainHits[DomainDatabase] > 0 ||
			s.DomainHits[DomainRAG] > 0 ||
			s.DomainHits[DomainProcessing] > 0 ||
			s.DomainHits[DomainMigrations] > 0
		if !backendHit {
			reducers = append(reducers, RiskReducer{
				ID:          "style_only_note",
				Label:       "Style-only frontend change (commit note)",
				Points:      w.StyleOnlyReducerPoints,
				Evidence:    shortSnippet(s.StyleOnlyNoteSnippet),
				CategoryKey: CategoryTestConfidence,
			})
		}
	}

	// If the diff only touches tests (no sensitive non-test code), reduce overall risk a bit.
	sensitiveInDiff := s.DomainHits[DomainAuth] > 0 ||
		s.DomainHits[DomainRAG] > 0 ||
		s.DomainHits[DomainProcessing] > 0 ||
		s.DomainHits[DomainOrchestration] > 0 ||
		s.DomainHits[DomainMigrations] > 0
	if !sensitiveInDiff && s.TestFiles > 0 && s.FileCount > 0 && s.TestFiles == s.FileCount {
		reducers = append(reducers, RiskReducer{
			ID:          "test_only_diff",
			Label:       "Test-only diff",
			Points:      w.TestOnlyDiffReducerPoints,
			Evidence:    "All changed files are classified as tests",
			CategoryKey: CategoryCode,
		})
	}

	// Test-heavy diff: meaningful test coverage in diff without being all-tests.
	if w.TestHeavyLOCRatioThreshold > 0 &&
		s.TestLOCRatio >= w.TestHeavyLOCRatioThreshold &&
		s.FileCount > 0 && s.TestFiles < s.FileCount {
		reducers = append(reducers, RiskReducer{
			ID:          "test_heavy_diff",
			Label:       "Test-heavy diff",
			Points:      w.TestHeavyReducerPoints,
			Evidence:    fmt.Sprintf("%.0f%% of LOC churn is in test files", s.TestLOCRatio*100),
			CategoryKey: CategoryTestConfidence,
		})
	}

	return reducers
}

// validationNoteStrength classifies the strength of a validation note snippet.
// Returns "strong", "moderate", or "basic".
func validationNoteStrength(snippet string) string {
	lower := strings.ToLower(snippet)
	if strings.Contains(lower, "e2e") || strings.Contains(lower, "smoke") ||
		strings.Contains(lower, "playwright") || strings.Contains(lower, "end-to-end") {
		return "strong"
	}
	if strings.Contains(lower, "staging") || strings.Contains(lower, "dispatch") ||
		strings.Contains(lower, "deploy") || strings.Contains(lower, " ci ") ||
		strings.Contains(lower, "pipeline") {
		return "moderate"
	}
	return "basic"
}

func okBool(m map[string]struct{}, key string) bool {
	_, ok := m[key]
	return ok
}

func shortSnippet(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if len(s) <= 80 {
		return s
	}
	return s[:77] + "..."
}
