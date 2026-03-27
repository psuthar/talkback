package prrisk

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

// WriteJSON writes Result as indented JSON.
func WriteJSON(path string, r Result) error {
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0644)
}

// WriteMarkdown writes a human-readable report including mitigations.
func WriteMarkdown(path string, r Result) error {
	var sb strings.Builder
	sb.WriteString("# TalkBack PR Risk Report (v2)\n\n")
	sb.WriteString(fmt.Sprintf("**Generated:** %s  \n", r.GeneratedAt.Format("2006-01-02T15:04:05Z")))
	sb.WriteString(fmt.Sprintf("**Base ref:** `%s`  \n\n", r.BaseRef))
	sb.WriteString("## Summary\n\n")
	sb.WriteString(fmt.Sprintf("| Metric | Value |\n|--------|-------|\n"))
	sb.WriteString(fmt.Sprintf("| Risk score | **%.1f** / 100 |\n", r.RiskScore))
	sb.WriteString(fmt.Sprintf("| Band | **%s** |\n", r.RiskBand))
	sb.WriteString(fmt.Sprintf("| Files changed | %d |\n", r.Signals.FileCount))
	sb.WriteString(fmt.Sprintf("| LOC churn (add+del) | %d |\n", r.Signals.TotalLOC))
	sb.WriteString(fmt.Sprintf("| Test files in diff | %d |\n", r.Signals.TestFiles))
	sb.WriteString(fmt.Sprintf("| Config-ish files (CI/deploy/mod) | %d |\n", r.Signals.ConfigFiles))
	if r.Signals.GitError != "" {
		sb.WriteString(fmt.Sprintf("| Git | **error:** %s |\n", r.Signals.GitError))
	}
	sb.WriteString("\n## Domain hits\n\n")
	if len(r.Signals.DomainHits) == 0 {
		sb.WriteString("_None._\n")
	} else {
		sb.WriteString("| Domain | Files |\n|--------|-------|\n")
		for _, k := range sortedDomainKeys(r.Signals.DomainHits) {
			sb.WriteString(fmt.Sprintf("| %s | %d |\n", k, r.Signals.DomainHits[k]))
		}
	}
	sb.WriteString("\n## Risk factors\n\n")
	if len(r.Factors) == 0 {
		sb.WriteString("_No factors triggered._\n")
	} else {
		for _, f := range r.Factors {
			sb.WriteString(fmt.Sprintf("### %s (`%s`)\n\n", f.Label, f.ID))
			sb.WriteString(fmt.Sprintf("- **Points:** %.1f\n", f.Points))
			if f.Detail != "" {
				sb.WriteString(fmt.Sprintf("- **Detail:** %s\n", f.Detail))
			}
			sb.WriteString("\n")
		}
	}
	sb.WriteString("## Mitigations\n\n")
	for _, m := range r.Mitigations {
		sb.WriteString(fmt.Sprintf("### `%s`\n\n", m.FactorID))
		for _, a := range m.Actions {
			sb.WriteString(fmt.Sprintf("- %s\n", a))
		}
		sb.WriteString("\n")
	}
	sb.WriteString("## Integrations\n\n")
	if r.Integrations.JiraIssueKey != "" {
		sb.WriteString(fmt.Sprintf("- **Jira:** %s\n", r.Integrations.JiraIssueKey))
	} else {
		sb.WriteString("- **Jira:** _(set `PRRISK_JIRA_ISSUE_KEY` for optional linkage)_\n")
	}
	sb.WriteString("\n## Suggested PR comment (markdown)\n\n")
	sb.WriteString("```markdown\n")
	sb.WriteString(r.Integrations.PRCommentMarkdown)
	sb.WriteString("\n```\n")
	return os.WriteFile(path, []byte(sb.String()), 0644)
}

func sortedDomainKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
