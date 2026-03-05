package obsworker

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadQueriesFromPath_WindowReplaced(t *testing.T) {
	path := filepath.Join("testdata", "queries.json")
	base, _, err := LoadQueriesFromPath(path, 15, "", false)
	if err != nil {
		t.Fatalf("LoadQueriesFromPath: %v", err)
	}
	for _, s := range base {
		if strings.Contains(s.NRQL, "{{window}}") {
			t.Errorf("query %q still contains {{window}}: %s", s.Name, s.NRQL)
		}
		if !strings.Contains(s.NRQL, "15") {
			t.Errorf("query %q should contain 15 minutes, got: %s", s.Name, s.NRQL)
		}
	}
}

func TestLoadQueriesFromPath_MissingFile(t *testing.T) {
	_, _, err := LoadQueriesFromPath("testdata/nonexistent.json", 30, "", false)
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !strings.Contains(err.Error(), "read") && !strings.Contains(err.Error(), "no such file") {
		t.Errorf("error should mention read or file, got: %v", err)
	}
}

func TestLoadQueriesFromPath_ExpectedNamesAndNRQL(t *testing.T) {
	path := filepath.Join("testdata", "queries.json")
	base, deepDive, err := LoadQueriesFromPath(path, 30, "", false)
	if err != nil {
		t.Fatalf("LoadQueriesFromPath: %v", err)
	}
	if len(base) != 2 {
		t.Fatalf("expected 2 base queries, got %d", len(base))
	}
	if len(deepDive) != 0 {
		t.Fatalf("legacy schema: expected 0 deep-dive queries, got %d", len(deepDive))
	}
	names := map[string]bool{}
	for _, s := range base {
		names[s.Name] = true
		if s.NRQL == "" {
			t.Errorf("query %q has empty NRQL", s.Name)
		}
	}
	if !names["throughput"] || !names["errors"] {
		t.Errorf("expected names throughput and errors, got %v", names)
	}
}

func TestLoadQueriesFromPath_AppFilterInjected(t *testing.T) {
	path := filepath.Join("testdata", "queries.json")
	base, _, err := LoadQueriesFromPath(path, 30, "talkback-api-prod", false)
	if err != nil {
		t.Fatalf("LoadQueriesFromPath: %v", err)
	}
	var foundFilter bool
	for _, s := range base {
		if strings.Contains(s.NRQL, "{{appNameClause}}") {
			t.Errorf("query %q still contains {{appNameClause}}", s.Name)
		}
		if strings.Contains(s.NRQL, "WHERE appName = 'talkback-api-prod'") {
			foundFilter = true
		}
	}
	if !foundFilter {
		t.Error("expected at least one query to contain WHERE appName = 'talkback-api-prod' when appName is set")
	}
}

func TestLoadQueriesFromPath_RejectsAppNameWithSingleQuote(t *testing.T) {
	path := filepath.Join("testdata", "queries.json")
	_, _, err := LoadQueriesFromPath(path, 30, "app'name", false)
	if err == nil {
		t.Fatal("expected error when appName contains single quote")
	}
	if !strings.Contains(err.Error(), "single quote") {
		t.Errorf("error should mention single quote, got: %v", err)
	}
}

func TestLoadQueriesFromPath_RequireAppNameFilterFailsWhenEmpty(t *testing.T) {
	path := filepath.Join("testdata", "queries.json")
	_, _, err := LoadQueriesFromPath(path, 30, "", true)
	if err == nil {
		t.Fatal("expected error when requireAppNameFilter is true and appName is empty")
	}
	if !strings.Contains(err.Error(), "OBS_REQUIRE_APPNAME_FILTER") && !strings.Contains(err.Error(), "empty") {
		t.Errorf("error should mention require filter or empty app name, got: %v", err)
	}
}

func TestLoadQueriesFromPath_V2Schema_BaseAndDeepDive(t *testing.T) {
	path := filepath.Join("testdata", "queries_v2.json")
	base, deepDive, err := LoadQueriesFromPath(path, 30, "MyApp", false)
	if err != nil {
		t.Fatalf("LoadQueriesFromPath: %v", err)
	}
	if len(base) != 2 {
		t.Fatalf("expected 2 base queries, got %d", len(base))
	}
	if len(deepDive) != 2 {
		t.Fatalf("expected 2 deep-dive queries, got %d", len(deepDive))
	}
	baseNames := make(map[string]bool)
	for _, s := range base {
		baseNames[s.Name] = true
		if strings.Contains(s.NRQL, "{{window}}") || strings.Contains(s.NRQL, "{{appNameClause}}") {
			t.Errorf("base query %q still has template: %s", s.Name, s.NRQL)
		}
	}
	if !baseNames["txn_summary"] || !baseNames["throughput"] {
		t.Errorf("expected base names txn_summary and throughput, got %v", baseNames)
	}
	deepNames := make(map[string]bool)
	for _, s := range deepDive {
		deepNames[s.Name] = true
		if !strings.Contains(s.NRQL, "WHERE appName = 'MyApp'") {
			t.Errorf("deep-dive query %q should have app filter, got: %s", s.Name, s.NRQL)
		}
		if !strings.Contains(s.NRQL, "30") {
			t.Errorf("deep-dive query %q should have window 30, got: %s", s.Name, s.NRQL)
		}
	}
	if !deepNames["latency_by_txn_p95"] || !deepNames["errors_by_txn"] {
		t.Errorf("expected deep-dive names latency_by_txn_p95 and errors_by_txn, got %v", deepNames)
	}
}
