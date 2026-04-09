package mcpserver

import (
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/psuthar/talkback/internal/database"
)

// RegisterConfig controls which tools are mounted and shared version metadata.
type RegisterConfig struct {
	Version string
	// DB when non-nil enables get_session_metadata (requires DATABASE_URL at process start).
	DB *database.DB
}

// Register adds tools (health_check; get_session_metadata when DB is configured). Log lines go to stderr in main.
func Register(server *mcp.Server, cfg RegisterConfig) {
	registerHealthCheck(server, cfg)
	if cfg.DB != nil {
		// SCRUM-39: session read path via internal/database; same ACL as HTTP.
		registerGetSessionMetadata(server, cfg.DB)
	}
}
