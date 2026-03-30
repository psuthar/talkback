package prrisk

import (
	"regexp"
	"strings"
)

// polishRequiredValidationLine rewrites raw validation lines (often taxonomy-prefixed)
// into concise reviewer-facing wording for the short PR comment only.
// Full report (pr_risk.md) keeps the canonical structured lines.
func polishRequiredValidationLine(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return s
	}
	if mapped, ok := validationCommentPolish[s]; ok {
		return mapped
	}
	// Fallback: drop a single leading lowercase token + ": " (same idea as Python generic prefix strip).
	re := regexp.MustCompile(`^([a-z]+):\s+(.+)$`)
	if m := re.FindStringSubmatch(s); len(m) == 3 {
		rest := strings.TrimSpace(m[2])
		if rest != "" {
			return strings.ToUpper(rest[:1]) + rest[1:]
		}
	}
	return s
}

// validationCommentPolish maps exact engine emissions to PR-comment-friendly lines.
// Keys must match strings produced by validationForAction / ComputeRequiredValidations.
var validationCommentPolish = map[string]string{
	"ci: required status checks must pass before merge":        "Required status checks must pass before merge",
	"ci: restore reliable git diff before merge (see git error in report)": "Restore reliable git diff before merge (see report for git error)",
	"ci: full git history available for diff (fetch-depth: 0 or equivalent)": "Full git history available for this diff (equivalent to fetch-depth: 0)",
	"config: workflow / deploy / go.mod changes validated against required checks": "Validate workflow, deploy, or go.mod changes against required checks",
	"process: PR description with scoped, evidence-backed review plan": "Ensure PR includes a clear, scoped review plan",
	"process: validation note present in commit — confirm it matches what was run": "Confirm the validation note in the commit matches what was run",
	"process: PR title/body aligned with actual diff (intent match)": "Align PR title and body with the actual diff (intent match)",
	"process: structured review map for scattered multi-area change": "Use a structured review map for scattered multi-area changes",
	"test: auth/session/invite flows exercised (E2E or equivalent evidence)": "Exercise auth/session/invite flows (E2E or equivalent evidence)",
	"test: Q&A with citations validated for RAG-affecting changes": "Validate Q&A with citations when changing RAG-related code",
	"test: materials upload + processing smoke for pipeline changes":   "Run materials upload and processing smoke for pipeline changes",
	"test: tests or recorded evidence for sensitive paths":             "Add tests or recorded evidence for sensitive paths",
	"test: tests co-located or explicitly linked for changed code":     "Ensure test coverage is present or clearly linked for changed code",
	"test: targeted regression for high-churn area touched by diff":    "Run targeted regression for high-churn areas touched by this diff",
	"db: migrations validated with rollback/reversal plan documented": "Validate migrations with rollback/reversal plan documented",
}
