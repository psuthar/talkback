// Command prrisk runs deterministic PR risk scoring (v2.x) from git diff signals.
//
// Usage:
//
//	go run ./cmd/prrisk --repo-root . --base-ref origin/main --output-dir artifacts/release-readiness
//
// Env:
//   - PRRISK_JIRA_ISSUE_KEY — optional; embedded in report integrations for future Jira linking.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/psuthar/talkback/internal/prrisk"
)

func main() {
	repo := flag.String("repo-root", ".", "repository root (git worktree)")
	base := flag.String("base-ref", "origin/main", "git base ref for diff (e.g. origin/main)")
	outDir := flag.String("output-dir", "artifacts/release-readiness", "directory for pr_risk.json and pr_risk.md")
	jira := flag.String("jira-key", "", "optional Jira issue key (overrides PRRISK_JIRA_ISSUE_KEY)")
	flag.Parse()

	jiraKey := strings.TrimSpace(*jira)
	if jiraKey == "" {
		jiraKey = strings.TrimSpace(os.Getenv(prrisk.EnvJiraIssueKey))
	}

	signals := prrisk.ExtractSignals(*repo, *base)
	res := prrisk.Score(signals, prrisk.DefaultWeights(), jiraKey)

	out := filepath.Clean(*outDir)
	if err := os.MkdirAll(out, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "prrisk: mkdir: %v\n", err)
		os.Exit(1)
	}

	jsonOut := filepath.Join(out, "pr_risk.json")
	mdOut := filepath.Join(out, "pr_risk.md")
	semanticOut := filepath.Clean(filepath.Join(out, "..", "pr-risk.json"))

	if err := prrisk.WriteJSON(jsonOut, res); err != nil {
		fmt.Fprintf(os.Stderr, "prrisk: write json: %v\n", err)
		os.Exit(1)
	}
	if err := prrisk.WriteMarkdown(mdOut, res); err != nil {
		fmt.Fprintf(os.Stderr, "prrisk: write markdown: %v\n", err)
		os.Exit(1)
	}
	if err := prrisk.WriteSemanticPRRiskJSON(semanticOut, res); err != nil {
		fmt.Fprintf(os.Stderr, "prrisk: write semantic json: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("PR risk v%d.%d: score=%.1f (%s) — wrote %s/%s + %s\n",
		prrisk.Version, prrisk.VersionMinor, res.RiskScore, res.RiskBand,
		filepath.Base(jsonOut), filepath.Base(mdOut), filepath.Base(semanticOut))
	if res.Signals.GitError != "" {
		fmt.Fprintf(os.Stderr, "warning: git diff issue: %s\n", res.Signals.GitError)
	}
}
