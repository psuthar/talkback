package mcpserver

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// HealthCheckResponse is the JSON object returned by the health_check tool (text content).
// Fields are stable for scripts and agents; do not add secrets or environment dumps.
type HealthCheckResponse struct {
	// Status is "ok" when the MCP server process is serving tool calls.
	// "degraded" is reserved for a future signal (e.g. optional dependencies unhealthy) without failing the tool.
	Status string `json:"status"`
	// Service is always [TalkbackMCPName].
	Service string `json:"service"`
	// Version is the server version string (e.g. from -version flag or "dev").
	Version string `json:"version"`
}

func healthCheckVersion(cfg RegisterConfig) string {
	if cfg.Version != "" {
		return cfg.Version
	}
	return "dev"
}

// registerHealthCheck mounts the health_check MCP tool: minimal connectivity check with no TalkBack business data.
func registerHealthCheck(server *mcp.Server, cfg RegisterConfig) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        ToolHealthCheck,
		Description: "Returns connectivity and readiness for the TalkBack MCP server (no session or TalkBack business data).",
	}, func(_ context.Context, _ *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		start := time.Now()
		tool := ToolHealthCheck
		defer func() {
			log.Printf("mcp tool=%s duration_ms=%d", tool, time.Since(start).Milliseconds())
		}()

		raw, err := json.Marshal(HealthCheckResponse{
			Status:  "ok",
			Service: TalkbackMCPName,
			Version: healthCheckVersion(cfg),
		})
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
