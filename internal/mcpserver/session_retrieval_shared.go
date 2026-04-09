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

// mcpRunVectorRetrieval runs EnsureSessionIndex, embeds the query, and returns ranked chunks with scores (same stack as search_session_content).
func mcpRunVectorRetrieval(ctx context.Context, db *database.DB, sessionID uuid.UUID, query string, topK int) ([]rag.RetrievedChunk, string, *uuid.UUID, string, error) {
	if topK <= 0 {
		topK = maxSearchTopKDefault
	}
	if topK > maxSearchTopKCap {
		topK = maxSearchTopKCap
	}

	q := strings.TrimSpace(query)
	embedder := &rag.OpenAIEmbedder{}
	if err := rag.EnsureSessionIndex(ctx, db, embedder, sessionID, nil); err != nil {
		return nil, "", nil, "", mcpToolErr(503, "index unavailable: "+err.Error())
	}

	embeddings, err := embedder.Embed(ctx, []string{q})
	if err != nil {
		return nil, "", nil, "", mcpToolErr(503, "embedding failed: "+err.Error())
	}
	if len(embeddings) == 0 || len(embeddings[0]) == 0 {
		return nil, "", nil, "", mcpToolErr(503, "embedding unavailable (empty vector)")
	}

	primaryVID, err := primaryVideoIDForRetrieval(ctx, db, sessionID)
	if err != nil {
		return nil, "", nil, "", err
	}

	scored, err := rag.RetrieveTopKWithScores(ctx, db, sessionID, embeddings[0], topK, primaryVID)
	if err != nil {
		return nil, "", nil, "", mcpToolErr(500, "retrieval failed: "+err.Error())
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
