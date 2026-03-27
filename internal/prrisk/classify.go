package prrisk

import (
	"path/filepath"
	"strings"
)

// ClassifyDomain returns one primary domain label for a repo-relative path (forward slashes).
func ClassifyDomain(path string) string {
	p := strings.ToLower(filepath.ToSlash(strings.TrimSpace(path)))
	if p == "" {
		return DomainOther
	}

	switch {
	case strings.HasPrefix(p, "db/migrations/") || strings.HasPrefix(p, "internal/migrations/"):
		return DomainMigrations
	case IsTestPath(p):
		return DomainTests
	case strings.HasPrefix(p, "internal/auth/"):
		return DomainAuth
	case strings.Contains(p, "internal/handlers/") && strings.Contains(p, "login"):
		return DomainAuth
	case strings.HasPrefix(p, "internal/invitations/"):
		return DomainAuth
	case strings.HasPrefix(p, "internal/handlers/"):
		return DomainAPI
	case strings.HasPrefix(p, "internal/database/"):
		return DomainDatabase
	case strings.HasPrefix(p, "internal/rag/"):
		return DomainRAG
	case strings.HasPrefix(p, "internal/processing/"):
		return DomainProcessing
	case strings.HasPrefix(p, "internal/storage/"):
		return DomainStorage
	case strings.HasPrefix(p, "web/"):
		return DomainWeb
	case strings.HasPrefix(p, ".github/workflows/"):
		return DomainWorkflows
	case strings.HasPrefix(p, "deploy/") || p == "dockerfile" || strings.HasSuffix(p, "/dockerfile"):
		return DomainDeploy
	case strings.HasSuffix(p, "render.yaml") || strings.Contains(p, "/render.yaml"):
		return DomainDeploy
	case strings.HasPrefix(p, "cmd/"):
		return DomainAPI
	case strings.HasPrefix(p, "scripts/"):
		return DomainScripts
	default:
		if strings.HasPrefix(p, "internal/") {
			return DomainAPI
		}
		return DomainOther
	}
}

// IsTestPath reports whether the path is considered test-only or test-heavy.
func IsTestPath(path string) bool {
	p := strings.ToLower(filepath.ToSlash(path))
	if p == "" {
		return false
	}
	if strings.HasSuffix(p, "_test.go") {
		return true
	}
	if strings.Contains(p, "/testdata/") {
		return true
	}
	if strings.Contains(p, "/e2e/") || strings.Contains(p, "playwright") {
		return true
	}
	if strings.HasSuffix(p, ".spec.ts") || strings.HasSuffix(p, ".spec.tsx") {
		return true
	}
	if strings.Contains(p, "__tests__/") || strings.Contains(p, ".test.ts") || strings.Contains(p, ".test.tsx") {
		return true
	}
	return false
}

// IsConfigPath reports CI/deploy/config-ish paths.
func IsConfigPath(path string) bool {
	p := strings.ToLower(filepath.ToSlash(path))
	if strings.HasPrefix(p, ".github/") {
		return true
	}
	if strings.HasPrefix(p, "deploy/") {
		return true
	}
	if p == "go.mod" || p == "go.sum" {
		return true
	}
	if strings.HasSuffix(p, "dockerfile") || p == "dockerfile" {
		return true
	}
	if strings.HasSuffix(p, "render.yaml") {
		return true
	}
	return false
}

// IsMigrationPath reports SQL migration paths.
func IsMigrationPath(path string) bool {
	p := strings.ToLower(filepath.ToSlash(path))
	return strings.HasPrefix(p, "db/migrations/") || strings.HasPrefix(p, "internal/migrations/")
}

// touchesSensitiveCodeWithoutTests: non-test code in risky areas with zero test file changes.
func touchesSensitiveCodeWithoutTests(s Signals) bool {
	if s.TestFiles > 0 {
		return false
	}
	for _, f := range s.Files {
		if IsTestPath(f.Path) {
			continue
		}
		d := ClassifyDomain(f.Path)
		switch d {
		case DomainAuth, DomainAPI, DomainDatabase, DomainRAG, DomainProcessing, DomainWeb, DomainMigrations:
			return true
		}
	}
	return false
}
