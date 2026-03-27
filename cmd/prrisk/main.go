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
	outDir := flag.String("output-dir", "artifacts/release-readiness", "directory for pr_risk_v2.json/pr_risk_v2_1.json and corresponding .md files")
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

	// Backwards compatible outputs: keep legacy v2 filenames, and add v2.1 variants.
	jsonV2 := filepath.Join(out, "pr_risk_v2.json")
	mdV2 := filepath.Join(out, "pr_risk_v2.md")
	jsonV21 := filepath.Join(out, "pr_risk_v2_1.json")
	mdV21 := filepath.Join(out, "pr_risk_v2_1.md")

	if err := prrisk.WriteJSON(jsonV2, res); err != nil {
		fmt.Fprintf(os.Stderr, "prrisk: write json (v2): %v\n", err)
		os.Exit(1)
	}
	if err := prrisk.WriteJSON(jsonV21, res); err != nil {
		fmt.Fprintf(os.Stderr, "prrisk: write json (v2.1): %v\n", err)
		os.Exit(1)
	}
	if err := prrisk.WriteMarkdown(mdV2, res); err != nil {
		fmt.Fprintf(os.Stderr, "prrisk: write markdown (v2): %v\n", err)
		os.Exit(1)
	}
	if err := prrisk.WriteMarkdown(mdV21, res); err != nil {
		fmt.Fprintf(os.Stderr, "prrisk: write markdown (v2.1): %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("PR risk v2.1: score=%.1f (%s) — wrote %s/%s\n", res.RiskScore, res.RiskBand, filepath.Base(jsonV21), filepath.Base(mdV21))
	if res.Signals.GitError != "" {
		fmt.Fprintf(os.Stderr, "warning: git diff issue: %s\n", res.Signals.GitError)
	}
}
