package mcpserver

import (
	"encoding/json"
	"testing"
)

func TestHealthCheckResponse_JSONRoundTrip(t *testing.T) {
	t.Parallel()
	in := HealthCheckResponse{
		Status:  "ok",
		Service: TalkbackMCPName,
		Version: "1.0.0",
	}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	var out HealthCheckResponse
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if out.Status != "ok" || out.Service != TalkbackMCPName || out.Version != "1.0.0" {
		t.Fatalf("unexpected: %+v", out)
	}
}

func TestHealthCheckVersion(t *testing.T) {
	t.Parallel()
	if got := healthCheckVersion(RegisterConfig{Version: "x"}); got != "x" {
		t.Fatalf("got %q", got)
	}
	if got := healthCheckVersion(RegisterConfig{}); got != "dev" {
		t.Fatalf("empty version: got %q want dev", got)
	}
}
