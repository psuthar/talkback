package mcpserver

// Well-known MCP tool names mounted by [Register] (stable identifiers for clients and tests).
const (
	ToolHealthCheck        = "health_check"
	ToolGetSessionMetadata = "get_session_metadata"
)

// ToolNames returns the tool identifiers [Register] will add for cfg, in registration order.
// get_session_metadata is included only when DB is non-nil.
func ToolNames(cfg RegisterConfig) []string {
	names := []string{ToolHealthCheck}
	if cfg.DB != nil {
		names = append(names, ToolGetSessionMetadata)
	}
	return names
}
