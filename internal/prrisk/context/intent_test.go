package riskcontext

import (
	"strings"
	"testing"
)

// Tests run without a real git repo; all rely on PRTitle/PRBody so no fallback needed.

func TestIntentNoTextSkipped(t *testing.T) {
	in := Input{
		PRTitle:    "",
		PRBody:     "",
		GitError:   "no repo", // prevents git HEAD fallback
		DomainHits: map[string]int{"auth": 1},
	}
	r := AnalyzeIntent(in)
	if r.Mismatch {
		t.Error("expected no mismatch when no text available")
	}
	if !r.Aligned {
		t.Error("expected aligned=true when skipped")
	}
}

func TestIntentWeakTitleNoKeywordsNotAligned(t *testing.T) {
	in := Input{
		PRTitle:    "wip",
		DomainHits: map[string]int{"auth": 1},
	}
	r := AnalyzeIntent(in)
	if r.IntentStrength != "weak" {
		t.Errorf("expected weak intent strength, got %q", r.IntentStrength)
	}
	if r.Aligned {
		t.Error("weak title with no keywords should not be aligned")
	}
	if r.Mismatch {
		t.Error("weak title should not set mismatch (no expected domains)")
	}
}

func TestIntentNoKeywordsSkipped(t *testing.T) {
	// Generic words that match no keyword rule.
	in := Input{
		PRTitle:    "chore: bump version and update readme",
		DomainHits: map[string]int{"web": 1},
	}
	r := AnalyzeIntent(in)
	if r.Mismatch {
		t.Error("expected no mismatch when no strong keywords matched")
	}
	if len(r.KeywordsMatched) != 0 {
		t.Errorf("expected no matched keywords, got %v", r.KeywordsMatched)
	}
}

func TestIntentAligned(t *testing.T) {
	// Title says "auth", diff touches auth domain.
	// Avoid "session" which also maps to "api" domain and would cause a mismatch.
	in := Input{
		PRTitle:    "fix: auth token expiry handling",
		DomainHits: map[string]int{"auth": 2},
	}
	r := AnalyzeIntent(in)
	if r.Mismatch {
		t.Errorf("expected no mismatch for auth title + auth diff, got detail: %s", r.Detail)
	}
	if !r.Aligned {
		t.Error("expected aligned=true")
	}
}

func TestIntentMismatch(t *testing.T) {
	// Title says "auth" but diff only touches migrations — mismatch.
	in := Input{
		PRTitle:    "fix: auth login flow",
		DomainHits: map[string]int{"migrations": 1},
	}
	r := AnalyzeIntent(in)
	if !r.Mismatch {
		t.Errorf("expected mismatch when auth keyword but only migrations in diff, got detail: %s", r.Detail)
	}
	if r.Aligned {
		t.Error("expected aligned=false")
	}
}

func TestIntentMigrationKeyword(t *testing.T) {
	// Avoid "session" which also maps to "api" domain.
	in := Input{
		PRTitle:    "add migration for new table",
		DomainHits: map[string]int{"migrations": 1},
	}
	r := AnalyzeIntent(in)
	if r.Mismatch {
		t.Errorf("expected no mismatch for migration title + migrations diff: %s", r.Detail)
	}
}

func TestIntentWorkflowKeyword(t *testing.T) {
	in := Input{
		PRTitle:    "ci: update workflow to add prrisk step",
		DomainHits: map[string]int{"workflows": 1},
	}
	r := AnalyzeIntent(in)
	if r.Mismatch {
		t.Errorf("expected no mismatch for workflow keyword + workflows diff: %s", r.Detail)
	}
}

func TestIntentKeywordInBody(t *testing.T) {
	// Keyword only in body, not title. Avoid "session" which adds "api" to expected.
	in := Input{
		PRTitle:    "improvements",
		PRBody:     "Updated the auth token handling.",
		DomainHits: map[string]int{"auth": 1},
	}
	r := AnalyzeIntent(in)
	if r.Mismatch {
		t.Errorf("expected no mismatch when keyword in body matches diff: %s", r.Detail)
	}
}

func TestIntentWebE2EKeywordExpectsWebDomain(t *testing.T) {
	// "e2e" keyword → expects "web" domain (not "tests", which is excluded from domainsPresent).
	// Diff that touches web/ files satisfies the expectation.
	in := Input{
		PRTitle:    "e2e: add creator access test",
		DomainHits: map[string]int{"web": 2, "tests": 4},
	}
	r := AnalyzeIntent(in)
	if r.Mismatch {
		t.Errorf("expected no mismatch for e2e keyword with web domain: %s", r.Detail)
	}
}

func TestIntentE2EKeywordMismatchesWhenOnlyTestFiles(t *testing.T) {
	// "e2e" expects "web" domain; a diff with only tests domain (excluded from inDiff)
	// does not satisfy "web" → mismatch. This documents the known limitation.
	in := Input{
		PRTitle:    "e2e: add creator access test",
		DomainHits: map[string]int{"tests": 4},
	}
	r := AnalyzeIntent(in)
	if !r.Mismatch {
		t.Error("e2e keyword with tests-only diff should be a mismatch (tests excluded from domainsPresent)")
	}
}

func TestIntentTitleRecordedOnResult(t *testing.T) {
	in := Input{
		PRTitle:    "fix: auth token refresh",
		DomainHits: map[string]int{"auth": 1},
	}
	r := AnalyzeIntent(in)
	if r.Title != "fix: auth token refresh" {
		t.Errorf("expected title on result, got %q", r.Title)
	}
}

func TestIntentDomainsInDiffPopulated(t *testing.T) {
	in := Input{
		PRTitle:    "update auth and rag",
		DomainHits: map[string]int{"auth": 1, "rag": 1, "tests": 2},
	}
	r := AnalyzeIntent(in)
	found := map[string]bool{}
	for _, d := range r.DomainsInDiff {
		found[d] = true
	}
	if !found["auth"] || !found["rag"] {
		t.Errorf("expected auth and rag in DomainsInDiff, got %v", r.DomainsInDiff)
	}
	// "tests" domain should be excluded from DomainsInDiff
	if found["tests"] {
		t.Error("tests domain should be excluded from DomainsInDiff")
	}
}

func TestDomainsPresent(t *testing.T) {
	got := domainsPresent(map[string]int{"auth": 1, "tests": 3, "rag": 0})
	found := map[string]bool{}
	for _, d := range got {
		found[d] = true
	}
	if !found["auth"] {
		t.Error("expected auth in domainsPresent result")
	}
	if found["tests"] {
		t.Error("tests should be excluded from domainsPresent")
	}
	if found["rag"] {
		t.Error("rag with count 0 should be excluded from domainsPresent")
	}
}

// --- IntentStrength ---

func TestIntentStrengthStrongWhenKeywordsAligned(t *testing.T) {
	in := Input{
		PRTitle:    "fix: auth token expiry handling",
		DomainHits: map[string]int{"auth": 1},
	}
	r := AnalyzeIntent(in)
	if r.IntentStrength != "strong" {
		t.Errorf("expected strong for normal title with matched keywords, got %q", r.IntentStrength)
	}
}

func TestIntentStrengthUnknownWhenNoKeywords(t *testing.T) {
	in := Input{
		PRTitle:    "chore: bump version and update readme",
		DomainHits: map[string]int{"web": 1},
	}
	r := AnalyzeIntent(in)
	if r.IntentStrength != "unknown" {
		t.Errorf("expected unknown when no strong keywords matched, got %q", r.IntentStrength)
	}
}

func TestIntentStrengthUnknownWhenNoText(t *testing.T) {
	in := Input{
		PRTitle:    "",
		PRBody:     "",
		GitError:   "no repo",
		DomainHits: map[string]int{"auth": 1},
	}
	r := AnalyzeIntent(in)
	if r.IntentStrength != "unknown" {
		t.Errorf("expected unknown when no text available, got %q", r.IntentStrength)
	}
}

// TestIntentWeakTitleWithMatchingKeyword verifies that a weak title (≤4 chars) containing
// a domain keyword produces IntentStrength="weak" even when domain alignment succeeds,
// and that the detail mentions the weak title.
func TestIntentWeakTitleWithMatchingKeyword(t *testing.T) {
	// "auth" is exactly 4 chars → weak. It also matches the "auth" keyword rule.
	in := Input{
		PRTitle:    "auth",
		DomainHits: map[string]int{"auth": 1},
	}
	r := AnalyzeIntent(in)
	if r.IntentStrength != "weak" {
		t.Errorf("expected weak for 4-char title with keyword match, got %q", r.IntentStrength)
	}
	if r.Mismatch {
		t.Error("expected no mismatch: auth keyword + auth domain present")
	}
	if !strings.Contains(r.Detail, "weak") {
		t.Errorf("expected weak note in detail, got %q", r.Detail)
	}
}

// --- isWeakTitle ---

func TestIsWeakTitleEmpty(t *testing.T) {
	if !isWeakTitle("") {
		t.Error("empty string should be a weak title")
	}
}

func TestIsWeakTitleShortLength(t *testing.T) {
	for _, title := range []string{"go", "ok", "ci", "auth"} {
		if !isWeakTitle(title) {
			t.Errorf("title %q (len %d) should be weak (≤4 chars)", title, len(title))
		}
	}
}

func TestIsWeakTitleGenericMap(t *testing.T) {
	for _, title := range []string{"wip", "fix", "update", "bump", "patch", "misc", "draft", "changes", "temp", "test"} {
		if !isWeakTitle(title) {
			t.Errorf("generic title %q should be weak", title)
		}
	}
}

func TestIsWeakTitleSingleTokenShort(t *testing.T) {
	// Single-token (no spaces) titles with length ≤8 are weak.
	for _, title := range []string{"cleanup", "refactor"} {
		if !isWeakTitle(title) {
			t.Errorf("single-token title %q (len %d) should be weak", title, len(title))
		}
	}
}

func TestIsWeakTitleNormalTitlesNotWeak(t *testing.T) {
	for _, title := range []string{
		"fix: auth token expiry handling",
		"add migration for users table",
		"refactoring",              // 11 chars, has no space but > 8
		"ci: update prrisk step",   // has space, > 4, not in generic map
	} {
		if isWeakTitle(title) {
			t.Errorf("title %q should not be weak", title)
		}
	}
}

func TestContainsDomain(t *testing.T) {
	if !containsDomain([]string{"auth", "rag"}, "auth") {
		t.Error("expected auth to be found")
	}
	if containsDomain([]string{"auth", "rag"}, "migrations") {
		t.Error("expected migrations not to be found")
	}
	// web domain: accepted if "tests" present
	if !containsDomain([]string{"tests"}, "web") {
		t.Error("expected web domain to be accepted when tests domain is present")
	}
}
