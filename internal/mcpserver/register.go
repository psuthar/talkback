package mcpserver

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/psuthar/talkback/internal/database"
	"github.com/psuthar/talkback/internal/storage"
)

// RegisterConfig controls which tools are mounted and shared version metadata.
type RegisterConfig struct {
	Version string
	// DB when non-nil enables session tools (requires DATABASE_URL at process start).
	DB *database.DB
	// Storage optional; passed to RAG index/search/ask paths (nil = same as historical MCP: DB-only index, R2 PDFs may skip page chunking).
	Storage storage.Interface
}

// Register adds tools (health_check; DB-backed session tools when DB is configured). Log lines go to stderr in main.
func Register(server *mcp.Server, cfg RegisterConfig) {
	registerHealthCheck(server, cfg)
	if cfg.DB != nil {
		// SCRUM-39: session read path via internal/database; same ACL as HTTP.
		registerGetSessionMetadata(server, cfg.DB)
		// SCRUM-55 / SCRUM-60: persisted structured decisions; get_decisions is an alias of get_session_decisions.
		registerGetSessionDecisionsTools(server, cfg.DB)
		// SCRUM-64: decision fields matched by topic across accessible sessions.
		registerGetDecisionsByTopic(server, cfg.DB)
		// SCRUM-56 / SCRUM-61: ephemeral on-read action items; get_action_items is an alias of get_session_action_items.
		registerGetSessionActionItemsTools(server, cfg.DB, cfg.Storage)
		// SCRUM-43 / SCRUM-48: deterministic session chunk search (internal/rag retrieval); search_session + legacy alias search_session_content.
		registerSearchSessionTools(server, cfg.DB, cfg.Storage)
		// SCRUM-63: cross-session ranked search (acting user's accessible sessions only).
		registerSearchAllSessions(server, cfg.DB)
		// SCRUM-45 / SCRUM-52: raw ranked chunks + scores (no LLM synthesis); get_session_raw_chunks + legacy get_session_retrieval_context.
		registerGetSessionRetrievalContextTools(server, cfg.DB, cfg.Storage)
		// SCRUM-46: list indexed chunks by transcript/material/link source (EnsureSessionIndex + session_chunks).
		registerGetSessionSourceChunks(server, cfg.DB, cfg.Storage)
		// SCRUM-44: session-scoped RAG Q&A (internal/utils QA + persistence).
		registerAskSessionQuestionTools(server, cfg.DB, cfg.Storage)
	}
}
