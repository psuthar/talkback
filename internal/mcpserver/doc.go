// Package mcpserver wires the TalkBack MCP JSON-RPC server (see cmd/talkback-mcp).
//
// Session DB tools (SCRUM-39, SCRUM-55, SCRUM-56, SCRUM-43, SCRUM-45, SCRUM-46, SCRUM-44, SCRUM-49): when DATABASE_URL is set, [Register] mounts get_session_metadata, get_session_decisions, get_decisions (alias), get_session_action_items, get_action_items (alias),
// search_session, search_session_content (alias), get_session_raw_chunks, get_session_retrieval_context (alias), get_session_source_chunks, ask_session, ask_session_question (alias), which use [github.com/psuthar/talkback/internal/database] and the same access
// checks as HTTP (global admin or UserCanAccessSession). The acting user comes from TALKBACK_MCP_ACTING_USER_ID
// and/or TALKBACK_MCP_KEY_USER_MAP_JSON (per client API key in strict mode — SCRUM-70); see docs/mcp-server.md.
// Optional [RegisterConfig.Storage] (R2 when STORAGE_DRIVER=r2 in cmd/talkback-mcp) matches HTTP session indexing for object-backed PDFs.
//
// Protocol stack (Model Context Protocol over newline-delimited JSON-RPC on stdio):
//
//   - [NewTalkbackMCPServer] builds [github.com/modelcontextprotocol/go-sdk/mcp.Server] with server
//     instructions, then installs receiving middleware (middleware.go, [Auth.RequireToolAuthMiddleware]) so only
//     tools/call is API-key gated; initialize, tools/list, and other non-tool methods are unchanged.
//   - [Register] attaches tool handlers (health_check in health.go; optional DB-backed
//     get_session_metadata, get_session_decisions, get_decisions, get_session_action_items, get_action_items, search_session, search_session_content, get_session_raw_chunks, get_session_retrieval_context, get_session_source_chunks, ask_session, and ask_session_question when DATABASE_URL is configured — SCRUM-39, SCRUM-55, SCRUM-56, SCRUM-60, SCRUM-61, SCRUM-43, SCRUM-48, SCRUM-45, SCRUM-52, SCRUM-46, SCRUM-44, SCRUM-49, SCRUM-50, SCRUM-54).
//   - The binary calls [github.com/modelcontextprotocol/go-sdk/mcp.Server.Run] with
//     [github.com/modelcontextprotocol/go-sdk/mcp.StdioTransport] (stdout = wire protocol, stderr = logs).
//
// Structured stderr logging (SCRUM-40): mcp_log.go and docs/mcp-server.md — tool completion and auth failures
// use parseable key=value lines; API keys, bearer tokens, and full _meta are never logged.
//
// Optional HTTP health (SCRUM-69): when TALKBACK_MCP_HEALTH_ADDR is set, cmd/talkback-mcp registers
// GET /healthz and /ready via [RegisterHealthHTTPRoutes] for Kubernetes probes (stdio MCP unchanged).
package mcpserver
