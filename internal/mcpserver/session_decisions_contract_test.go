package mcpserver

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestMcpSessionDecisionsSchemaFileIsValidJSON ensures the repo schema file stays parseable (SCRUM-57).
func TestMcpSessionDecisionsSchemaFileIsValidJSON(t *testing.T) {
	t.Parallel()
	root := repoRootForTests(t)
	path := filepath.Join(root, "docs", "schemas", "mcp-session-decisions-v1.schema.json")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("schema JSON: %v", err)
	}
	if m["$schema"] == nil {
		t.Fatal("expected $schema in JSON Schema file")
	}
}

func repoRootForTests(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 16; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	t.Fatal("go.mod not found from cwd")
	return ""
}
