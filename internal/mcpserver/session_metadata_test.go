package mcpserver

import (
	"encoding/json"
	"testing"
)

func TestMCPToolErrJSON(t *testing.T) {
	err := mcpToolErr(404, "session not found")
	var m map[string]any
	if e := json.Unmarshal([]byte(err.Error()), &m); e != nil {
		t.Fatalf("error text should be JSON: %v", e)
	}
	if m["http_status"] != float64(404) {
		t.Fatalf("http_status: %v", m["http_status"])
	}
	if m["error"] != "session not found" {
		t.Fatalf("error: %v", m["error"])
	}
}
