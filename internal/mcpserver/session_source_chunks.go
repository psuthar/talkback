// get_session_source_chunks: list indexed transcript/material/link chunks from the RAG session_chunks table (SCRUM-46).
// Aligns with EnsureSessionIndex + same persisted chunks used by RetrieveTopK / RetrieveTopKWithScores.
package mcpserver

import (
	"context"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/psuthar/talkback/internal/database"
	"github.com/psuthar/talkback/internal/rag"
	"github.com/psuthar/talkback/internal/storage"
)

var allowedSessionSourceTypes = map[string]struct{}{
	"transcript": {},
	"material":   {},
	"link":       {},
}

type getSessionSourceChunksInput struct {
	SessionID  string `json:"session_id" jsonschema:"UUID of the TalkBack session"`
	SourceType string `json:"source_type" jsonschema:"transcript | material | link"`
	SourceID   string `json:"source_id,omitempty" jsonschema:"Optional UUID of the video (transcript), material, or link to filter chunks"`
	Limit      int    `json:"limit,omitempty" jsonschema:"Max chunks to return (default 500, max 2000)"`
}

type sourceChunkOut struct {
	ChunkID      string                 `json:"chunk_id"`
	ChunkIdx     int                    `json:"chunk_idx"`
	Text         string                 `json:"text"`
	ContentHash  string                 `json:"content_hash,omitempty"`
	SourceType   string                 `json:"source_type"`
	SourceID     *string                `json:"source_id,omitempty"`
	Anchor       map[string]interface{} `json:"anchor,omitempty"`
	StartMs      *int64                 `json:"start_ms,omitempty"`
	EndMs        *int64                 `json:"end_ms,omitempty"`
	CreatedAtRFC string                 `json:"created_at,omitempty"`
}

type getSessionSourceChunksOutput struct {
	SessionID      string           `json:"session_id"`
	SourceType     string           `json:"source_type"`
	SourceID       *string          `json:"source_id,omitempty"`
	EmbeddingModel string           `json:"embedding_model"`
	Chunks         []sourceChunkOut `json:"chunks"`
	Truncated      bool             `json:"truncated,omitempty"`
}

func registerGetSessionSourceChunks(server *mcp.Server, db *database.DB, store storage.Interface) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        ToolGetSessionSourceChunks,
		Description: "Lists indexed chunks for a transcript, material, or link source within a session (reads session_chunks — the same RAG index used by RetrieveTopK). Calls EnsureSessionIndex first so the index exists. Optional source_id narrows to one video/material/link; omit it to list all chunks of that source_type in the session. No LLM. Requires DATABASE_URL and TALKBACK_MCP_ACTING_USER_ID; OPENAI_API_KEY is required when the index must be built.",
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in getSessionSourceChunksInput) (*mcp.CallToolResult, getSessionSourceChunksOutput, error) {
		start := time.Now()
		tool := ToolGetSessionSourceChunks
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
			return nil, getSessionSourceChunksOutput{}, mcpToolErrCode(mcpErrCodeActingUserNotConfigured, 403, "acting user not configured; set TALKBACK_MCP_ACTING_USER_ID to a TalkBack user UUID")
		}

		sessionID, err := uuid.Parse(strings.TrimSpace(in.SessionID))
		if err != nil {
			return nil, getSessionSourceChunksOutput{}, mcpToolErr(400, "invalid session_id: must be a UUID")
		}
		logSessionID = sessionID.String()

		st := strings.TrimSpace(in.SourceType)
		if st == "" {
			return nil, getSessionSourceChunksOutput{}, mcpToolErr(400, "source_type is required (transcript, material, or link)")
		}
		if _, ok := allowedSessionSourceTypes[st]; !ok {
			return nil, getSessionSourceChunksOutput{}, mcpToolErr(400, "source_type must be transcript, material, or link")
		}

		var filterSourceID *uuid.UUID
		sidStr := strings.TrimSpace(in.SourceID)
		if sidStr != "" {
			parsed, err := uuid.Parse(sidStr)
			if err != nil {
				return nil, getSessionSourceChunksOutput{}, mcpToolErr(400, "invalid source_id: must be a UUID")
			}
			filterSourceID = &parsed
		}

		if _, _, err := mcpLoadSessionWithReadAccess(ctx, db, sessionID, actingID); err != nil {
			return nil, getSessionSourceChunksOutput{}, err
		}

		limit := in.Limit
		if limit <= 0 {
			limit = 500
		}
		if limit > 2000 {
			limit = 2000
		}

		embedder := &rag.OpenAIEmbedder{}
		if err := rag.EnsureSessionIndex(ctx, db, embedder, sessionID, store); err != nil {
			code, msg := mcpClassifyIndexError(err)
			return nil, getSessionSourceChunksOutput{}, mcpToolErrCode(code, 503, msg)
		}

		rows, err := db.ListSessionChunksBySessionIDAndSource(ctx, sessionID, st, filterSourceID, limit)
		if err != nil {
			return nil, getSessionSourceChunksOutput{}, err
		}

		truncated := len(rows) == limit && limit > 0

		outChunks := make([]sourceChunkOut, 0, len(rows))
		for _, c := range rows {
			if c == nil {
				continue
			}
			item := sourceChunkOut{
				ChunkID:      c.ID.String(),
				ChunkIdx:     c.ChunkIdx,
				Text:         truncateRetrievalChunkText(c.Text),
				ContentHash:  c.ContentHash,
				SourceType:   c.SourceType,
				Anchor:       c.AnchorJSON,
				CreatedAtRFC: c.CreatedAt.UTC().Format(time.RFC3339),
			}
			if c.SourceID != nil {
				s := c.SourceID.String()
				item.SourceID = &s
			}
			if a := c.AnchorJSON; a != nil {
				item.StartMs = anchorMs(a, "start_ms")
				item.EndMs = anchorMs(a, "end_ms")
			}
			outChunks = append(outChunks, item)
		}

		var outSourceID *string
		if filterSourceID != nil {
			s := filterSourceID.String()
			outSourceID = &s
		}

		out := getSessionSourceChunksOutput{
			SessionID:      sessionID.String(),
			SourceType:     st,
			SourceID:       outSourceID,
			EmbeddingModel: embedder.ModelName(),
			Chunks:         outChunks,
			Truncated:      truncated,
		}
		return nil, out, nil
	})
}
