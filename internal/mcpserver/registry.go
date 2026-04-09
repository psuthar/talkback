package mcpserver

// Well-known MCP tool names mounted by [Register] (stable identifiers for clients and tests).
const (
	ToolHealthCheck                = "health_check"
	ToolGetSessionMetadata         = "get_session_metadata"
	ToolSearchSessionContent       = "search_session_content"
	ToolGetSessionRetrievalContext = "get_session_retrieval_context"
	ToolAskSessionQuestion         = "ask_session_question"
)

// ToolNames returns the tool identifiers [Register] will add for cfg, in registration order.
// Session DB tools are included only when DB is non-nil.
func ToolNames(cfg RegisterConfig) []string {
	names := []string{ToolHealthCheck}
	if cfg.DB != nil {
		names = append(names, ToolGetSessionMetadata, ToolSearchSessionContent, ToolGetSessionRetrievalContext, ToolAskSessionQuestion)
	}
	return names
}
