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

// LoadQueries loads the default queries file, substitutes {{window}} with cfg.WindowMins,
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
	return LoadQueriesFromPath(path, cfg.WindowMins)
}

// LoadQueriesFromPath loads queries from the given path and substitutes {{window}} with windowMins.
// Used by tests with a custom path (e.g. testdata/queries.json).
func LoadQueriesFromPath(path string, windowMins int) ([]QuerySpec, error) {
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

	windowStr := fmt.Sprintf("%d", windowMins)
	out := make([]QuerySpec, len(doc.Queries))
	for i, q := range doc.Queries {
		out[i] = QuerySpec{
			Name: q.Name,
			NRQL: strings.ReplaceAll(q.NRQL, "{{window}}", windowStr),
		}
	}
	return out, nil
}
