package mcpserver

import (
	"context"
	"encoding/json"
	"log"
	"time"

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
	mcp.AddTool(server, &mcp.Tool{
		Name:        ToolHealthCheck,
		Description: "Returns connectivity and readiness for the TalkBack MCP server (no session or TalkBack business data).",
	}, func(_ context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		start := time.Now()
		tool := ToolHealthCheck
		defer func() {
			log.Printf("mcp tool=%s duration_ms=%d", tool, time.Since(start).Milliseconds())
		}()

		body := map[string]string{
			"status":  "ok",
			"service": "talkback-mcp",
			"version": cfg.Version,
		}
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, nil, err
		}
		return &mcp.CallToolResult{
			Content: []mcp.Content{
				&mcp.TextContent{Text: string(raw)},
			},
		}, nil, nil
	})
	if cfg.DB != nil {
		registerGetSessionMetadata(server, cfg.DB)
	}
}
