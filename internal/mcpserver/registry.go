package mcpserver

// Well-known MCP tool names mounted by [Register] (stable identifiers for clients and tests).
const (
	ToolHealthCheck                = "health_check"
	ToolGetSessionMetadata         = "get_session_metadata"
	ToolGetSessionDecisions        = "get_session_decisions"
	ToolGetDecisions               = "get_decisions"
	ToolGetDecisionsByTopic        = "get_decisions_by_topic"
	ToolGetSessionActionItems      = "get_session_action_items"
	ToolGetActionItems             = "get_action_items"
	ToolSearchSession              = "search_session"
	ToolSearchSessionContent       = "search_session_content"
	ToolGetSessionRawChunks        = "get_session_raw_chunks"
	ToolGetSessionRetrievalContext = "get_session_retrieval_context"
	ToolGetSessionSourceChunks     = "get_session_source_chunks"
	ToolAskSession                 = "ask_session"
	ToolAskSessionQuestion         = "ask_session_question"
	ToolSearchAllSessions          = "search_all_sessions"
)

// ToolNames returns the tool identifiers [Register] will add for cfg, in registration order.
// Session DB tools are included only when DB is non-nil.
func ToolNames(cfg RegisterConfig) []string {
	names := []string{ToolHealthCheck}
	if cfg.DB != nil {
		names = append(names, ToolGetSessionMetadata, ToolGetSessionDecisions, ToolGetDecisions, ToolGetDecisionsByTopic, ToolGetSessionActionItems, ToolGetActionItems, ToolSearchSession, ToolSearchSessionContent, ToolSearchAllSessions, ToolGetSessionRawChunks, ToolGetSessionRetrievalContext, ToolGetSessionSourceChunks, ToolAskSession, ToolAskSessionQuestion)
	}
	return names
}
