// Package riskcontext implements deterministic contextual signals for PR risk (v2.4+).
// It must not import internal/prrisk to avoid import cycles; callers map FileChange from prrisk.
package riskcontext

// FileChange is a minimal path + churn record (mirrors prrisk.FileChange).
type FileChange struct {
	Path    string
	Added   int
	Deleted int
}

// Input is everything needed to compute proximity, concentration, hotspots, and intent.
type Input struct {
	RepoRoot string
	// GitError non-empty skips git log–based hotspot and optional intent fallbacks.
	GitError string

	Files  []FileChange
	IsTest []bool // parallel to Files; caller supplies from prrisk.IsTestPath

	// DomainHits uses prrisk domain labels (auth, rag, workflows, …).
	DomainHits map[string]int

	// PR text (optional). If empty, intent analysis uses git HEAD subject/body when possible.
	PRTitle string
	PRBody  string
}
