// Package mcpserver registers TalkBack MCP tools on an [mcp.Server] (stdio transport).
package mcpserver

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Register adds tools for this milestone (e.g. health_check). Log lines go to the default logger (use stderr in main).
func Register(server *mcp.Server, version string) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        "health_check",
		Description: "Returns connectivity and readiness for the TalkBack MCP server (no session or TalkBack business data).",
	}, func(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		start := time.Now()
		tool := "health_check"
		defer func() {
			log.Printf("mcp tool=%s duration_ms=%d", tool, time.Since(start).Milliseconds())
		}()

		body := map[string]string{
			"status":  "ok",
			"service": "talkback-mcp",
			"version": version,
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
}
