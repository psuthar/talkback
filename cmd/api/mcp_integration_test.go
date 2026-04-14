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

// buildTestMCPHandler mirrors the /mcp route setup from main() for a given API key.
// Returns the handler that should be mounted at /mcp/ (already wraps StripPrefix).
//
// Uses t.Setenv, so callers must NOT call t.Parallel().
func buildTestMCPHandler(t *testing.T, apiKey string) http.Handler {
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

	mcpSubMux := http.NewServeMux()
	mcpserver.RegisterHealthHTTPRoutes(mcpSubMux, "test", nil)
	mcpHandler := mcp.NewStreamableHTTPHandler(func(_ *http.Request) *mcp.Server {
		return server
	}, &mcp.StreamableHTTPOptions{
		Stateless:                  true,
		DisableLocalhostProtection: true,
	})
	mcpSubMux.Handle("/", auth.HTTPBearerAuthMiddleware(mcpHandler))

	// Wrap with StripPrefix so that /mcp/healthz → /healthz inside the sub-mux,
	// exactly as registered in main() via http.Handle("/mcp/", http.StripPrefix(...)).
	return http.StripPrefix("/mcp", mcpSubMux)
}

// TestMCPIntegration_healthzUnprotected verifies that GET /mcp/healthz returns 200
// without any Authorization header — required so cloud health checks work without credentials.
func TestMCPIntegration_healthzUnprotected(t *testing.T) {
	srv := httptest.NewServer(buildTestMCPHandler(t, "integration-key"))
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/mcp/healthz")
	if err != nil {
		t.Fatalf("GET /mcp/healthz: %v", err)
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

// TestMCPIntegration_readyUnprotected verifies that GET /mcp/ready returns 200
// without any Authorization header — required so readiness probes work without credentials.
func TestMCPIntegration_readyUnprotected(t *testing.T) {
	srv := httptest.NewServer(buildTestMCPHandler(t, "integration-key"))
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/mcp/ready")
	if err != nil {
		t.Fatalf("GET /mcp/ready: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
}

// TestMCPIntegration_mcpRequiresBearer verifies that POST /mcp/ without an Authorization
// header returns 401 — confirming the Bearer auth middleware is in place.
func TestMCPIntegration_mcpRequiresBearer(t *testing.T) {
	srv := httptest.NewServer(buildTestMCPHandler(t, "integration-key"))
	t.Cleanup(srv.Close)

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/mcp/", strings.NewReader(`{"jsonrpc":"2.0","method":"initialize","id":1}`))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /mcp/: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

// TestMCPIntegration_mcpWrongBearerRejected verifies that a wrong Bearer token returns 401,
// confirming the middleware validates the token value and not just its presence.
func TestMCPIntegration_mcpWrongBearerRejected(t *testing.T) {
	srv := httptest.NewServer(buildTestMCPHandler(t, "integration-key"))
	t.Cleanup(srv.Close)

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/mcp/", strings.NewReader(`{"jsonrpc":"2.0","method":"initialize","id":1}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer wrong-key")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /mcp/: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", resp.StatusCode)
	}
}

// TestMCPIntegration_nonLocalhostHostAllowed verifies that a request with a non-loopback
// Host header (simulating a cloud reverse proxy like Render) is not rejected with 403.
// Before SCRUM-92, the go-sdk DNS rebinding guard blocked these requests because the server
// listens on 127.0.0.1 (httptest default) while the Host header is a public hostname.
func TestMCPIntegration_nonLocalhostHostAllowed(t *testing.T) {
	srv := httptest.NewServer(buildTestMCPHandler(t, "integration-key"))
	t.Cleanup(srv.Close)

	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/mcp/", strings.NewReader(`{"jsonrpc":"2.0","method":"initialize","id":1}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer integration-key")
	// Simulate a reverse proxy forwarding the original public Host header (e.g. Render).
	req.Host = "talkback-895n.onrender.com"

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /mcp/ with public Host: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusForbidden {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("got 403 Forbidden with public Host header — DNS rebinding protection not disabled: %s", body)
	}
}

// TestMCPIntegration_mcpValidBearerPassesThrough verifies that the correct Bearer token
// is accepted and the request reaches the MCP handler (non-401 response).
func TestMCPIntegration_mcpValidBearerPassesThrough(t *testing.T) {
	srv := httptest.NewServer(buildTestMCPHandler(t, "integration-key"))
	t.Cleanup(srv.Close)

	initBody := `{"jsonrpc":"2.0","method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"1"}},"id":1}`
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/mcp/", strings.NewReader(initBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer integration-key")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST /mcp/: %v", err)
	}
	defer resp.Body.Close()

	// A non-401 response confirms the Bearer token was accepted by the auth middleware.
	if resp.StatusCode == http.StatusUnauthorized {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("got 401 with valid Bearer — body: %s", body)
	}
}
