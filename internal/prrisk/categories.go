package prrisk

import (
	"strings"

	riskcontext "github.com/psuthar/talkback/internal/prrisk/context"
)

const (
	CategoryCode           = "code"
	CategoryWorkflow       = "workflow"
	CategoryTestConfidence = "test_confidence"
)

func laneForFactorID(factorID string) string {
	if strings.HasPrefix(factorID, "context_") {
		return CategoryCode
	}
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

func testConfidenceScore(s Signals, ci *riskcontext.ContextInsights) (float64, *ConfidenceBreakdown) {
	const base = 50.0
	bd := &ConfidenceBreakdown{BaseScore: base}
	score := base

	sensitiveChanged := s.DomainHits[DomainAuth] > 0 ||
		s.DomainHits[DomainRAG] > 0 ||
		s.DomainHits[DomainProcessing] > 0 ||
		s.DomainHits[DomainMigrations] > 0

	if !sensitiveChanged {
		const delta = 35.0
		bd.Adjustments = append(bd.Adjustments, ConfidenceAdjustment{
			Reason: "No sensitive domains changed",
			Delta:  delta,
		})
		score += delta // 85
	} else {
		const sensitiveAdj = -10.0
		bd.Adjustments = append(bd.Adjustments, ConfidenceAdjustment{
			Reason: "Sensitive domains changed",
			Delta:  sensitiveAdj,
		})
		score += sensitiveAdj // 40

		if s.E2ETestFiles > 0 {
			const e2eAdj = 40.0
			bd.Adjustments = append(bd.Adjustments, ConfidenceAdjustment{
				Reason: "E2E tests present in diff",
				Delta:  e2eAdj,
			})
			score += e2eAdj // 80
		} else if s.UnitTestFiles > 0 || s.TestFiles > 0 {
			const unitAdj = 20.0
			bd.Adjustments = append(bd.Adjustments, ConfidenceAdjustment{
				Reason: "Unit tests present in diff",
				Delta:  unitAdj,
			})
			score += unitAdj // 60
		} else {
			const noTestAdj = -15.0
			bd.Adjustments = append(bd.Adjustments, ConfidenceAdjustment{
				Reason: "No tests for sensitive domain changes",
				Delta:  noTestAdj,
			})
			score += noTestAdj // 25
		}
	}

	// Proximity-based confidence penalties: apply only when testable code exists in diff
	// (mode != "" and mode != "n_a") and tests are present (so we're not double-penalising
	// the "no test files" branch above).
	if s.TestFiles > 0 && ci != nil && ci.Proximity.Mode != "" && ci.Proximity.Mode != "n_a" {
		switch ci.Proximity.StructuralAlignment {
		case "distant":
			const distantAdj = -15.0
			bd.Adjustments = append(bd.Adjustments, ConfidenceAdjustment{
				Reason: "Tests structurally distant from changed code",
				Delta:  distantAdj,
			})
			score += distantAdj
		case "partial":
			const partialAdj = -8.0
			bd.Adjustments = append(bd.Adjustments, ConfidenceAdjustment{
				Reason: "Tests only partially aligned with changed code",
				Delta:  partialAdj,
			})
			score += partialAdj
		}
		// Behavioral coverage "unknown": -5 base penalty for any diff with tests,
		// plus -5 extra when sensitive domains changed (total -10 for sensitive).
		// proximity.go uses a broader sensitive definition (includes api, database) than
		// testConfidenceScore (auth, rag, processing, migrations). The extra penalty is
		// gated on sensitiveChanged to stay consistent with the rest of the confidence logic.
		if ci.Proximity.BehavioralCoverage == "unknown" {
			const unknownCovBase = -5.0
			bd.Adjustments = append(bd.Adjustments, ConfidenceAdjustment{
				Reason: "Behavioral coverage depth unknown",
				Delta:  unknownCovBase,
			})
			score += unknownCovBase
			if sensitiveChanged {
				const unknownCovSensitive = -5.0
				bd.Adjustments = append(bd.Adjustments, ConfidenceAdjustment{
					Reason: "Behavioral coverage depth unknown for sensitive domain changes",
					Delta:  unknownCovSensitive,
				})
				score += unknownCovSensitive
			}
		}
	}

	bd.FinalScore = clamp100(score)
	return clamp100(score), bd
}

// ComputeCategories builds the decision-grade risk category breakdown.
func ComputeCategories(s Signals, factors []RiskFactor, reducers []RiskReducer, ci *riskcontext.ContextInsights) []RiskCategory {
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

	conf, bd := testConfidenceScore(s, ci)
	testConfidenceRisk := clamp100(100 - conf)
	if s.GitError != "" {
		// Git issues reduce confidence regardless of test evidence.
		const gitAdj = -10.0
		bd.Adjustments = append(bd.Adjustments, ConfidenceAdjustment{
			Reason: "Git error reduces confidence",
			Delta:  gitAdj,
		})
		testConfidenceRisk = clamp100(testConfidenceRisk + 10)
		conf = clamp100(conf + gitAdj)
		bd.FinalScore = conf
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
			Breakdown:  bd,
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
