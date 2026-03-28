package prrisk

import (
	"fmt"
	"strings"
)

// EnvJiraIssueKey is an optional env var for future Jira linking (documented, not required).
const EnvJiraIssueKey = "PRRISK_JIRA_ISSUE_KEY"

// BuildIntegrations fills PR comment markdown and optional Jira hook.
func BuildIntegrations(factors []RiskFactor, score float64, baseRef, jiraKey string, actions []RequiredAction, math ScoreMath, enf Enforcement) Integrations {
	md := &strings.Builder{}
	fmt.Fprintf(md, "## PR Risk (%s)\n\n", ReportVersionString())
	fmt.Fprintf(md, "**Score:** %.1f/100 (%s) vs `%s`\n\n", score, band(score), baseRef)

	rec := strings.ToUpper(enf.MergeRecommendation)
	fmt.Fprintf(md, "**Merge recommendation:** **%s** — %s\n\n", rec, enf.Rationale)
	if es := enf.EvidenceSummary; es.PassCount+es.MissingCount+es.UnknownCount+es.FailCount > 0 {
		fmt.Fprintf(md, "**Evidence:** %d pass · %d missing · %d unknown · %d fail\n\n",
			es.PassCount, es.MissingCount, es.UnknownCount, es.FailCount)
	}
	if strings.TrimSpace(enf.RecommendedReview.Strategy) != "" {
		fmt.Fprintf(md, "**Review strategy:** %s\n\n", enf.RecommendedReview.Strategy)
	}
	if len(enf.BlockingReasons) > 0 {
		md.WriteString("**Blocking / elevated review reasons:**\n")
		for _, b := range enf.BlockingReasons {
			if strings.TrimSpace(b) != "" {
				fmt.Fprintf(md, "- %s\n", b)
			}
		}
		md.WriteString("\n")
	}

	if len(enf.Reasons) > 0 {
		md.WriteString("**Policy trace:**\n")
		maxR := 4
		if len(enf.Reasons) < maxR {
			maxR = len(enf.Reasons)
		}
		for i := 0; i < maxR; i++ {
			fmt.Fprintf(md, "- %s\n", enf.Reasons[i])
		}
		if len(enf.Reasons) > maxR {
			fmt.Fprintf(md, "_…and %d more in `pr_risk.md`._\n", len(enf.Reasons)-maxR)
		}
		md.WriteString("\n")
	}

	if len(enf.RequiredValidations) > 0 {
		md.WriteString("**Top required validations:**\n")
		maxV := 5
		if len(enf.RequiredValidations) < maxV {
			maxV = len(enf.RequiredValidations)
		}
		for i := 0; i < maxV; i++ {
			fmt.Fprintf(md, "%d. %s\n", i+1, enf.RequiredValidations[i])
		}
		if len(enf.RequiredValidations) > maxV {
			fmt.Fprintf(md, "_…and %d more in `pr_risk.md`._\n", len(enf.RequiredValidations)-maxV)
		}
		md.WriteString("\n")
	}

	rh := enf.RecommendedReview.RoutingHints
	if len(rh) > 0 {
		md.WriteString("**Review routing:**\n")
		maxH := 4
		if len(rh) < maxH {
			maxH = len(rh)
		}
		for i := 0; i < maxH; i++ {
			fmt.Fprintf(md, "- %s\n", rh[i])
		}
		if len(rh) > maxH {
			fmt.Fprintf(md, "_…and %d more in `pr_risk.md`._\n", len(rh)-maxH)
		}
		md.WriteString("\n")
	}

	if math.FactorsSubtotal > 0 || math.ReducersSubtotal > 0 || math.FloorMinScore > 0 {
		fmt.Fprintf(md, "**Score math:** factors **%.1f** − reducers **%.1f** → net **%.1f**",
			math.FactorsSubtotal, math.ReducersSubtotal, math.NetBeforeFloor)
		if math.FloorMinScore > 0 {
			fmt.Fprintf(md, "; floor **%.0f**", math.FloorMinScore)
			if math.FloorApplied {
				md.WriteString(" **(applied)**")
			}
		}
		fmt.Fprintf(md, " → **final %.1f** (%s)\n\n", math.FinalScore, math.FinalBand)
	}

	if jiraKey != "" {
		fmt.Fprintf(md, "**Tracked issue:** %s\n\n", jiraKey)
	}
	if len(factors) == 0 {
		md.WriteString("_No specific risk factors matched._\n")
	} else {
		md.WriteString("**Risk factors:**\n")
		for _, f := range factors {
			fmt.Fprintf(md, "- **%s** (%.0f): %s\n", f.Label, f.Points, f.Detail)
		}
	}

	md.WriteString("\n**Required actions before merge:**\n")
	n := len(actions)
	if n == 0 {
		md.WriteString("_None._\n")
	} else {
		maxShow := 5
		if n < maxShow {
			maxShow = n
		}
		for i := 0; i < maxShow; i++ {
			a := actions[i]
			prio := a.Priority
			if prio == "" {
				prio = priorityForActionID(a.ID)
			}
			fmt.Fprintf(md, "%d. **[ %s ]** **%s**", i+1, prio, a.Title)
			if len(a.Checklist) > 0 {
				fmt.Fprintf(md, " — %s", a.Checklist[0])
			}
			md.WriteString("\n")
		}
		if n > maxShow {
			fmt.Fprintf(md, "_…and %d more (see artifact `pr_risk.md`)._\n", n-maxShow)
		} else {
			md.WriteString("\n_Full checklist in artifact `pr_risk.md`._\n")
		}
	}

	md.WriteString("\n_See artifact `pr_risk.md` for reducers, category breakdown, and mitigations._\n")

	return Integrations{
		JiraIssueKey:      jiraKey,
		PRCommentMarkdown: md.String(),
	}
}
