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

// queriesDoc is the JSON structure in ops/observability/queries.json.
type queriesDoc struct {
	WindowMinutes int         `json:"window_minutes"`
	Queries      []QuerySpec `json:"queries"`
}

// LoadQueries loads the default queries file with resolved appName and substitutes {{window}}, {{appNameClause}}, {{appNameWhereAnd}}.
// resolvedAppName is the result of ResolveAppName; requireAppNameFilter makes LoadQueriesFromPath fail if appName is empty.
func LoadQueries(cfg Config, resolvedAppName string, requireAppNameFilter bool) ([]QuerySpec, error) {
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

// LoadQueriesFromPath loads queries from the given path and substitutes {{window}}, {{appNameClause}}, {{appNameWhereAnd}}.
// appNameClause is "WHERE appName = 'name'" when appName is set (appName must not contain single quotes); otherwise "".
// appNameWhereAnd is "WHERE appName = 'name' AND " when appName set, else "WHERE " (for queries that add more conditions).
// If requireAppNameFilter is true and appName is empty, returns an error.
func LoadQueriesFromPath(path string, windowMins int, appName string, requireAppNameFilter bool) ([]QuerySpec, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}

	var doc queriesDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}

	if len(doc.Queries) == 0 {
		return nil, fmt.Errorf("%s: no queries defined", path)
	}

	appName = strings.TrimSpace(appName)
	if appName != "" && strings.Contains(appName, "'") {
		return nil, fmt.Errorf("app name must not contain single quotes")
	}
	if requireAppNameFilter && appName == "" {
		return nil, fmt.Errorf("OBS_REQUIRE_APPNAME_FILTER is true but app name is empty; set OBS_APP_NAME or ensure discover_appnames returns one")
	}

	appNameClause := ""
	appNameWhereAnd := "WHERE "
	if appName != "" {
		appNameClause = "WHERE appName = '" + appName + "'"
		appNameWhereAnd = "WHERE appName = '" + appName + "' AND "
	}

	windowStr := fmt.Sprintf("%d", windowMins)
	out := make([]QuerySpec, len(doc.Queries))
	for i, q := range doc.Queries {
		nrql := strings.ReplaceAll(q.NRQL, "{{window}}", windowStr)
		nrql = strings.ReplaceAll(nrql, "{{appNameClause}}", appNameClause)
		nrql = strings.ReplaceAll(nrql, "{{appNameWhereAnd}}", appNameWhereAnd)
		out[i] = QuerySpec{Name: q.Name, NRQL: nrql}
	}
	return out, nil
}
