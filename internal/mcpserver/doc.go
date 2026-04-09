// Package mcpserver wires the TalkBack MCP JSON-RPC server (see cmd/talkback-mcp).
//
// Session DB tool (SCRUM-39): when DATABASE_URL is set, [Register] mounts get_session_metadata,
// which uses [github.com/psuthar/talkback/internal/database] and the same access checks as HTTP
// (global admin or UserCanAccessSession). The acting user is TALKBACK_MCP_ACTING_USER_ID.
//
// Protocol stack (Model Context Protocol over newline-delimited JSON-RPC on stdio):
//
//   - [NewTalkbackMCPServer] builds [github.com/modelcontextprotocol/go-sdk/mcp.Server] with server
//     instructions, then installs receiving middleware (middleware.go, [Auth.RequireToolAuthMiddleware]) so only
//     tools/call is API-key gated; initialize, tools/list, and other non-tool methods are unchanged.
//   - [Register] attaches tool handlers (health_check in health.go; optional DB-backed
//     get_session_metadata in session_metadata.go when DATABASE_URL is configured — SCRUM-39).
//   - The binary calls [github.com/modelcontextprotocol/go-sdk/mcp.Server.Run] with
//     [github.com/modelcontextprotocol/go-sdk/mcp.StdioTransport] (stdout = wire protocol, stderr = logs).
//
// Structured stderr logging (SCRUM-40): mcp_log.go and docs/mcp-server.md — tool completion and auth failures
// use parseable key=value lines; API keys, bearer tokens, and full _meta are never logged.
//
// Optional HTTP health (SCRUM-69): when TALKBACK_MCP_HEALTH_ADDR is set, cmd/talkback-mcp registers
// GET /healthz and /ready via [RegisterHealthHTTPRoutes] for Kubernetes probes (stdio MCP unchanged).
package mcpserver
