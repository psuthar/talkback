package prrisk

import (
	"sort"
	"strings"

	riskcontext "github.com/psuthar/talkback/internal/prrisk/context"
)

func joinCommaSorted(xs []string) string {
	if len(xs) == 0 {
		return ""
	}
	cp := append([]string(nil), xs...)
	sort.Strings(cp)
	return strings.Join(cp, ", ")
}

// ComputeRoutingHints returns deterministic reviewer-routing hints from domains,
// context insights, and factors.
func ComputeRoutingHints(s Signals, factors []RiskFactor, insights *riskcontext.ContextInsights) []string {
	seen := make(map[string]struct{})
	var out []string

	add := func(line string) {
		line = strings.TrimSpace(line)
		if line == "" {
			return
		}
		if _, ok := seen[line]; ok {
			return
		}
		seen[line] = struct{}{}
		out = append(out, line)
	}

	has := factorIDs(factors)

	if s.DomainHits[DomainAuth] > 0 {
		add("Include a reviewer familiar with auth, sessions, and invitations.")
	}
	if s.DomainHits[DomainRAG] > 0 || s.DomainHits[DomainProcessing] > 0 {
		add("Route processing/RAG changes to someone who owns ingestion, jobs, and Q&A quality.")
	}
	if s.DomainHits[DomainOrchestration] > 0 {
		add("Include reviewer familiar with creator orchestration recommendations and draft-approval flows.")
	}
	if s.DomainHits[DomainWeb] > 0 || okBool(has, "web_large") {
		add("Include frontend review for web/ UI and client behavior.")
	}
	if s.MigrationFiles > 0 || s.DomainHits[DomainMigrations] > 0 {
		add("Include database/migrations review before merge.")
	}
	if s.DomainHits[DomainWorkflows] > 0 || s.DomainHits[DomainDeploy] > 0 {
		add("Include CI/infra review for workflow or deploy config changes.")
	}

	if insights != nil {
		if (insights.Concentration.Mode == "focused" || insights.Concentration.Mode == "focused_large") && insights.Concentration.TopPrefix != "" {
			add("Focus primary review on `" + insights.Concentration.TopPrefix + "` (majority of churn).")
		}
		if insights.Concentration.Mode == "focused_large" && insights.Concentration.TopPrefix != "" {
			add("Large single-area churn — run targeted regression and integration checks around `" + insights.Concentration.TopPrefix + "`.")
		}
		if insights.Concentration.Mode == "scattered" && s.FileCount >= 10 {
			add("Split review by subsystem or commit; avoid single-threaded review of unrelated areas.")
		}
		if len(insights.Hotspots) > 0 {
			p := insights.Hotspots[0].Prefix
			add("Extra reviewer attention on `" + p + "` (several recent commits touched this area).")
		}
		if insights.Intent.Mismatch && len(insights.Intent.DomainsExpected) > 0 {
			add("Confirm scope with author: PR text implies domains " + joinCommaSorted(insights.Intent.DomainsExpected) + " but diff may differ.")
		}
	}

	// Broad touches without domain-specific hint: generic routing.
	if len(out) == 0 && s.FileCount > 0 && s.GitError == "" {
		add("Assign reviewers using changed paths and domain hits; use CODEOWNERS if configured.")
	}

	return out
}
