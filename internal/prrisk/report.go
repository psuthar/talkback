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
	sb.WriteString(fmt.Sprintf("# TalkBack PR Risk Report (%s)\n\n", ReportVersionString()))
	sb.WriteString(fmt.Sprintf("**Generated:** %s  \n", r.GeneratedAt.Format("2006-01-02T15:04:05Z")))
	sb.WriteString(fmt.Sprintf("**Base ref:** `%s`  \n\n", r.BaseRef))
	if r.Interpretation != "" {
		sb.WriteString(fmt.Sprintf("> %s\n\n", r.Interpretation))
	}
	sb.WriteString("## Summary\n\n")
	sb.WriteString("| Metric | Value |\n|--------|-------|\n")
	sb.WriteString(fmt.Sprintf("| Risk score | **%.1f** / 100 |\n", r.RiskScore))
	sb.WriteString(fmt.Sprintf("| Band | **%s** |\n", r.RiskBand))
	sb.WriteString(fmt.Sprintf("| Report version | **%s** |\n", r.ReportVersion))
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
	enf := r.Enforcement
	sb.WriteString("\n## Enforcement & merge\n\n")
	sb.WriteString("| Item | Value |\n|------|-------|\n")
	sb.WriteString(fmt.Sprintf("| **Merge recommendation** | **%s** |\n", strings.ToUpper(enf.MergeRecommendation)))
	sb.WriteString(fmt.Sprintf("| Rationale | %s |\n", enf.Rationale))
	if es := enf.EvidenceSummary; es.PassCount+es.MissingCount+es.FailCount+es.NotEvaluatedCount > 0 {
		sb.WriteString(fmt.Sprintf("| Evidence | %d pass · %d missing · %d fail · %d not evaluated |\n",
			es.PassCount, es.MissingCount, es.FailCount, es.NotEvaluatedCount))
	}
	sb.WriteString("\n### Recommended review strategy\n\n")
	sb.WriteString(enf.RecommendedReview.Strategy + "\n\n")
	sb.WriteString("### Review routing (recommended)\n\n")
	if len(enf.RecommendedReview.RoutingHints) == 0 {
		sb.WriteString("_None._\n\n")
	} else {
		for _, h := range enf.RecommendedReview.RoutingHints {
			sb.WriteString(fmt.Sprintf("- %s\n", h))
		}
		sb.WriteString("\n")
	}
	sb.WriteString("### Blocking / elevated review reasons\n\n")
	if len(enf.BlockingReasons) == 0 {
		sb.WriteString("_None._\n\n")
	} else {
		for _, b := range enf.BlockingReasons {
			sb.WriteString(fmt.Sprintf("- %s\n", b))
		}
		sb.WriteString("\n")
	}
	sb.WriteString("### Policy trace (deterministic)\n\n")
	if len(enf.Reasons) == 0 {
		sb.WriteString("_None._\n\n")
	} else {
		for _, x := range enf.Reasons {
			sb.WriteString(fmt.Sprintf("- %s\n", x))
		}
		sb.WriteString("\n")
	}
	sb.WriteString("### Required validations before merge\n\n")
	if len(enf.RequiredValidations) == 0 {
		sb.WriteString("_None beyond standard CI._\n\n")
	} else {
		for _, v := range enf.RequiredValidations {
			sb.WriteString(fmt.Sprintf("- %s\n", v))
		}
		sb.WriteString("\n")
	}
	// Show all evidence items except EvidenceUnknown (truly no-information entries).
	// EvidenceNotEvaluated items are shown with 📋 to indicate human review is required.
	var visibleEvidence []ValidationEvidence
	for _, ev := range enf.EvidenceStatus {
		if ev.Status != EvidenceUnknown {
			visibleEvidence = append(visibleEvidence, ev)
		}
	}
	if len(visibleEvidence) > 0 {
		sb.WriteString("### Evidence status (repo-local signals)\n\n")
		sum := enf.EvidenceSummary
		sb.WriteString(fmt.Sprintf(
			"> ✅ %d pass · ⚠️ %d missing · ❌ %d fail · 📋 %d not evaluated (human review required)\n\n",
			sum.PassCount, sum.MissingCount, sum.FailCount, sum.NotEvaluatedCount,
		))
		sb.WriteString("| Action / Validation | Status | Source | Rationale |\n")
		sb.WriteString("|---------------------|--------|--------|-----------|\n")
		for _, ev := range visibleEvidence {
			icon := evidenceStatusIcon(ev.Status)
			label := string(ev.Status)
			if ev.Status == EvidenceNotEvaluated {
				label = "not evaluated"
			}
			sb.WriteString(fmt.Sprintf("| `%s` | %s %s | %s | %s |\n",
				ev.ID, icon, label, ev.Source, ev.Rationale))
		}
		sb.WriteString("\n")
	}
	sb.WriteString("### Review requirements\n\n")
	if len(enf.ReviewRequirements) == 0 {
		sb.WriteString("_None._\n\n")
	} else {
		for _, v := range enf.ReviewRequirements {
			sb.WriteString(fmt.Sprintf("- %s\n", v))
		}
		sb.WriteString("\n")
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

	if r.ContextInsights != nil {
		ci := r.ContextInsights
		sb.WriteString("\n## Context insights\n\n")
		sb.WriteString("### Test–code proximity\n\n")
		sb.WriteString(fmt.Sprintf("- **Structural alignment:** `%s` — %s\n", ci.Proximity.StructuralAlignment, ci.Proximity.Detail))
		if ci.Proximity.BehavioralCoverage != "" {
			sb.WriteString(fmt.Sprintf("- **Behavioral coverage depth:** `%s`\n", ci.Proximity.BehavioralCoverage))
		}
		sb.WriteString(fmt.Sprintf("- Non-test files: **%d** with nearby test in diff: **%d** (ratio **%.0f%%**)\n",
			ci.Proximity.NonTestFiles, ci.Proximity.WithNearbyTestInDiff, ci.Proximity.Ratio*100))

		sb.WriteString("\n### Change concentration\n\n")
		sb.WriteString(fmt.Sprintf("- **Mode:** `%s` — %s\n", ci.Concentration.Mode, ci.Concentration.Detail))
		if ci.Concentration.TopPrefix != "" {
			sb.WriteString(fmt.Sprintf("- Top area: `%s` (~%.0f%% of churn); **%d** distinct path prefixes.\n",
				ci.Concentration.TopPrefix, ci.Concentration.TopShare*100, ci.Concentration.UniqueDirs))
		}

		if len(ci.Hotspots) > 0 {
			sb.WriteString("\n### Hotspots (recent git activity)\n\n")
			for _, h := range ci.Hotspots {
				sb.WriteString(fmt.Sprintf("- **`%s`** — %d recent path hits — %s\n", h.Prefix, h.RecentCount, h.Detail))
			}
		} else {
			sb.WriteString("\n### Hotspots (recent git activity)\n\n")
			sb.WriteString("_No overlapping high-churn prefixes detected (or git history unavailable)._\n")
		}

		sb.WriteString("\n### PR intent vs diff\n\n")
		if ci.Intent.IntentStrength != "" {
			sb.WriteString(fmt.Sprintf("- **Intent strength:** `%s`\n", ci.Intent.IntentStrength))
		}
		if ci.Intent.Title != "" {
			sb.WriteString(fmt.Sprintf("- **Subject line (source):** %s\n", ci.Intent.Title))
		}
		if len(ci.Intent.KeywordsMatched) > 0 {
			sb.WriteString(fmt.Sprintf("- **Keywords matched:** %s\n", strings.Join(ci.Intent.KeywordsMatched, ", ")))
		}
		if len(ci.Intent.DomainsExpected) > 0 {
			sb.WriteString(fmt.Sprintf("- **Domains implied by text:** %s\n", strings.Join(ci.Intent.DomainsExpected, ", ")))
		}
		if len(ci.Intent.DomainsInDiff) > 0 {
			sb.WriteString(fmt.Sprintf("- **Domains in diff (non-test):** %s\n", strings.Join(ci.Intent.DomainsInDiff, ", ")))
		}
		aligned := "yes"
		if ci.Intent.IntentStrength == "unknown" {
			aligned = "n/a"
		} else if !ci.Intent.Aligned {
			aligned = "no"
		}
		sb.WriteString(fmt.Sprintf("- **Aligned:** %s — %s\n", aligned, ci.Intent.Detail))
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
			prio := a.Priority
			if prio == "" {
				prio = priorityForActionID(a.ID)
			}
			sb.WriteString(fmt.Sprintf("### [ %s ] %s\n\n", prio, a.Title))
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
