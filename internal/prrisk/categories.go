package prrisk

const (
	CategoryCode           = "code"
	CategoryWorkflow       = "workflow"
	CategoryTestConfidence = "test_confidence"
)

func laneForFactorID(factorID string) string {
	switch factorID {
	case
		"git_unavailable",
		"diff_very_large",
		"diff_large",
		"many_files",
		"domain_auth",
		"domain_migrations",
		"domain_rag",
		"domain_processing",
		"web_large":
		return CategoryCode
	case
		"ci_workflows",
		"deploy_config",
		"go_mod_deps":
		return CategoryWorkflow
	case
		"tests_missing":
		return CategoryTestConfidence
	default:
		// If we add new factor IDs and forget this mapping, treat as code.
		return CategoryCode
	}
}

func testConfidenceScore(s Signals) float64 {
	// Sensitive domains in this diff are where missing tests matter.
	sensitiveChanged := s.DomainHits[DomainAuth] > 0 ||
		s.DomainHits[DomainRAG] > 0 ||
		s.DomainHits[DomainProcessing] > 0 ||
		s.DomainHits[DomainMigrations] > 0

	if !sensitiveChanged {
		// Changes are likely low-risk for correctness; "confidence" is high.
		return 85
	}

	// Prefer E2E evidence.
	if s.E2ETestFiles > 0 {
		return 80
	}
	if s.UnitTestFiles > 0 || s.TestFiles > 0 {
		return 60
	}
	// No tests in diff for sensitive areas.
	return 25
}

// ComputeCategories builds the decision-grade risk category breakdown.
func ComputeCategories(s Signals, factors []RiskFactor, reducers []RiskReducer) []RiskCategory {
	baseByLane := map[string]float64{}
	factorsByLane := map[string][]string{}
	for _, f := range factors {
		lane := laneForFactorID(f.ID)
		baseByLane[lane] += f.Points
		factorsByLane[lane] = append(factorsByLane[lane], f.ID)
	}

	reducerByLane := map[string]float64{}
	reducersByLane := map[string][]string{}
	for _, r := range reducers {
		lane := r.CategoryKey
		if lane == "" {
			lane = CategoryCode
		}
		reducerByLane[lane] += r.Points
		reducersByLane[lane] = append(reducersByLane[lane], r.ID)
	}

	// Lane scores are deterministic and clamped, independent of overall RiskScore.
	codeRisk := clamp100(baseByLane[CategoryCode] - reducerByLane[CategoryCode])
	workflowRisk := clamp100(baseByLane[CategoryWorkflow] - reducerByLane[CategoryWorkflow])

	conf := testConfidenceScore(s)
	testConfidenceRisk := clamp100(100 - conf)
	if s.GitError != "" {
		// Git issues reduce confidence regardless of test evidence.
		testConfidenceRisk = clamp100(testConfidenceRisk + 10)
		conf = clamp100(conf - 10)
	}

	return []RiskCategory{
		{
			Key:       CategoryCode,
			Label:     "Code changes",
			RiskScore: codeRisk,
			Factors:   uniqueStrings(factorsByLane[CategoryCode]),
			Reducers:  uniqueStrings(reducersByLane[CategoryCode]),
		},
		{
			Key:       CategoryWorkflow,
			Label:     "Workflow / deployment changes",
			RiskScore: workflowRisk,
			Factors:   uniqueStrings(factorsByLane[CategoryWorkflow]),
			Reducers:  uniqueStrings(reducersByLane[CategoryWorkflow]),
		},
		{
			Key:        CategoryTestConfidence,
			Label:      "Test confidence",
			RiskScore:  testConfidenceRisk,
			Confidence: clamp100(conf),
			Factors:    uniqueStrings(factorsByLane[CategoryTestConfidence]),
			Reducers:   uniqueStrings(reducersByLane[CategoryTestConfidence]),
		},
	}
}

func uniqueStrings(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, s := range in {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}
