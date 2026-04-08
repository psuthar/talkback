package mcpserver

import (
	"encoding/json"
	"testing"
)

func TestHealthCheckJSONShape(t *testing.T) {
	m := map[string]string{
		"status":  "ok",
		"service": "talkback-mcp",
		"version": "test-1",
	}
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]string
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if out["status"] != "ok" || out["service"] != "talkback-mcp" || out["version"] != "test-1" {
		t.Fatalf("unexpected: %+v", out)
	}
}
