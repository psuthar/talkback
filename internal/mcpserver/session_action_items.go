// get_session_action_items and get_action_items (SCRUM-56, SCRUM-61): same handler — ephemeral on-read extraction of action items from session content.
// JSON contract (SCRUM-58): docs/mcp-session-action-items-schema.md, docs/schemas/mcp-session-action-items-v1.schema.json
// Empty/partial success vs errors: docs/mcp-structured-intelligence-fallbacks.md (SCRUM-62).
// v1: no DB persistence; single embedding + single LLM call per invocation; optional owner only when grounded in context.
package mcpserver

import (
	"context"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/psuthar/talkback/internal/database"
	"github.com/psuthar/talkback/internal/rag"
	"github.com/psuthar/talkback/internal/storage"
	"github.com/psuthar/talkback/internal/utils"
)

const (
	actionItemsSchemaVersion  = "1"
	actionItemsRetrievalQuery = "action items tasks follow-up assignments owners responsibilities next steps decisions"
	actionItemsTopK           = 15
)

type getSessionActionItemsInput struct {
	SessionID string `json:"session_id" jsonschema:"UUID of the TalkBack session"`
}

type sessionActionItemOut struct {
	Description string  `json:"description"`
	Owner       *string `json:"owner,omitempty"`
}

type getSessionActionItemsOutput struct {
	SchemaVersion string                 `json:"schema_version"`
	SessionID     string                 `json:"session_id"`
	ActionItems   []sessionActionItemOut `json:"action_items"`
	LowSignal     bool                   `json:"low_signal"`
	Note          string                 `json:"note,omitempty"`
	LLMModel      string                 `json:"llm_model,omitempty"`
}

func registerGetSessionActionItemsTools(server *mcp.Server, db *database.DB, store storage.Interface) {
	const desc = "Returns ephemeral action items for a session: descriptions and optional owner when explicitly grounded in session content. Generated on read from indexed transcript/materials (RAG) + one LLM call — not persisted (v1). Requires DATABASE_URL, TALKBACK_MCP_ACTING_USER_ID, and OPENAI_API_KEY. Same session ACL as get_session_metadata. Low-signal sessions return an empty list with low_signal=true."
	registerGetSessionActionItemsTool(server, db, store, ToolGetSessionActionItems, desc)
	registerGetSessionActionItemsTool(server, db, store, ToolGetActionItems, "Same behavior as "+ToolGetSessionActionItems+" (SCRUM-61). "+desc)
}

func registerGetSessionActionItemsTool(server *mcp.Server, db *database.DB, store storage.Interface, toolName string, description string) {
	mcp.AddTool(server, &mcp.Tool{
		Name:        toolName,
		Description: description,
	}, func(ctx context.Context, _ *mcp.CallToolRequest, in getSessionActionItemsInput) (*mcp.CallToolResult, getSessionActionItemsOutput, error) {
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

		if strings.TrimSpace(os.Getenv("OPENAI_API_KEY")) == "" {
			return nil, getSessionActionItemsOutput{}, mcpToolErrCode("openai_not_configured", 503, "OPENAI_API_KEY is not set; action items require embeddings and an LLM call")
		}

		actingID, ok := ActingUserID(ctx)
		if !ok {
			return nil, getSessionActionItemsOutput{}, mcpToolErrCode(mcpErrCodeActingUserNotConfigured, 403, "acting user not configured; set TALKBACK_MCP_ACTING_USER_ID to a TalkBack user UUID")
		}

		sessionID, err := uuid.Parse(strings.TrimSpace(in.SessionID))
		if err != nil {
			return nil, getSessionActionItemsOutput{}, mcpToolErr(400, "invalid session_id: must be a UUID")
		}
		logSessionID = sessionID.String()

		user, err := db.GetUserByID(ctx, actingID)
		if err != nil {
			return nil, getSessionActionItemsOutput{}, err
		}
		if user == nil {
			return nil, getSessionActionItemsOutput{}, mcpToolErr(403, "acting user not found in database")
		}

		session, err := db.GetSession(ctx, sessionID)
		if err != nil || session == nil {
			if err != nil && strings.Contains(err.Error(), "not found") {
				return nil, getSessionActionItemsOutput{}, mcpToolErr(404, "session not found")
			}
			if err != nil {
				return nil, getSessionActionItemsOutput{}, err
			}
			return nil, getSessionActionItemsOutput{}, mcpToolErr(404, "session not found")
		}

		if allowed, err := userMayReadSessionMCP(ctx, db, session, user); err != nil {
			return nil, getSessionActionItemsOutput{}, err
		} else if !allowed {
			return nil, getSessionActionItemsOutput{}, mcpToolErrCode(mcpErrCodeSessionAccessDenied, 403, "you do not have access to this session")
		}

		artifacts, err := db.GetArtifactsBySessionID(ctx, sessionID)
		if err != nil || len(artifacts) == 0 {
			return nil, getSessionActionItemsOutput{}, mcpToolErr(400, "session has no artifacts; create an artifact first")
		}

		embedder := &rag.OpenAIEmbedder{}
		if err := rag.EnsureSessionIndex(ctx, db, embedder, sessionID, store); err != nil {
			code, msg := mcpClassifyIndexError(err)
			return nil, getSessionActionItemsOutput{}, mcpToolErrCode(code, 503, msg)
		}

		videoSources, _ := db.GetVideoSourcesBySessionID(ctx, sessionID)
		primary, _ := resolveEffectivePrimaryAndAdditional(videoSources)
		var primaryVideoID *uuid.UUID
		if primary != nil {
			primaryVideoID = &primary.ID
		}

		if err := mcpAcquireSessionEmbeddingQuota(sessionID); err != nil {
			return nil, getSessionActionItemsOutput{}, err
		}

		q := actionItemsRetrievalQuery
		questionEmbedding, err := embedder.Embed(ctx, []string{q})
		if err != nil {
			code, st, msg := mcpClassifyEmbeddingError(err)
			return nil, getSessionActionItemsOutput{}, mcpToolErrCode(code, st, msg)
		}
		if len(questionEmbedding) == 0 || len(questionEmbedding[0]) == 0 {
			return nil, getSessionActionItemsOutput{}, mcpToolErrCode("embedding_empty_result", 503, "Embedding provider returned no vector; check model and input.")
		}

		sessionChunks, err := rag.RetrieveTopK(ctx, db, sessionID, questionEmbedding[0], actionItemsTopK, primaryVideoID)
		if err != nil {
			return nil, getSessionActionItemsOutput{}, mcpToolErrCode("retrieval_failed", 500, "Vector retrieval failed.")
		}
		if len(sessionChunks) == 0 && mcpSessionHasIndexableContent(ctx, db, sessionID) {
			_ = db.DeleteChunkEmbeddingsBySessionID(ctx, sessionID)
			_ = db.DeleteSessionChunksBySessionID(ctx, sessionID)
			if reindexErr := rag.IndexSession(ctx, db, embedder, sessionID, store); reindexErr == nil {
				sessionChunks, err = rag.RetrieveTopK(ctx, db, sessionID, questionEmbedding[0], actionItemsTopK, primaryVideoID)
				if err != nil {
					return nil, getSessionActionItemsOutput{}, mcpToolErrCode("retrieval_failed", 500, "Vector retrieval failed.")
				}
			}
		}

		chunks := mcpSessionChunksToUtilsChunks(sessionChunks)
		sctx := utils.SessionContext{
			Premise:         session.Premise,
			PrimaryDecision: session.PrimaryDecision,
		}

		ext, err := utils.ExtractActionItemsFromContext(ctx, chunks, session.Title, sctx)
		if err != nil {
			return nil, getSessionActionItemsOutput{}, mcpToolErr(500, "failed to extract action items: "+err.Error())
		}

		out := getSessionActionItemsOutput{
			SchemaVersion: actionItemsSchemaVersion,
			SessionID:     session.ID.String(),
			LowSignal:     ext.LowSignal,
		}
		if len(chunks) > 0 {
			out.LLMModel = "gpt-4o-mini"
		}
		for _, it := range ext.ActionItems {
			out.ActionItems = append(out.ActionItems, sessionActionItemOut{
				Description: it.Description,
				Owner:       it.Owner,
			})
		}
		if len(chunks) == 0 {
			out.LowSignal = true
			out.Note = "No indexed content was retrieved for this session; action items require transcript or material text in the index."
		} else if out.LowSignal && len(out.ActionItems) == 0 {
			out.Note = "No clear action items were found in the available content (low_signal)."
		}
		return nil, out, nil
	})
}
