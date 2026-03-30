package prrisk

import (
	"fmt"
	"strings"
)

// ComputeEnforcement derives merge recommendation, validations, review routing, and evidence status
// from a full Result. Evidence status (v2.6+) is computed from repo-local signals only.
func ComputeEnforcement(r Result) Enforcement {
	validations := ComputeRequiredValidations(r.Signals, r.RequiredActions)
	hints := ComputeRoutingHints(r.Signals, r.Factors, r.ContextInsights)

	// Compute evidence before finalising the recommendation so the upgrade can fire.
	evidenceStatus, evidenceSummary := ComputeEvidenceStatus(r)

	rec := mergeRecommendation(r)
	rec = evidenceAwareUpgrade(rec, evidenceStatus, r.RequiredActions)

	strategy := reviewStrategyFor(rec, r.RiskBand)
	reqs := reviewRequirements(rec, r.RiskBand, r.RiskScore)
	blocking := computeBlockingReasons(r, rec)
	if rec != "pass" {
		blocking = dedupeStrings(append(blocking, evidenceBlockingReasons(evidenceStatus, r.RequiredActions)...))
	} else {
		blocking = dedupeStrings(blocking)
	}
	reasons := computePolicyReasons(r, rec)

	return Enforcement{
		MergeRecommendation: rec,
		Rationale:           mergeRationale(r, rec),
		RecommendedReview: RecommendedReview{
			Strategy:     strategy,
			RoutingHints: hints,
		},
		RequiredValidations: validations,
		ReviewRequirements:  reqs,
		BlockingReasons:     blocking,
		Reasons:             reasons,
		EvidenceStatus:      evidenceStatus,
		EvidenceSummary:     evidenceSummary,
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
			"PR risk is low (score %.0f/100). Normal prerequisites — CI checks, required reviews, and any targeted testing — still apply before merging.",
			r.RiskScore,
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
		add("Prefer a reviewer familiar with the touched subsystems (see recommended_review.routing_hints).")
	}
	if score >= 45 && rec != "pass" {
		add("Confirm CI is green for all required checks tied to this branch.")
	}
	return out
}

func computeBlockingReasons(r Result, rec string) []string {
	var out []string
	if r.Signals.GitError != "" {
		out = append(out, "Git diff unavailable — change scope cannot be verified from history.")
	}
	switch rec {
	case "block":
		if r.Signals.GitError == "" {
			out = append(out, fmt.Sprintf("Merge-block policy: risk band %q (score %.0f) requires completing high-priority actions before merge.", r.RiskBand, r.RiskScore))
		}
	case "warn":
		out = append(out, fmt.Sprintf("Elevated review: risk band %q — complete listed validations before merge.", r.RiskBand))
	}
	if r.ScoreMath.FloorApplied && len(r.ScoreMath.FloorReasons) > 0 {
		out = append(out, "Risk floor applied so trust-critical changes are not masked by reducers.")
	}
	return dedupeStrings(out)
}

func computePolicyReasons(r Result, rec string) []string {
	var out []string
	out = append(out, "Deterministic policy: merge recommendation derives from risk band, git availability, and tests_missing in low band.")
	if rec == "block" || rec == "warn" {
		out = append(out, "Required validations are distinct from mitigations: validations are merge gates; mitigations are factor-specific guidance.")
	}
	if len(r.RequiredActions) > 0 {
		out = append(out, "Required actions prioritized as high / medium / supporting by action ID and risk class.")
	}
	if r.ContextInsights != nil && r.ContextInsights.Intent.IntentStrength == "weak" {
		out = append(out, "PR title/body treated as weak intent — alignment scoring was limited.")
	}
	return dedupeStrings(out)
}

func dedupeStrings(ss []string) []string {
	seen := make(map[string]struct{})
	var out []string
	for _, s := range ss {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}
