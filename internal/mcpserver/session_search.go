// search_session and search_session_content: deterministic session-scoped chunk retrieval (SCRUM-43, SCRUM-48).
package mcpserver

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/psuthar/talkback/internal/database"
)

type searchSessionContentInput struct {
	SessionID string `json:"session_id" jsonschema:"UUID of the TalkBack session"`
	Query     string `json:"query" jsonschema:"Search query (embedded; deterministic ranking)"`
	TopK      int    `json:"top_k,omitempty" jsonschema:"Max results (default 10, max 50)"`
}

type searchSessionHit struct {
	Rank       int                    `json:"rank"`
	Similarity float64                `json:"similarity"`
	Snippet    string                 `json:"snippet"`
	SourceType string                 `json:"source_type"`
	SourceID   *string                `json:"source_id,omitempty"`
	Anchor     map[string]interface{} `json:"anchor,omitempty"`
	StartMs    *int64                 `json:"start_ms,omitempty"`
	EndMs      *int64                 `json:"end_ms,omitempty"`
}

type searchSessionContentOutput struct {
	SessionID string             `json:"session_id"`
	Query     string             `json:"query"`
	Results   []searchSessionHit `json:"results"`
}

func registerSearchSessionTools(server *mcp.Server, db *database.DB) {
	const desc = "Deterministic search over indexed session content: embeds the query and returns top-k chunks by cosine similarity (same ranking as web Q&A, no LLM). Requires DATABASE_URL and TALKBACK_MCP_ACTING_USER_ID; enforces session read access. Output includes snippet, source_type (transcript/material/link), source_id, anchor, and transcript start_ms/end_ms when present."
	registerSearchSessionTool(server, db, ToolSearchSession, desc)
	registerSearchSessionTool(server, db, ToolSearchSessionContent, "Same behavior as "+ToolSearchSession+" (backward-compatible tool name). "+desc)
}

func registerSearchSessionTool(server *mcp.Server, db *database.DB, toolName string, description string) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        toolName,
		Description: description,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in searchSessionContentInput) (*mcp.CallToolResult, searchSessionContentOutput, error) {
		start := time.Now()
		tool := toolName
		var logSessionID string
		defer func() {
			extras := map[string]string{}
			if logSessionID != "" {
				extras["session_id"] = logSessionID
			}
			logMCPToolComplete(tool, time.Since(start), extras)
		}()

		actingID, ok := ActingUserID(ctx)
		if !ok {
			return nil, searchSessionContentOutput{}, mcpToolErr(403, "acting user not configured; set TALKBACK_MCP_ACTING_USER_ID to a TalkBack user UUID")
		}

		sessionID, err := uuid.Parse(strings.TrimSpace(in.SessionID))
		if err != nil {
			return nil, searchSessionContentOutput{}, mcpToolErr(400, "invalid session_id: must be a UUID")
		}
		logSessionID = sessionID.String()

		query := strings.TrimSpace(in.Query)
		if query == "" {
			return nil, searchSessionContentOutput{}, mcpToolErr(400, "query is required")
		}

		k := in.TopK
		if _, _, err := mcpLoadSessionWithReadAccess(ctx, db, sessionID, actingID); err != nil {
			return nil, searchSessionContentOutput{}, err
		}

		scored, queryOut, _, _, err := mcpRunVectorRetrieval(ctx, db, sessionID, query, k)
		if err != nil {
			return nil, searchSessionContentOutput{}, err
		}

		out := searchSessionContentOutput{
			SessionID: sessionID.String(),
			Query:     queryOut,
			Results:   make([]searchSessionHit, 0, len(scored)),
		}
		for i, row := range scored {
			hit := searchSessionHit{
				Rank:       i + 1,
				Similarity: row.Score,
				Snippet:    truncateSearchSnippet(row.Chunk.Text),
				SourceType: row.Chunk.SourceType,
				Anchor:     row.Chunk.AnchorJSON,
			}
			if row.Chunk.SourceID != nil {
				s := row.Chunk.SourceID.String()
				hit.SourceID = &s
			}
			if a := row.Chunk.AnchorJSON; a != nil {
				hit.StartMs = anchorMs(a, "start_ms")
				hit.EndMs = anchorMs(a, "end_ms")
			}
			out.Results = append(out.Results, hit)
		}

		return nil, out, nil
	})
}
