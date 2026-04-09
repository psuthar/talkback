// Package mcpserver wires the TalkBack MCP JSON-RPC server (see cmd/talkback-mcp).
//
// Protocol stack (Model Context Protocol over newline-delimited JSON-RPC on stdio):
//
//   - [NewTalkbackMCPServer] builds [github.com/modelcontextprotocol/go-sdk/mcp.Server] with server
//     instructions, then installs receiving middleware ([Auth.RequireToolAuthMiddleware]) so only
//     tools/call is API-key gated; initialize, tools/list, and other non-tool methods are unchanged.
//   - [Register] attaches tool handlers (health_check in health.go; optional DB tools).
//   - The binary calls [github.com/modelcontextprotocol/go-sdk/mcp.Server.Run] with
//     [github.com/modelcontextprotocol/go-sdk/mcp.StdioTransport] (stdout = wire protocol, stderr = logs).
package mcpserver
