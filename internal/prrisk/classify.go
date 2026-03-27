package prrisk

import (
	"path/filepath"
	"strings"
)

// ClassifyDomain returns one primary domain label for a repo-relative path (forward slashes).
func ClassifyDomain(path string) string {
	if IsTestPath(path) {
		return DomainTests
	}
	return ClassifyArea(path)
}

// ClassifyArea returns one primary, product-relevant area label for a repo-relative path.
// It intentionally does not treat test paths specially; use IsTestPath/ClassifyDomain for that.
func ClassifyArea(path string) string {
	p := strings.ToLower(filepath.ToSlash(strings.TrimSpace(path)))
	if p == "" {
		return DomainOther
	}

	switch {
	case strings.HasPrefix(p, "db/migrations/") || strings.HasPrefix(p, "internal/migrations/"):
		return DomainMigrations

	// Auth/session and invite flows.
	case strings.HasPrefix(p, "internal/auth/"):
		return DomainAuth
	case strings.HasPrefix(p, "internal/invitations/"):
		return DomainAuth
	case strings.Contains(p, "internal/handlers/") &&
		(strings.Contains(p, "login") ||
			strings.Contains(p, "session_invite") ||
			strings.Contains(p, "invitations") ||
			strings.Contains(p, "session_participant") ||
			strings.Contains(p, "zoom_auth") ||
			strings.Contains(p, "teams_auth") ||
			strings.Contains(p, "auth") ||
			strings.Contains(p, "invite")):
		return DomainAuth

	// Decision-grade Q&A / RAG.
	case strings.HasPrefix(p, "internal/rag/"):
		return DomainRAG
	case strings.Contains(p, "internal/utils/qa.go"):
		return DomainRAG
	case strings.Contains(p, "internal/handlers/session_ask") ||
		strings.Contains(p, "internal/handlers/session_questions") ||
		strings.Contains(p, "internal/handlers/session_reindex"):
		return DomainRAG

	// Material processing / transcription pipeline.
	case strings.HasPrefix(p, "internal/processing/"):
		return DomainProcessing
	case strings.Contains(p, "internal/handlers/transcript_") ||
		strings.Contains(p, "internal/handlers/transcript") ||
		strings.Contains(p, "internal/handlers/video_url_ingestion") ||
		strings.Contains(p, "internal/handlers/video_upload") ||
		strings.Contains(p, "internal/handlers/session_materials") ||
		strings.Contains(p, "internal/handlers/transcript_job_status") ||
		strings.Contains(p, "internal/handlers/transcript_jobs"):
		return DomainProcessing

	// Storage and remaining internal areas.
	case strings.HasPrefix(p, "internal/storage/"):
		return DomainStorage
	case strings.HasPrefix(p, "internal/database/"):
		return DomainDatabase
	case strings.HasPrefix(p, "internal/handlers/"):
		return DomainAPI

	// Frontend.
	case strings.HasPrefix(p, "web/"):
		return DomainWeb

	// CI / deploy / infra config.
	case strings.HasPrefix(p, ".github/workflows/"):
		return DomainWorkflows
	case strings.HasPrefix(p, "deploy/") || p == "dockerfile" || strings.HasSuffix(p, "/dockerfile"):
		return DomainDeploy
	case strings.HasSuffix(p, "render.yaml") || strings.Contains(p, "/render.yaml"):
		return DomainDeploy

	// Misc.
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

// IsE2EPath detects Playwright E2E specs (web/ tests).
func IsE2EPath(path string) bool {
	p := strings.ToLower(filepath.ToSlash(path))
	if strings.Contains(p, "web/tests/e2e/") {
		return true
	}
	// Allow explicit suffix patterns for robustness.
	if strings.Contains(p, "/e2e/") && (strings.HasSuffix(p, ".e2e.ts") || strings.HasSuffix(p, ".e2e.tsx") || strings.HasSuffix(p, ".spec.ts") || strings.HasSuffix(p, ".spec.tsx")) {
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
