package prrisk

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// band returns risk band label from score 0-100.
func band(score float64) string {
	switch {
	case score < 20:
		return "low"
	case score < 45:
		return "medium"
	case score < 70:
		return "high"
	default:
		return "critical"
	}
}

func clamp100(x float64) float64 {
	if x < 0 {
		return 0
	}
	if x > 100 {
		return 100
	}
	return math.Round(x*10) / 10
}

// Score applies weighted deterministic factors. jiraKey is optional (for Integrations hook).
func Score(s Signals, w ScoreWeights, jiraKey string) Result {
	now := time.Now().UTC()
	factors := make([]RiskFactor, 0, 16)
	sum := 0.0

	add := func(id, label string, pts float64, detail string) {
		if pts <= 0 {
			return
		}
		factors = append(factors, RiskFactor{ID: id, Label: label, Points: pts, Detail: detail})
		sum += pts
	}

	loc := float64(s.TotalLOC)
	if s.GitError != "" {
		add("git_unavailable", "Git diff unavailable — risk from change size unknown", 25, s.GitError)
	} else {
		if loc >= float64(w.VeryLargeDiffLOC) {
			add("diff_very_large", "Very large diff (high churn)", w.VeryLargeDiffPoints,
				fmt.Sprintf("total LOC churn (add+del)=%d (threshold %d)", s.TotalLOC, w.VeryLargeDiffLOC))
		} else if loc >= float64(w.LargeDiffLOC) {
			add("diff_large", "Large diff", w.LargeDiffPoints,
				fmt.Sprintf("total LOC churn=%d (threshold %d)", s.TotalLOC, w.LargeDiffLOC))
		}
		if s.FileCount >= w.ManyFilesThreshold {
			add("many_files", "Many files touched", w.ManyFilesPoints,
				fmt.Sprintf("%d files (threshold %d)", s.FileCount, w.ManyFilesThreshold))
		}
	}

	if s.DomainHits[DomainAuth] > 0 {
		add("domain_auth", "Auth/session/invite area changed", w.AuthPoints,
			fmt.Sprintf("%d file(s) in auth-related paths", s.DomainHits[DomainAuth]))
	}
	if s.MigrationFiles > 0 || s.DomainHits[DomainMigrations] > 0 {
		add("domain_migrations", "Database migrations present", w.MigrationsPoints,
			fmt.Sprintf("%d migration file(s)", s.MigrationFiles))
	}
	if s.DomainHits[DomainRAG] > 0 {
		add("domain_rag", "RAG pipeline changed", w.RAGPoints,
			fmt.Sprintf("%d file(s)", s.DomainHits[DomainRAG]))
	}
	if s.DomainHits[DomainProcessing] > 0 {
		add("domain_processing", "Processing/transcription pipeline changed", w.ProcessingPoints,
			fmt.Sprintf("%d file(s)", s.DomainHits[DomainProcessing]))
	}

	webLOC := webChurn(s)
	if webLOC >= w.WebLargeLOC {
		add("web_large", "Large frontend change", w.WebLargePoints,
			fmt.Sprintf("estimated web LOC churn≈%d (threshold %d)", webLOC, w.WebLargeLOC))
	}

	if s.DomainHits[DomainWorkflows] > 0 {
		add("ci_workflows", "CI/GitHub Actions workflows changed", w.WorkflowsPoints,
			fmt.Sprintf("%d workflow file(s)", s.DomainHits[DomainWorkflows]))
	}
	if s.DomainHits[DomainDeploy] > 0 {
		add("deploy_config", "Deploy / container / hosting config changed", w.DeployPoints,
			fmt.Sprintf("%d file(s)", s.DomainHits[DomainDeploy]))
	}
	if goModChanged(s) {
		add("go_mod_deps", "Go module or dependency lockfile changed", w.ConfigPoints,
			"go.mod and/or go.sum modified")
	}

	if touchesSensitiveCodeWithoutTests(s) {
		add("tests_missing", "Sensitive areas changed without test file changes in this diff", w.TestsMissingPoints,
			"no *_test.go / web test paths in diff; consider adding or updating tests")
	}

	// Reducers deterministically lower risk by subtracting points from the base sum.
	reducers := DetectReducers(s, factors, w)
	reducerSum := 0.0
	for _, r := range reducers {
		reducerSum += r.Points
	}

	score := clamp100(sum - reducerSum)
	if score > 100 {
		score = 100
	}

	riskBand := band(score)
	cats := ComputeCategories(s, factors, reducers)
	req := ComputeRequiredActions(s, factors, reducers, score, riskBand)

	mits := Mitigate(factors)
	integ := BuildIntegrations(factors, score, s.BaseRef, jiraKey)

	return Result{
		Version:         Version,
		VersionMinor:    VersionMinor,
		GeneratedAt:     now,
		BaseRef:         s.BaseRef,
		Signals:         s,
		RiskScore:       score,
		RiskBand:        riskBand,
		Factors:         factors,
		Categories:      cats,
		Reducers:        reducers,
		RequiredActions: req,
		Mitigations:     mits,
		Integrations:    integ,
	}
}

func webChurn(s Signals) int {
	n := 0
	for _, f := range s.Files {
		if strings.HasPrefix(f.Path, "web/") {
			n += f.Added + f.Deleted
		}
	}
	return n
}
