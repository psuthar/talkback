// search_all_sessions: ranked vector search across all sessions visible to the acting user (SCRUM-63).
package mcpserver

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/psuthar/talkback/internal/database"
	"github.com/psuthar/talkback/internal/rag"
)

// Fixed session UUID used only for per-session embedding rate-limit accounting on cross-session search (one embedding per tool call).
var mcpCrossSessionEmbeddingQuotaID = uuid.MustParse("00000000-0000-4000-8000-000000000001")

type searchAllSessionsInput struct {
	Query string `json:"query" jsonschema:"Search query (embedded; ranked across accessible sessions)"`
	TopK  int    `json:"top_k,omitempty" jsonschema:"Max hits (default 10, max 50)"`
}

type searchAllSessionsHit struct {
	Rank       int                    `json:"rank"`
	Similarity float64                `json:"similarity"`
	SessionID  string                 `json:"session_id"`
	Title      string                 `json:"session_title"`
	UpdatedAt  string                 `json:"session_updated_at,omitempty"`
	Snippet    string                 `json:"snippet"`
	SourceType string                 `json:"source_type"`
	SourceID   *string                `json:"source_id,omitempty"`
	Anchor     map[string]interface{} `json:"anchor,omitempty"`
	StartMs    *int64                 `json:"start_ms,omitempty"`
	EndMs      *int64                 `json:"end_ms,omitempty"`
}

type searchAllSessionsOutput struct {
	SchemaVersion string                 `json:"schema_version"`
	Query         string                 `json:"query"`
	Results       []searchAllSessionsHit `json:"results"`
	LLMModel      string                 `json:"llm_model,omitempty"`
	Note          string                 `json:"note,omitempty"`
}

const crossSessionSearchSchemaVersion = "1"

// DefaultMaxCrossSessionChunks caps how many chunk+embedding rows are loaded into memory for
// search_all_sessions ranking (SCRUM-74). Override with TALKBACK_MCP_MAX_CROSS_SESSION_CHUNKS.
const (
	DefaultMaxCrossSessionChunks = 50_000
	minMaxCrossSessionChunks     = 1_000
	maxMaxCrossSessionChunks     = 500_000
)

// crossSessionChunkLoadCap returns the max rows to load from ListChunksWithEmbeddingsBySessionIDs.
// Invalid or empty env falls back to DefaultMaxCrossSessionChunks; values clamp to [min, max].
func crossSessionChunkLoadCap() int {
	s := strings.TrimSpace(os.Getenv("TALKBACK_MCP_MAX_CROSS_SESSION_CHUNKS"))
	if s == "" {
		return DefaultMaxCrossSessionChunks
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < minMaxCrossSessionChunks {
		return DefaultMaxCrossSessionChunks
	}
	if n > maxMaxCrossSessionChunks {
		return maxMaxCrossSessionChunks
	}
	return n
}

func registerSearchAllSessions(server *mcp.Server, db *database.DB) {
	const desc = "Search indexed content across every TalkBack session the acting user may read (creator, member, or global admin within cap). Embeds the query once, ranks chunks by cosine similarity globally, and returns snippets with session_id and session metadata. Does not build missing indexes — sessions without chunk embeddings contribute no hits. Requires DATABASE_URL, acting user, and OPENAI_API_KEY. Auth scope: only sessions visible to the MCP user (SCRUM-70); never a global dump of other tenants."
	mcp.AddTool(server, &mcp.Tool{
		Name:        ToolSearchAllSessions,
		Description: desc,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in searchAllSessionsInput) (*mcp.CallToolResult, searchAllSessionsOutput, error) {
		start := time.Now()
		tool := ToolSearchAllSessions
		defer func() {
			logMCPToolComplete(tool, time.Since(start), nil)
		}()

		if strings.TrimSpace(os.Getenv("OPENAI_API_KEY")) == "" {
			return nil, searchAllSessionsOutput{}, mcpToolErrCode("openai_not_configured", 503, "OPENAI_API_KEY is not set; cross-session search requires query embeddings")
		}

		actingID, ok := ActingUserID(ctx)
		if !ok {
			return nil, searchAllSessionsOutput{}, mcpToolErrCode(mcpErrCodeActingUserNotConfigured, 403, "acting user not configured; set TALKBACK_MCP_ACTING_USER_ID to a TalkBack user UUID")
		}

		q := strings.TrimSpace(in.Query)
		if q == "" {
			return nil, searchAllSessionsOutput{}, mcpToolErr(400, "query is required")
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
			return nil, searchAllSessionsOutput{}, err
		}
		if user == nil {
			return nil, searchAllSessionsOutput{}, mcpToolErr(403, "acting user not found in database")
		}

		sessionIDs, err := db.ListAccessibleSessionIDsForUser(ctx, user)
		if err != nil {
			return nil, searchAllSessionsOutput{}, err
		}
		if len(sessionIDs) == 0 {
			return nil, searchAllSessionsOutput{
				SchemaVersion: crossSessionSearchSchemaVersion,
				Query:         q,
				Results:       nil,
				Note:          "No accessible sessions found for this user.",
			}, nil
		}

		chunks, err := db.ListChunksWithEmbeddingsBySessionIDs(ctx, sessionIDs)
		if err != nil {
			return nil, searchAllSessionsOutput{}, err
		}
		if len(chunks) == 0 {
			return nil, searchAllSessionsOutput{
				SchemaVersion: crossSessionSearchSchemaVersion,
				Query:         q,
				Results:       nil,
				Note:          "No indexed chunks in accessible sessions (build session indexes via session tools or the web app).",
			}, nil
		}

		capN := crossSessionChunkLoadCap()
		truncNote := ""
		if len(chunks) > capN {
			chunks = chunks[:capN]
			truncNote = fmt.Sprintf(
				"Chunk corpus truncated to the first %d rows for ranking (ordered by session_id, source_type, chunk_idx). Raise TALKBACK_MCP_MAX_CROSS_SESSION_CHUNKS (max %d) if you need a larger working set.",
				capN, maxMaxCrossSessionChunks,
			)
		}

		if err := mcpAcquireSessionEmbeddingQuota(mcpCrossSessionEmbeddingQuotaID); err != nil {
			return nil, searchAllSessionsOutput{}, err
		}

		embedder := &rag.OpenAIEmbedder{}
		embeddings, err := embedder.Embed(ctx, []string{q})
		if err != nil {
			code, st, msg := mcpClassifyEmbeddingError(err)
			return nil, searchAllSessionsOutput{}, mcpToolErrCode(code, st, msg)
		}
		if len(embeddings) == 0 || len(embeddings[0]) == 0 {
			return nil, searchAllSessionsOutput{}, mcpToolErrCode("embedding_empty_result", 503, "Embedding provider returned no vector; check model and input.")
		}

		scored := rag.CrossSessionTopKByChunks(chunks, embeddings[0], k)
		if len(scored) == 0 {
			note := "No chunks matched the query embedding (dimension mismatch or empty index rows)."
			if truncNote != "" {
				note = truncNote + " " + note
			}
			return nil, searchAllSessionsOutput{
				SchemaVersion: crossSessionSearchSchemaVersion,
				Query:         q,
				Results:       nil,
				Note:          note,
			}, nil
		}

		metaIDs := make([]uuid.UUID, 0, len(scored))
		seen := make(map[uuid.UUID]struct{})
		for _, row := range scored {
			sid := row.Chunk.SessionID
			if _, ok := seen[sid]; !ok {
				seen[sid] = struct{}{}
				metaIDs = append(metaIDs, sid)
			}
		}
		meta, err := db.ListSessionMetadataByIDs(ctx, metaIDs)
		if err != nil {
			return nil, searchAllSessionsOutput{}, err
		}

		out := searchAllSessionsOutput{
			SchemaVersion: crossSessionSearchSchemaVersion,
			Query:         q,
			Results:       make([]searchAllSessionsHit, 0, len(scored)),
			LLMModel:      embedder.ModelName(),
			Note:          truncNote,
		}
		for i, row := range scored {
			sid := row.Chunk.SessionID
			m := meta[sid]
			hit := searchAllSessionsHit{
				Rank:       i + 1,
				Similarity: row.Score,
				SessionID:  sid.String(),
				Title:      m.Title,
				Snippet:    truncateSearchSnippet(row.Chunk.Text),
				SourceType: row.Chunk.SourceType,
				Anchor:     row.Chunk.AnchorJSON,
			}
			if !m.UpdatedAt.IsZero() {
				hit.UpdatedAt = m.UpdatedAt.UTC().Format(time.RFC3339)
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
