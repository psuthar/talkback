// Package mcpserver is the shared tool registry for the TalkBack MCP binary (see cmd/talkback-mcp).
// The process entrypoint constructs an [github.com/modelcontextprotocol/go-sdk/mcp.Server], calls
// [Register] to mount tools, then runs the server over stdio using [github.com/modelcontextprotocol/go-sdk/mcp.StdioTransport].
package mcpserver
