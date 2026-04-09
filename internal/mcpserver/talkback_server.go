package mcpserver

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/psuthar/talkback/internal/database"
)

// TalkbackMCPName is the MCP implementation name advertised in initialize.
const TalkbackMCPName = "talkback-mcp"

// TalkbackServerConfig holds everything needed to construct the JSON-RPC MCP server
// (stdio transport is attached later via [mcp.Server.Run]).
//
// Wiring order (required by SCRUM-36 / MCP Phase 1):
//  1. [mcp.NewServer] with implementation + server options (instructions, capabilities inference).
//  2. [mcp.Server.AddReceivingMiddleware] with [Auth.RequireToolAuthMiddleware] — applies only to
//     inbound methods; the handler skips non-tool methods so initialize and tools/list stay open.
//  3. [Register] — mounts health_check and optional DB-backed tools.
type TalkbackServerConfig struct {
	Version string
	DB      *database.DB
	Auth    Auth
}

// NewTalkbackMCPServer returns a fully wired [mcp.Server] for TalkBack.
// It implements the MCP session lifecycle and method routing via the official Go SDK; stdio
// framing (newline-delimited JSON-RPC) is provided by [mcp.StdioTransport] at Run time.
func NewTalkbackMCPServer(cfg TalkbackServerConfig) *mcp.Server {
	ver := cfg.Version
	if ver == "" {
		ver = "dev"
	}
	impl := &mcp.Implementation{
		Name:    TalkbackMCPName,
		Version: ver,
	}
	opts := &mcp.ServerOptions{
		Instructions: "TalkBack MCP: tools include health_check; with DATABASE_URL, get_session_metadata, " +
			"search_session_content, and ask_session_question are registered. Configure TALKBACK_MCP_API_KEY; set TALKBACK_MCP_ACTING_USER_ID for session tools; OPENAI_API_KEY is required for RAG Q&A.",
	}
	s := mcp.NewServer(impl, opts)
	s.AddReceivingMiddleware(cfg.Auth.RequireToolAuthMiddleware())
	Register(s, RegisterConfig{Version: ver, DB: cfg.DB})
	return s
}
