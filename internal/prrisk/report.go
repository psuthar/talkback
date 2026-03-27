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
	sb.WriteString(fmt.Sprintf("# TalkBack PR Risk Report (v%d.%d)\n\n", r.Version, r.VersionMinor))
	sb.WriteString(fmt.Sprintf("**Generated:** %s  \n", r.GeneratedAt.Format("2006-01-02T15:04:05Z")))
	sb.WriteString(fmt.Sprintf("**Base ref:** `%s`  \n\n", r.BaseRef))
	if r.Interpretation != "" {
		sb.WriteString(fmt.Sprintf("> %s\n\n", r.Interpretation))
	}
	sb.WriteString("## Summary\n\n")
	sb.WriteString(fmt.Sprintf("| Metric | Value |\n|--------|-------|\n"))
	sb.WriteString(fmt.Sprintf("| Risk score | **%.1f** / 100 |\n", r.RiskScore))
	sb.WriteString(fmt.Sprintf("| Band | **%s** |\n", r.RiskBand))
	for _, c := range r.Categories {
		if c.Key == CategoryTestConfidence {
			sb.WriteString(fmt.Sprintf("| Test confidence | **%.0f** / 100 |\n", c.Confidence))
			break
		}
	}
	sb.WriteString(fmt.Sprintf("| Files changed | %d |\n", r.Signals.FileCount))
	sb.WriteString(fmt.Sprintf("| LOC churn (add+del) | %d |\n", r.Signals.TotalLOC))
	sb.WriteString(fmt.Sprintf("| Test files in diff | %d |\n", r.Signals.TestFiles))
	sb.WriteString(fmt.Sprintf("| Config-ish files (CI/deploy/mod) | %d |\n", r.Signals.ConfigFiles))
	if r.Signals.GitError != "" {
		sb.WriteString(fmt.Sprintf("| Git | **error:** %s |\n", r.Signals.GitError))
	}
	if r.Signals.ValidationNoteFound {
		sn := strings.ReplaceAll(r.Signals.ValidationNoteSnippet, "\n", " ")
		sb.WriteString(fmt.Sprintf("| Validation note | yes (%s) |\n", sn))
	}
	sb.WriteString("\n## Score math\n\n")
	sm := r.ScoreMath
	sb.WriteString("| Step | Value |\n|------|------:|\n")
	sb.WriteString(fmt.Sprintf("| Factors subtotal (sum of factor points) | **%.1f** |\n", sm.FactorsSubtotal))
	sb.WriteString(fmt.Sprintf("| Reducers subtotal (points subtracted) | **%.1f** |\n", sm.ReducersSubtotal))
	sb.WriteString(fmt.Sprintf("| Net before floor | **%.1f** |\n", sm.NetBeforeFloor))
	if sm.FloorMinScore > 0 {
		applied := "no"
		if sm.FloorApplied {
			applied = "yes"
		}
		sb.WriteString(fmt.Sprintf("| Floor minimum (when rules apply) | **%.0f** |\n", sm.FloorMinScore))
		sb.WriteString(fmt.Sprintf("| Floor applied | **%s** |\n", applied))
		if len(sm.FloorReasons) > 0 {
			sb.WriteString("| Floor reasons | ")
			sb.WriteString(strings.Join(sm.FloorReasons, "; "))
			sb.WriteString(" |\n")
		}
	} else {
		sb.WriteString("| Floor rules | _none_ |\n")
	}
	sb.WriteString(fmt.Sprintf("| **Final score** | **%.1f** |\n", sm.FinalScore))
	sb.WriteString(fmt.Sprintf("| **Final band** | **%s** |\n", sm.FinalBand))
	sb.WriteString("\n## Domain hits\n\n")
	if len(r.Signals.DomainHits) == 0 {
		sb.WriteString("_None._\n")
	} else {
		sb.WriteString("| Domain | Files |\n|--------|-------|\n")
		for _, k := range sortedDomainKeys(r.Signals.DomainHits) {
			sb.WriteString(fmt.Sprintf("| %s | %d |\n", k, r.Signals.DomainHits[k]))
		}
	}

	sb.WriteString("\n## Risk categories (decision lanes)\n\n")
	if len(r.Categories) == 0 {
		sb.WriteString("_No category breakdown._\n")
	} else {
		sb.WriteString("| Category | Risk score | Confidence |\n")
		sb.WriteString("|----------|------------:|------------:|\n")
		for _, c := range r.Categories {
			conf := ""
			if c.Key == CategoryTestConfidence {
				conf = fmt.Sprintf("%.0f", c.Confidence)
			}
			sb.WriteString(fmt.Sprintf("| %s | %.1f | %s |\n", c.Label, c.RiskScore, conf))
		}
	}
	// Confidence breakdown sub-table (test_confidence lane only).
	for _, c := range r.Categories {
		if c.Key == CategoryTestConfidence && c.Breakdown != nil {
			sb.WriteString("\n### Test confidence breakdown\n\n")
			sb.WriteString(fmt.Sprintf("Base score: %.0f\n\n", c.Breakdown.BaseScore))
			if len(c.Breakdown.Adjustments) > 0 {
				sb.WriteString("| Reason | Δ |\n|--------|---:|\n")
				for _, adj := range c.Breakdown.Adjustments {
					sign := "+"
					if adj.Delta < 0 {
						sign = ""
					}
					sb.WriteString(fmt.Sprintf("| %s | %s%.0f |\n", adj.Reason, sign, adj.Delta))
				}
				sb.WriteString(fmt.Sprintf("\n**Final confidence score: %.0f / 100**\n", c.Breakdown.FinalScore))
			}
			break
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

	sb.WriteString("\n## Reducers (what lowers risk)\n\n")
	if len(r.Reducers) == 0 {
		sb.WriteString("_No reducers matched._\n")
	} else {
		for _, red := range r.Reducers {
			sb.WriteString(fmt.Sprintf("### `%s` (-%.1f points)\n\n", red.ID, red.Points))
			sb.WriteString(fmt.Sprintf("- %s\n", red.Label))
			if red.CategoryKey != "" {
				sb.WriteString(fmt.Sprintf("- Primarily affects: `%s`\n", red.CategoryKey))
			}
			if red.Evidence != "" {
				sb.WriteString(fmt.Sprintf("- Evidence: %s\n", red.Evidence))
			}
			sb.WriteString("\n")
		}
	}

	sb.WriteString("\n## Required actions before merge\n\n")
	if len(r.RequiredActions) == 0 {
		sb.WriteString("_No required actions for this risk profile. Review mitigations if helpful._\n")
	} else {
		for _, a := range r.RequiredActions {
			sb.WriteString(fmt.Sprintf("### %s\n\n", a.Title))
			for _, c := range a.Checklist {
				sb.WriteString(fmt.Sprintf("- %s\n", c))
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
