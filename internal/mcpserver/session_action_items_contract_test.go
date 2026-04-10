package mcpserver

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestMcpSessionActionItemsSchemaFileIsValidJSON ensures the repo schema file stays parseable (SCRUM-58).
func TestMcpSessionActionItemsSchemaFileIsValidJSON(t *testing.T) {
	t.Parallel()
	root := repoRootForTests(t)
	path := filepath.Join(root, "docs", "schemas", "mcp-session-action-items-v1.schema.json")
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
