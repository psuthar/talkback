// search_session_content: deterministic session-scoped chunk retrieval (SCRUM-43).
package mcpserver

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/psuthar/talkback/internal/database"
	"github.com/psuthar/talkback/internal/rag"
)

const (
	maxSearchSnippetRunes = 2000
	maxSearchTopKDefault  = 10
	maxSearchTopKCap      = 50
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

func registerSearchSessionContent(server *mcp.Server, db *database.DB) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        ToolSearchSessionContent,
		Description: "Deterministic search over indexed session content: embeds the query and returns top-k chunks by cosine similarity (same ranking as web Q&A, no LLM). Requires DATABASE_URL and TALKBACK_MCP_ACTING_USER_ID; enforces session read access. Output includes snippet, source_type (transcript/material/link), source_id, anchor, and transcript start_ms/end_ms when present.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in searchSessionContentInput) (*mcp.CallToolResult, searchSessionContentOutput, error) {
		start := time.Now()
		tool := ToolSearchSessionContent
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
		if k <= 0 {
			k = maxSearchTopKDefault
		}
		if k > maxSearchTopKCap {
			k = maxSearchTopKCap
		}

		user, err := db.GetUserByID(ctx, actingID)
		if err != nil {
			return nil, searchSessionContentOutput{}, err
		}
		if user == nil {
			return nil, searchSessionContentOutput{}, mcpToolErr(403, "acting user not found in database")
		}

		session, err := db.GetSession(ctx, sessionID)
		if err != nil || session == nil {
			if err != nil && strings.Contains(err.Error(), "not found") {
				return nil, searchSessionContentOutput{}, mcpToolErr(404, "session not found")
			}
			if err != nil {
				return nil, searchSessionContentOutput{}, err
			}
			return nil, searchSessionContentOutput{}, mcpToolErr(404, "session not found")
		}

		if allowed, err := userMayReadSessionMCP(ctx, db, session, user); err != nil {
			return nil, searchSessionContentOutput{}, err
		} else if !allowed {
			return nil, searchSessionContentOutput{}, mcpToolErr(403, "you do not have access to this session")
		}

		embedder := &rag.OpenAIEmbedder{}
		if err := rag.EnsureSessionIndex(ctx, db, embedder, sessionID, nil); err != nil {
			return nil, searchSessionContentOutput{}, mcpToolErr(503, "index unavailable: "+err.Error())
		}

		embeddings, err := embedder.Embed(ctx, []string{query})
		if err != nil {
			return nil, searchSessionContentOutput{}, mcpToolErr(503, "embedding failed: "+err.Error())
		}
		if len(embeddings) == 0 || len(embeddings[0]) == 0 {
			return nil, searchSessionContentOutput{}, mcpToolErr(503, "embedding unavailable (empty vector)")
		}

		primaryVID, err := primaryVideoIDForRetrieval(ctx, db, sessionID)
		if err != nil {
			return nil, searchSessionContentOutput{}, err
		}

		scored, err := rag.RetrieveTopKWithScores(ctx, db, sessionID, embeddings[0], k, primaryVID)
		if err != nil {
			return nil, searchSessionContentOutput{}, mcpToolErr(500, "retrieval failed: "+err.Error())
		}

		out := searchSessionContentOutput{
			SessionID: sessionID.String(),
			Query:     query,
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

func truncateSearchSnippet(s string) string {
	if utf8.RuneCountInString(s) <= maxSearchSnippetRunes {
		return s
	}
	r := []rune(s)
	return string(r[:maxSearchSnippetRunes]) + "…"
}

func anchorMs(m map[string]interface{}, key string) *int64 {
	v, ok := m[key]
	if !ok || v == nil {
		return nil
	}
	switch n := v.(type) {
	case float64:
		x := int64(n)
		return &x
	case int:
		x := int64(n)
		return &x
	case int64:
		return &n
	default:
		return nil
	}
}
