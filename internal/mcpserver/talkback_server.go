package mcpserver

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/psuthar/talkback/internal/database"
	"github.com/psuthar/talkback/internal/storage"
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
	// Storage optional; when non-nil (e.g. R2), session index build matches HTTP — PDF materials are fetched for chunking (same as SessionAsk).
	Storage storage.Interface
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
		Instructions: "TalkBack MCP: tools include health_check; with DATABASE_URL, get_session_metadata, get_session_decisions, get_session_action_items (ephemeral LLM action items on read), " +
			"search_session (same as search_session_content), search_all_sessions (cross-session ranked search for the acting user), get_session_raw_chunks (same as get_session_retrieval_context), get_session_source_chunks, ask_session (same as ask_session_question) are registered. Configure TALKBACK_MCP_API_KEY; set TALKBACK_MCP_ACTING_USER_ID for session tools, or TALKBACK_MCP_KEY_USER_MAP_JSON in strict key mode (per-key users, SCRUM-70); OPENAI_API_KEY is required for embeddings (search, cross-session search, raw retrieval, index build), action items, and RAG Q&A.",
	}
	s := mcp.NewServer(impl, opts)
	s.AddReceivingMiddleware(cfg.Auth.RequireToolAuthMiddleware())
	Register(s, RegisterConfig{Version: ver, DB: cfg.DB, Storage: cfg.Storage})
	return s
}
