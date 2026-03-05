package obsworker

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// QuerySpec is a single named NRQL query.
type QuerySpec struct {
	Name string `json:"name"`
	NRQL string `json:"nrql"`
}

// queriesDoc is the legacy JSON structure (flat "queries" array).
type queriesDoc struct {
	WindowMinutes int          `json:"window_minutes"`
	Queries       []QuerySpec  `json:"queries"`
}

// queriesDocV2 is the grouped schema: base_queries + deep_dive_queries.
type queriesDocV2 struct {
	WindowMinutes   int          `json:"window_minutes"`
	BaseQueries     []QuerySpec  `json:"base_queries"`
	DeepDiveQueries []QuerySpec  `json:"deep_dive_queries"`
}

// LoadQueries loads the default queries file and returns base + optional deep-dive query lists.
// resolvedAppName is the result of ResolveAppName; requireAppNameFilter makes LoadQueriesFromPath fail if appName is empty.
func LoadQueries(cfg Config, resolvedAppName string, requireAppNameFilter bool) (base []QuerySpec, deepDive []QuerySpec, err error) {
	path := "ops/observability/queries.json"
	if _, err := os.Stat(path); err != nil {
		if exe, err := os.Executable(); err == nil {
			alt := filepath.Join(filepath.Dir(exe), "..", "..", path)
			if _, err := os.Stat(alt); err == nil {
				path = alt
			}
		}
	}
	return LoadQueriesFromPath(path, cfg.WindowMins, resolvedAppName, requireAppNameFilter)
}

// LoadQueriesFromPath loads queries from the given path. Supports:
// - New schema: base_queries (required, non-empty) and deep_dive_queries (optional)
// - Legacy schema: single "queries" array (used as base, deepDive empty)
// Templating: {{window}}, {{appNameClause}}, {{appNameWhereAnd}}.
func LoadQueriesFromPath(path string, windowMins int, appName string, requireAppNameFilter bool) (base []QuerySpec, deepDive []QuerySpec, err error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("read %s: %w", path, err)
	}

	appName = strings.TrimSpace(appName)
	if appName != "" && strings.Contains(appName, "'") {
		return nil, nil, fmt.Errorf("app name must not contain single quotes")
	}
	if requireAppNameFilter && appName == "" {
		return nil, nil, fmt.Errorf("OBS_REQUIRE_APPNAME_FILTER is true but app name is empty; set OBS_APP_NAME or ensure discover_appnames returns one")
	}

	appNameClause := ""
	appNameWhereAnd := "WHERE "
	if appName != "" {
		appNameClause = "WHERE appName = '" + appName + "'"
		appNameWhereAnd = "WHERE appName = '" + appName + "' AND "
	}
	windowStr := fmt.Sprintf("%d", windowMins)
	applyTemplating := func(q QuerySpec) QuerySpec {
		nrql := strings.ReplaceAll(q.NRQL, "{{window}}", windowStr)
		nrql = strings.ReplaceAll(nrql, "{{appNameClause}}", appNameClause)
		nrql = strings.ReplaceAll(nrql, "{{appNameWhereAnd}}", appNameWhereAnd)
		return QuerySpec{Name: q.Name, NRQL: nrql}
	}

	// Try new schema first
	var docV2 queriesDocV2
	if err := json.Unmarshal(raw, &docV2); err == nil && len(docV2.BaseQueries) > 0 {
		base = make([]QuerySpec, len(docV2.BaseQueries))
		for i, q := range docV2.BaseQueries {
			base[i] = applyTemplating(q)
		}
		deepDive = make([]QuerySpec, len(docV2.DeepDiveQueries))
		for i, q := range docV2.DeepDiveQueries {
			deepDive[i] = applyTemplating(q)
		}
		return base, deepDive, nil
	}

	// Legacy schema
	var doc queriesDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if len(doc.Queries) == 0 {
		return nil, nil, fmt.Errorf("%s: no queries defined (need base_queries or queries)", path)
	}
	base = make([]QuerySpec, len(doc.Queries))
	for i, q := range doc.Queries {
		base[i] = applyTemplating(q)
	}
	return base, nil, nil
}
