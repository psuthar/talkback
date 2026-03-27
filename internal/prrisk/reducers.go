package prrisk

import "strings"

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

	// Validation note reducer (primarily intended to suppress risky config/workflow without evidence).
	if s.ValidationNoteFound {
		if _, ok := has["ci_workflows"]; ok || okBool(has, "deploy_config") || okBool(has, "go_mod_deps") {
			reducers = append(reducers, RiskReducer{
				ID:          "validation_note_present",
				Label:       "Validation note present",
				Points:      w.ValidationNoteReducerPoints,
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

	// If the diff only touches tests (no sensitive non-test code), reduce overall risk a bit.
	sensitiveInDiff := s.DomainHits[DomainAuth] > 0 ||
		s.DomainHits[DomainRAG] > 0 ||
		s.DomainHits[DomainProcessing] > 0 ||
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

	return reducers
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
