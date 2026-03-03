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

// LoadQueries loads the default queries file, substitutes {{window}} and {{appFilter}} from cfg,
// and returns the query specs. Caller should run from repo root so "ops/observability/queries.json" resolves.
func LoadQueries(cfg Config) ([]QuerySpec, error) {
	path := "ops/observability/queries.json"
	if _, err := os.Stat(path); err != nil {
		if exe, err := os.Executable(); err == nil {
			alt := filepath.Join(filepath.Dir(exe), "..", "..", path)
			if _, err := os.Stat(alt); err == nil {
				path = alt
			}
		}
	}
	return LoadQueriesFromPath(path, cfg.WindowMins, cfg.AppName)
}

// LoadQueriesFromPath loads queries from the given path and substitutes {{window}} and {{appFilter}}.
// appFilter is "WHERE appName = 'name'" when appName is set and non-empty (appName must not contain single quotes); otherwise "".
// Used by tests with a custom path (e.g. testdata/queries.json).
func LoadQueriesFromPath(path string, windowMins int, appName string) ([]QuerySpec, error) {
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

	appFilter := ""
	if appName = strings.TrimSpace(appName); appName != "" {
		if strings.Contains(appName, "'") {
			return nil, fmt.Errorf("OBS_APP_NAME must not contain single quotes")
		}
		appFilter = "WHERE appName = '" + appName + "'"
	}

	windowStr := fmt.Sprintf("%d", windowMins)
	out := make([]QuerySpec, len(doc.Queries))
	for i, q := range doc.Queries {
		nrql := strings.ReplaceAll(q.NRQL, "{{window}}", windowStr)
		nrql = strings.ReplaceAll(nrql, "{{appFilter}}", appFilter)
		out[i] = QuerySpec{Name: q.Name, NRQL: nrql}
	}
	return out, nil
}
