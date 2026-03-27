package prrisk

import "testing"

func TestClassifyDomain(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		{"internal/auth/session.go", DomainAuth},
		{"internal/handlers/login.go", DomainAuth},
		{"internal/handlers/sessions.go", DomainAPI},
		{"db/migrations/0001_x.up.sql", DomainMigrations},
		{"web/src/App.tsx", DomainWeb},
		{"web/src/foo.test.tsx", DomainTests},
		{"internal/rag/foo.go", DomainRAG},
		{".github/workflows/ci.yml", DomainWorkflows},
		{"go.mod", DomainOther},
	}
	for _, tc := range cases {
		got := ClassifyDomain(tc.path)
		if got != tc.want {
			t.Errorf("ClassifyDomain(%q) = %q, want %q", tc.path, got, tc.want)
		}
	}
}

func TestIsTestPath(t *testing.T) {
	if !IsTestPath("internal/foo/bar_test.go") {
		t.Error("expected _test.go")
	}
	if !IsTestPath("web/e2e/smoke.spec.ts") {
		t.Error("expected e2e spec")
	}
	if IsTestPath("internal/handlers/sessions.go") {
		t.Error("should not be test")
	}
}
