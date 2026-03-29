package prrisk

import (
	"fmt"
	"strings"
)

// EnvJiraIssueKey is an optional env var for future Jira linking (documented, not required).
const EnvJiraIssueKey = "PRRISK_JIRA_ISSUE_KEY"

// BuildIntegrations fills PR comment markdown and optional Jira hook.
// The PR comment is intentionally short and decision-focused: merge recommendation,
// evidence summary, top 2 risk drivers, top 2 required validations, top 2 routing hints.
// Full analysis lives in the pr_risk.md artifact.
func BuildIntegrations(factors []RiskFactor, score float64, baseRef, jiraKey string, actions []RequiredAction, math ScoreMath, enf Enforcement) Integrations {
	md := &strings.Builder{}
	fmt.Fprintf(md, "## PR Risk (%s)\n\n", ReportVersionString())
	fmt.Fprintf(md, "**Score:** %.1f/100 (%s) · base `%s`\n\n", score, band(score), baseRef)

	rec := strings.ToUpper(enf.MergeRecommendation)
	fmt.Fprintf(md, "**Merge recommendation:** **%s**", rec)
	if enf.Rationale != "" {
		fmt.Fprintf(md, " — %s", enf.Rationale)
	}
	md.WriteString("\n\n")

	if es := enf.EvidenceSummary; es.PassCount+es.MissingCount+es.FailCount+es.NotEvaluatedCount > 0 {
		fmt.Fprintf(md, "**Evidence:** %d pass · %d missing · %d fail · %d not evaluated\n\n",
			es.PassCount, es.MissingCount, es.FailCount, es.NotEvaluatedCount)
	}

	// Top 2 risk drivers
	if len(factors) > 0 {
		md.WriteString("**Top risk drivers:**\n")
		maxF := 2
		if len(factors) < maxF {
			maxF = len(factors)
		}
		for i := 0; i < maxF; i++ {
			f := factors[i]
			if f.Detail != "" {
				fmt.Fprintf(md, "- %s (%.0f pts): %s\n", f.Label, f.Points, f.Detail)
			} else {
				fmt.Fprintf(md, "- %s (%.0f pts)\n", f.Label, f.Points)
			}
		}
		if len(factors) > maxF {
			fmt.Fprintf(md, "_…and %d more in `pr_risk.md`_\n", len(factors)-maxF)
		}
		md.WriteString("\n")
	}

	// Top 2 required validations
	if len(enf.RequiredValidations) > 0 {
		md.WriteString("**Top required validations:**\n")
		maxV := 2
		if len(enf.RequiredValidations) < maxV {
			maxV = len(enf.RequiredValidations)
		}
		for i := 0; i < maxV; i++ {
			fmt.Fprintf(md, "%d. %s\n", i+1, enf.RequiredValidations[i])
		}
		if len(enf.RequiredValidations) > maxV {
			fmt.Fprintf(md, "_…and %d more in `pr_risk.md`_\n", len(enf.RequiredValidations)-maxV)
		}
		md.WriteString("\n")
	}

	// Top 2 routing hints
	rh := enf.RecommendedReview.RoutingHints
	if len(rh) > 0 {
		md.WriteString("**Review routing:**\n")
		maxH := 2
		if len(rh) < maxH {
			maxH = len(rh)
		}
		for i := 0; i < maxH; i++ {
			fmt.Fprintf(md, "- %s\n", rh[i])
		}
		if len(rh) > maxH {
			fmt.Fprintf(md, "_…and %d more in `pr_risk.md`_\n", len(rh)-maxH)
		}
		md.WriteString("\n")
	}

	// Condensed score math
	if math.FactorsSubtotal > 0 || math.ReducersSubtotal > 0 || math.FloorMinScore > 0 {
		fmt.Fprintf(md, "**Score math:** factors %.1f − reducers %.1f → %.1f",
			math.FactorsSubtotal, math.ReducersSubtotal, math.FinalScore)
		if math.FloorApplied {
			fmt.Fprintf(md, " (floor %.0f applied)", math.FloorMinScore)
		}
		fmt.Fprintf(md, " · %s\n\n", math.FinalBand)
	}

	if jiraKey != "" {
		fmt.Fprintf(md, "**Tracked:** %s\n\n", jiraKey)
	}

	md.WriteString("_Full checklist and analysis in artifact `pr_risk.md`._\n")

	return Integrations{
		JiraIssueKey:      jiraKey,
		PRCommentMarkdown: md.String(),
	}
}
