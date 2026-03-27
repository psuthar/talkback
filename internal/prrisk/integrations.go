package prrisk

import (
	"fmt"
	"strings"
)

// EnvJiraIssueKey is an optional env var for future Jira linking (documented, not required).
const EnvJiraIssueKey = "PRRISK_JIRA_ISSUE_KEY"

// BuildIntegrations fills PR comment markdown and optional Jira hook.
func BuildIntegrations(factors []RiskFactor, score float64, baseRef, jiraKey string) Integrations {
	md := &strings.Builder{}
	fmt.Fprintf(md, "## PR Risk (v%d.%d)\n\n", Version, VersionMinor)
	fmt.Fprintf(md, "**Score:** %.1f/100 (%s) vs `%s`\n\n", score, band(score), baseRef)
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
	md.WriteString("_See artifact `pr_risk_v2_1.md` for the full checklist._\n")

	md.WriteString("\n_See artifact `pr_risk_v2_1.md` for reducers, category breakdown, and full mitigations._\n")

	return Integrations{
		JiraIssueKey:      jiraKey,
		PRCommentMarkdown: md.String(),
	}
}
