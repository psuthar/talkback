package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/psuthar/talkback/internal/mcpserver"
)

// buildTestHTTPMux mirrors the HTTP-mode mux setup in main() without starting a listener.
// This is the same code path exercised when TALKBACK_MCP_HTTP_ADDR / PORT is set —
// i.e. the Render.com (remote) deployment path that SCRUM-86 depends on.
//
// Note: uses t.Setenv, so callers must NOT call t.Parallel().
func buildTestHTTPMux(t *testing.T, apiKey string) http.Handler {
	t.Helper()
	t.Setenv("TALKBACK_MCP_API_KEY", apiKey)
	t.Setenv("TALKBACK_MCP_REQUIRE_CLIENT_KEY", "true")
	t.Setenv("TALKBACK_MCP_ACTING_USER_ID", "")
	t.Setenv("TALKBACK_MCP_KEY_USER_MAP_JSON", "")

	auth, err := mcpserver.LoadAuthFromEnv()
	if err != nil {
		t.Fatalf("LoadAuthFromEnv: %v", err)
	}

	server := mcpserver.NewTalkbackMCPServer(mcpserver.TalkbackServerConfig{
		Version: "test",
		DB:      nil,
		Auth:    auth,
	})

	mux := http.NewServeMux()
	mcpserver.RegisterHealthHTTPRoutes(mux, "test", nil)
	mcpHandler := mcp.NewStreamableHTTPHandler(func(_ *http.Request) *mcp.Server {
		return server
	}, &mcp.StreamableHTTPOptions{Stateless: true})
	mux.Handle("/", auth.HTTPBearerAuthMiddleware(mcpHandler))

	return mux
}

// TestHTTPServerIntegration_healthzUnprotected verifies that the /healthz probe endpoint
// is accessible without authentication — required so cloud health checks (Render.com) and
// IDE connectivity probes work before a session is established.
func TestHTTPServerIntegration_healthzUnprotected(t *testing.T) {
	srv := httptest.NewServer(buildTestHTTPMux(t, "integration-key"))
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	var out map[string]string
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("JSON parse: %v — body: %s", err, body)
	}
	if out["status"] != "ok" {
		t.Fatalf("status: %q, body: %s", out["status"], body)
	}
	if out["service"] != mcpserver.TalkbackMCPName {
		t.Fatalf("service: %q", out["service"])
	}
}

// TestHTTPServerIntegration_readyUnprotected verifies that the /ready probe endpoint is
// likewise accessible without auth (so liveness/readiness can be checked without credentials).
func TestHTTPServerIntegration_readyUnprotected(t *testing.T) {
	srv := httptest.NewServer(buildTestHTTPMux(t, "integration-key"))
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/ready")
	if err != nil {
		t.Fatalf("GET /ready: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

// TestHTTPServerIntegration_mcpRequiresBearer verifies that the MCP StreamableHTTP endpoint
// rejects requests with no Authorization header. This is the first line of defence when the
// remote server URL is exposed: unauthenticated POST to / must return 401.
func TestHTTPServerIntegration_mcpRequiresBearer(t *testing.T) {
	srv := httptest.NewServer(buildTestHTTPMux(t, "integration-key"))
	t.Cleanup(srv.Close)

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/", strings.NewReader(`{"jsonrpc":"2.0","method":"initialize","id":1}`))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

// TestHTTPServerIntegration_mcpWrongBearerRejected verifies that a wrong Bearer token is also
// rejected with 401 — confirming the auth middleware validates the token value, not just its presence.
func TestHTTPServerIntegration_mcpWrongBearerRejected(t *testing.T) {
	srv := httptest.NewServer(buildTestHTTPMux(t, "integration-key"))
	t.Cleanup(srv.Close)

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/", strings.NewReader(`{"jsonrpc":"2.0","method":"initialize","id":1}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer wrong-key")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

// TestHTTPServerIntegration_mcpValidBearerPassesThrough verifies that the MCP endpoint
// accepts requests with the correct Bearer token, confirming end-to-end auth plumbing.
// The initialize request is used because it does not require a tool call or DB — the MCP
// handshake itself returning a non-401 status is sufficient proof of auth success.
func TestHTTPServerIntegration_mcpValidBearerPassesThrough(t *testing.T) {
	srv := httptest.NewServer(buildTestHTTPMux(t, "integration-key"))
	t.Cleanup(srv.Close)

	initBody := `{"jsonrpc":"2.0","method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1"}},"id":1}`
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/", strings.NewReader(initBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer integration-key")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /: %v", err)
	}
	defer resp.Body.Close()

	// The MCP initialize method is not gated by tool auth — a non-401 confirms Bearer passed through.
	if resp.StatusCode == http.StatusUnauthorized {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("got 401 with valid Bearer — body: %s", body)
	}
}
