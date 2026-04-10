// Shared session-scoped vector retrieval for MCP tools (SCRUM-43 search, SCRUM-45 raw context).
// No LLM synthesis — only query embedding + rag.RetrieveTopKWithScores.
package mcpserver

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/database"
	"github.com/psuthar/talkback/internal/models"
	"github.com/psuthar/talkback/internal/rag"
	"github.com/psuthar/talkback/internal/storage"
)

const (
	maxSearchSnippetRunes  = 2000
	maxSearchTopKDefault   = 10
	maxSearchTopKCap       = 50
	maxRetrievalChunkRunes = 12000
)

// mcpLoadSessionWithReadAccess loads the session and verifies the acting user may read it (same rules as HTTP).
func mcpLoadSessionWithReadAccess(ctx context.Context, db *database.DB, sessionID uuid.UUID, actingID uuid.UUID) (*models.Session, *models.User, error) {
	user, err := db.GetUserByID(ctx, actingID)
	if err != nil {
		return nil, nil, err
	}
	if user == nil {
		return nil, nil, mcpToolErr(403, "acting user not found in database")
	}

	session, err := db.GetSession(ctx, sessionID)
	if err != nil || session == nil {
		if err != nil && strings.Contains(err.Error(), "not found") {
			return nil, nil, mcpToolErr(404, "session not found")
		}
		if err != nil {
			return nil, nil, err
		}
		return nil, nil, mcpToolErr(404, "session not found")
	}

	if allowed, err := userMayReadSessionMCP(ctx, db, session, user); err != nil {
		return nil, nil, err
	} else if !allowed {
		return nil, nil, mcpToolErr(403, "you do not have access to this session")
	}
	return session, user, nil
}

// mcpRunVectorRetrieval runs EnsureSessionIndex, embeds the query, and returns ranked chunks with scores (same stack as search_session / search_session_content).
// store is passed through to EnsureSessionIndex (optional R2 PDF chunking — parity with HTTP SessionAsk).
func mcpRunVectorRetrieval(ctx context.Context, db *database.DB, sessionID uuid.UUID, query string, topK int, store storage.Interface) ([]rag.RetrievedChunk, string, *uuid.UUID, string, error) {
	if topK <= 0 {
		topK = maxSearchTopKDefault
	}
	if topK > maxSearchTopKCap {
		topK = maxSearchTopKCap
	}

	q := strings.TrimSpace(query)
	embedder := &rag.OpenAIEmbedder{}
	if err := rag.EnsureSessionIndex(ctx, db, embedder, sessionID, store); err != nil {
		code, msg := mcpClassifyIndexError(err)
		return nil, "", nil, "", mcpToolErrCode(code, 503, msg)
	}

	if err := mcpAcquireSessionEmbeddingQuota(sessionID); err != nil {
		return nil, "", nil, "", err
	}

	embeddings, err := embedder.Embed(ctx, []string{q})
	if err != nil {
		code, st, msg := mcpClassifyEmbeddingError(err)
		return nil, "", nil, "", mcpToolErrCode(code, st, msg)
	}
	if len(embeddings) == 0 || len(embeddings[0]) == 0 {
		return nil, "", nil, "", mcpToolErrCode("embedding_empty_result", 503, "Embedding provider returned no vector; check model and input.")
	}

	primaryVID, err := primaryVideoIDForRetrieval(ctx, db, sessionID)
	if err != nil {
		return nil, "", nil, "", err
	}

	scored, err := rag.RetrieveTopKWithScores(ctx, db, sessionID, embeddings[0], topK, primaryVID)
	if err != nil {
		return nil, "", nil, "", mcpToolErrCode("retrieval_failed", 500, "Vector retrieval failed.")
	}
	return scored, q, primaryVID, embedder.ModelName(), nil
}

func truncateSearchSnippet(s string) string {
	if utf8.RuneCountInString(s) <= maxSearchSnippetRunes {
		return s
	}
	r := []rune(s)
	return string(r[:maxSearchSnippetRunes]) + "…"
}

func truncateRetrievalChunkText(s string) string {
	if utf8.RuneCountInString(s) <= maxRetrievalChunkRunes {
		return s
	}
	r := []rune(s)
	return string(r[:maxRetrievalChunkRunes]) + "…"
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
