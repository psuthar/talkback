package mcpserver

import (
	"strings"
	"testing"
)

func TestBuildMCPLogLine_toolComplete_health(t *testing.T) {
	t.Parallel()
	got := buildMCPLogLine(mcpLogEventToolComplete, ToolHealthCheck, 12, nil)
	want := "mcp event=tool_complete tool=health_check duration_ms=12"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestBuildMCPLogLine_toolComplete_sessionIDSorted(t *testing.T) {
	t.Parallel()
	got := buildMCPLogLine(mcpLogEventToolComplete, ToolGetSessionMetadata, 3, map[string]string{
		"session_id": "11111111-1111-1111-1111-111111111111",
	})
	if !strings.HasPrefix(got, "mcp event=tool_complete tool=get_session_metadata duration_ms=3 ") {
		t.Fatalf("prefix: %q", got)
	}
	if !strings.Contains(got, "session_id=11111111-1111-1111-1111-111111111111") {
		t.Fatalf("missing canonical session_id: %q", got)
	}
}

func TestBuildMCPLogLine_dropsDisallowedAndInvalidSessionID(t *testing.T) {
	t.Parallel()
	got := buildMCPLogLine(mcpLogEventToolComplete, ToolHealthCheck, 1, map[string]string{
		"session_id": "not-a-uuid",
		"api_key":    "secret",
		"foo":        "bar",
	})
	want := "mcp event=tool_complete tool=health_check duration_ms=1"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestBuildMCPLogLine_authFailed(t *testing.T) {
	t.Parallel()
	got := buildMCPLogLine(mcpLogEventAuthFailed, ToolHealthCheck, 0, map[string]string{
		"reason": "missing_or_invalid_key",
	})
	want := "mcp event=auth_failed tool=health_check duration_ms=0 reason=missing_or_invalid_key"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestSanitizeMCPToolName(t *testing.T) {
	t.Parallel()
	if got := sanitizeMCPToolName("health_check"); got != "health_check" {
		t.Fatalf("got %q", got)
	}
	if got := sanitizeMCPToolName("evil-tool"); got != "unknown_tool" {
		t.Fatalf("got %q", got)
	}
	if got := sanitizeMCPToolName(""); got != "unknown_tool" {
		t.Fatalf("got %q", got)
	}
}
