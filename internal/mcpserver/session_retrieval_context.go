// get_session_raw_chunks and get_session_retrieval_context: raw ranked chunks + scores for agent-side reasoning (SCRUM-45, SCRUM-52).
// Same vector retrieval stack as search_session / search_session_content; different payload (chunk ids, full text window, metadata). No LLM.
package mcpserver

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/psuthar/talkback/internal/database"
	"github.com/psuthar/talkback/internal/storage"
)

type getSessionRetrievalContextInput struct {
	SessionID string `json:"session_id" jsonschema:"UUID of the TalkBack session"`
	Query     string `json:"query" jsonschema:"Query string to embed for retrieval (no LLM synthesis)"`
	TopK      int    `json:"top_k,omitempty" jsonschema:"Max results (default 10, max 50)"`
}

type retrievalContextMeta struct {
	RetrievalMethod   string  `json:"retrieval_method"`
	EmbeddingModel    string  `json:"embedding_model"`
	TopK              int     `json:"top_k"`
	PrimaryVideoID    *string `json:"primary_video_id,omitempty"`
	ScoreBoostApplied bool    `json:"primary_transcript_score_boost_applied"`
}

type retrievalContextChunk struct {
	Rank         int                    `json:"rank"`
	ChunkID      string                 `json:"chunk_id"`
	ChunkIdx     int                    `json:"chunk_idx"`
	Score        float64                `json:"score"`
	SourceType   string                 `json:"source_type"`
	SourceID     *string                `json:"source_id,omitempty"`
	Text         string                 `json:"text"`
	ContentHash  string                 `json:"content_hash,omitempty"`
	Anchor       map[string]interface{} `json:"anchor,omitempty"`
	StartMs      *int64                 `json:"start_ms,omitempty"`
	EndMs        *int64                 `json:"end_ms,omitempty"`
	CreatedAtRFC string                 `json:"created_at,omitempty"`
}

type retrievalContextPayload struct {
	Meta   retrievalContextMeta    `json:"metadata"`
	Chunks []retrievalContextChunk `json:"chunks"`
}

type getSessionRetrievalContextOutput struct {
	SessionID        string                  `json:"session_id"`
	Query            string                  `json:"query"`
	RetrievalContext retrievalContextPayload `json:"retrieval_context"`
}

func registerGetSessionRetrievalContextTools(server *mcp.Server, db *database.DB, store storage.Interface) {
	const desc = "Raw session retrieval (chunks only): ranked chunks with cosine similarity scores and metadata (chunk_id, chunk_idx, content_hash, anchors, created_at). Uses the same embedding + RetrieveTopKWithScores stack as search_session / search_session_content; output is for agent-side reasoning and citations — no LLM answer generation. Requires DATABASE_URL, TALKBACK_MCP_ACTING_USER_ID, and OPENAI_API_KEY for the query embedding."
	registerGetSessionRetrievalContextTool(server, db, store, ToolGetSessionRawChunks, desc)
	registerGetSessionRetrievalContextTool(server, db, store, ToolGetSessionRetrievalContext, "Same behavior as "+ToolGetSessionRawChunks+" (SCRUM-45 name). "+desc)
}

func registerGetSessionRetrievalContextTool(server *mcp.Server, db *database.DB, store storage.Interface, toolName string, description string) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        toolName,
		Description: description,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in getSessionRetrievalContextInput) (*mcp.CallToolResult, getSessionRetrievalContextOutput, error) {
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
			return nil, getSessionRetrievalContextOutput{}, mcpToolErrCode(mcpErrCodeActingUserNotConfigured, 403, "acting user not configured; set TALKBACK_MCP_ACTING_USER_ID to a TalkBack user UUID")
		}

		sessionID, err := uuid.Parse(strings.TrimSpace(in.SessionID))
		if err != nil {
			return nil, getSessionRetrievalContextOutput{}, mcpToolErr(400, "invalid session_id: must be a UUID")
		}
		logSessionID = sessionID.String()

		query := strings.TrimSpace(in.Query)
		if query == "" {
			return nil, getSessionRetrievalContextOutput{}, mcpToolErr(400, "query is required")
		}

		k := in.TopK
		if _, _, err := mcpLoadSessionWithReadAccess(ctx, db, sessionID, actingID); err != nil {
			return nil, getSessionRetrievalContextOutput{}, err
		}

		scored, queryOut, primaryVID, embedModel, err := mcpRunVectorRetrieval(ctx, db, sessionID, query, k, store)
		if err != nil {
			return nil, getSessionRetrievalContextOutput{}, err
		}

		reqK := k
		if reqK <= 0 {
			reqK = maxSearchTopKDefault
		}
		if reqK > maxSearchTopKCap {
			reqK = maxSearchTopKCap
		}
		meta := retrievalContextMeta{
			RetrievalMethod:   "cosine_similarity_top_k",
			EmbeddingModel:    embedModel,
			TopK:              reqK,
			ScoreBoostApplied: primaryVID != nil,
		}
		if primaryVID != nil {
			s := primaryVID.String()
			meta.PrimaryVideoID = &s
		}

		chunks := make([]retrievalContextChunk, 0, len(scored))
		for i, row := range scored {
			rc := retrievalContextChunk{
				Rank:         i + 1,
				ChunkID:      row.Chunk.ID.String(),
				ChunkIdx:     row.Chunk.ChunkIdx,
				Score:        row.Score,
				SourceType:   row.Chunk.SourceType,
				Text:         truncateRetrievalChunkText(row.Chunk.Text),
				ContentHash:  row.Chunk.ContentHash,
				Anchor:       row.Chunk.AnchorJSON,
				CreatedAtRFC: row.Chunk.CreatedAt.UTC().Format(time.RFC3339),
			}
			if row.Chunk.SourceID != nil {
				s := row.Chunk.SourceID.String()
				rc.SourceID = &s
			}
			if a := row.Chunk.AnchorJSON; a != nil {
				rc.StartMs = anchorMs(a, "start_ms")
				rc.EndMs = anchorMs(a, "end_ms")
			}
			chunks = append(chunks, rc)
		}

		out := getSessionRetrievalContextOutput{
			SessionID: sessionID.String(),
			Query:     queryOut,
			RetrievalContext: retrievalContextPayload{
				Meta:   meta,
				Chunks: chunks,
			},
		}
		return nil, out, nil
	})
}
