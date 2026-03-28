package prrisk

import (
	"fmt"
	"strings"
)

// ComputeEnforcement derives merge recommendation, validations, and review routing from a full Result.
func ComputeEnforcement(r Result) Enforcement {
	validations := ComputeRequiredValidations(r.Signals, r.RequiredActions)
	hints := ComputeRoutingHints(r.Signals, r.Factors, r.ContextInsights)
	rec := mergeRecommendation(r)
	strategy := reviewStrategyFor(rec, r.RiskBand)
	reqs := reviewRequirements(rec, r.RiskBand, r.RiskScore)

	return Enforcement{
		MergeRecommendation: rec,
		Rationale:           mergeRationale(r, rec),
		ReviewStrategy:      strategy,
		RequiredValidations: validations,
		ReviewRequirements:  reqs,
		RoutingHints:        hints,
	}
}

func mergeRecommendation(r Result) string {
	if r.Signals.GitError != "" {
		return "block"
	}
	switch r.RiskBand {
	case "critical":
		return "block"
	case "high":
		return "block"
	case "medium":
		return "warn"
	case "low":
		if okBool(factorIDs(r.Factors), "tests_missing") {
			return "warn"
		}
		return "pass"
	default:
		return "warn"
	}
}

func mergeRationale(r Result, rec string) string {
	if r.Signals.GitError != "" {
		return "Git diff could not be computed; merge risk is unknown until CI checkout/history is fixed."
	}
	switch rec {
	case "block":
		return fmt.Sprintf(
			"Risk band is %s (score %.0f/100); treat as merge-blocked until required actions and validations are satisfied.",
			r.RiskBand, r.RiskScore,
		)
	case "warn":
		return fmt.Sprintf(
			"Risk band is %s (score %.0f/100); merge only after completing checklist items and review.",
			r.RiskBand, r.RiskScore,
		)
	default:
		return fmt.Sprintf(
			"Risk band is %s (score %.0f/100); standard review is sufficient from a PR-risk perspective.",
			r.RiskBand, r.RiskScore,
		)
	}
}

func reviewStrategyFor(rec, band string) string {
	if rec == "block" {
		return "Do not merge until required validations pass and reviewers confirm mitigation of listed risks. Re-run prrisk after substantive changes."
	}
	if rec == "warn" {
		if band == "medium" {
			return "Use a checklist-driven review: walk factors and required actions, then approve when evidence matches."
		}
		return "Complete the required actions and validations below, then proceed with normal approval."
	}
	return "Single-pass review is enough; spot-check touched paths if helpful."
}

func reviewRequirements(rec, band string, score float64) []string {
	seen := make(map[string]struct{})
	var out []string
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		if _, ok := seen[s]; ok {
			return
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}

	add("At least one approving review on the changed code.")
	if rec == "block" {
		add("Explicit sign-off that required actions and validations are complete before merge.")
	}
	if band == "high" || band == "critical" {
		add("Prefer a reviewer familiar with the touched subsystems (see routing hints).")
	}
	if score >= 45 && rec != "pass" {
		add("Confirm CI is green for all required checks tied to this branch.")
	}
	return out
}
