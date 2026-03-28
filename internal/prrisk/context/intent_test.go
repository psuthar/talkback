package riskcontext

import "testing"

// Tests run without a real git repo; all rely on PRTitle/PRBody so no fallback needed.

func TestIntentNoTextSkipped(t *testing.T) {
	in := Input{
		PRTitle: "",
		PRBody:  "",
		GitError: "no repo", // prevents git HEAD fallback
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
