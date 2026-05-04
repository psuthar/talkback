package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/database"
	"github.com/psuthar/talkback/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// SCRUM-297 — stale not_covered answers should auto-retry once the session's
// index has advanced past the cached answer's created_at. The retry updates the
// answer row in place so the answer ID remains stable for clients.
func TestSessionAsk_StaleNotCoveredAutoRetries(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	db := h.DB
	ctx := context.Background()

	session := createTestSessionForHandlers(t, db, "SCRUM-297 stale retry")
	artifact, err := db.CreateArtifact(ctx, session.ID, "Artifact", nil)
	require.NoError(t, err)

	// Seed a question with a stale, cached not_covered answer.
	const questionText = "What does Jerome do?"
	question := &models.Question{
		ID:             uuid.New(),
		ArtifactID:     artifact.ID,
		SessionID:      session.ID,
		QuestionText:   questionText,
		QuestionSource: models.QuestionSourceText,
	}
	require.NoError(t, db.CreateQuestion(ctx, question))

	const staleAnswerText = "STALE_NOT_COVERED_TEXT_DO_NOT_RETURN"
	cachedAnswer := &models.Answer{
		ID:           uuid.New(),
		QuestionID:   question.ID,
		AnswerText:   staleAnswerText,
		AnswerStatus: models.AnswerStatusNotCovered,
		Confidence:   0,
		Citations:    []models.Citation{},
	}
	require.NoError(t, db.CreateAnswer(ctx, cachedAnswer))

	// Backdate the cached answer's created_at so we can simulate the index
	// advancing afterward.
	pastTime := time.Now().Add(-1 * time.Hour)
	_, err = db.Pool.Exec(ctx, `UPDATE answers SET created_at = $1 WHERE id = $2`, pastTime, cachedAnswer.ID)
	require.NoError(t, err)

	// Advance the session's index_updated_at to a time after the cached answer.
	indexAdvancedAt := pastTime.Add(30 * time.Minute)
	_, err = db.Pool.Exec(ctx, `UPDATE sessions SET index_updated_at = $1, index_status = 'ready' WHERE id = $2`, indexAdvancedAt, session.ID)
	require.NoError(t, err)

	// Re-ask the same question. Without OPENAI_API_KEY the retry path falls
	// through to the empty-chunks branch, which returns the canonical
	// "not_covered" message — different from the seeded staleAnswerText. The
	// observable contract is: same answer ID, but updated content.
	body, _ := json.Marshal(map[string]string{"question_text": questionText})
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/"+session.ID.String()+"/ask", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.SessionAsk(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp SessionAskResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))

	// Same answer ID — the retry updated in place rather than creating a new row.
	assert.Equal(t, cachedAnswer.ID.String(), resp.Answer.ID, "answer ID should be preserved on stale-retry")

	// Stale text replaced by a fresh not_covered response.
	assert.NotEqual(t, staleAnswerText, resp.Answer.AnswerText, "stale text should be replaced by fresh response")

	// Persisted state matches what the response advertises.
	updated, err := db.GetAnswerByID(ctx, cachedAnswer.ID)
	require.NoError(t, err)
	require.NotNil(t, updated)
	assert.NotEqual(t, staleAnswerText, updated.AnswerText, "DB row should be updated in place")
	assert.True(t, updated.CreatedAt.After(pastTime), "created_at should be bumped to retry time")

	// No new question row created.
	count, err := db.CountQuestionsBySessionID(ctx, session.ID)
	require.NoError(t, err)
	assert.Equal(t, 1, count, "stale-retry should not create a new question row")
}

// SCRUM-297 — when the cached answer is "answered" (not not_covered), the
// dedupe path should still serve it as before, even if the index has advanced.
func TestSessionAsk_CachedAnsweredStillServedAfterIndexAdvance(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	db := h.DB
	ctx := context.Background()

	session := createTestSessionForHandlers(t, db, "SCRUM-297 cached answered")
	artifact, err := db.CreateArtifact(ctx, session.ID, "Artifact", nil)
	require.NoError(t, err)

	const questionText = "Who is on the team?"
	question := &models.Question{
		ID:             uuid.New(),
		ArtifactID:     artifact.ID,
		SessionID:      session.ID,
		QuestionText:   questionText,
		QuestionSource: models.QuestionSourceText,
	}
	require.NoError(t, db.CreateQuestion(ctx, question))

	const stableAnswerText = "Alice and Bob"
	cachedAnswer := &models.Answer{
		ID:           uuid.New(),
		QuestionID:   question.ID,
		AnswerText:   stableAnswerText,
		AnswerStatus: models.AnswerStatusAnswered,
		Confidence:   0.9,
		Citations:    []models.Citation{},
	}
	require.NoError(t, db.CreateAnswer(ctx, cachedAnswer))

	// Backdate the answer and advance index_updated_at past it.
	pastTime := time.Now().Add(-1 * time.Hour)
	_, err = db.Pool.Exec(ctx, `UPDATE answers SET created_at = $1 WHERE id = $2`, pastTime, cachedAnswer.ID)
	require.NoError(t, err)
	_, err = db.Pool.Exec(ctx, `UPDATE sessions SET index_updated_at = $1, index_status = 'ready' WHERE id = $2`, pastTime.Add(30*time.Minute), session.ID)
	require.NoError(t, err)

	body, _ := json.Marshal(map[string]string{"question_text": questionText})
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/"+session.ID.String()+"/ask", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.SessionAsk(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp SessionAskResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))

	// Cached answered response is preserved; only not_covered triggers retry.
	assert.Equal(t, cachedAnswer.ID.String(), resp.Answer.ID)
	assert.Equal(t, stableAnswerText, resp.Answer.AnswerText, "answered cache should not be invalidated by index advance")
}

// SCRUM-297 — cached not_covered without index advance is still served from
// cache (no retry). The retry only kicks in when the index has advanced past
// the cached answer's created_at.
func TestSessionAsk_CachedNotCoveredServedWhenIndexNotAdvanced(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	db := h.DB
	ctx := context.Background()

	session := createTestSessionForHandlers(t, db, "SCRUM-297 cache fresh")
	artifact, err := db.CreateArtifact(ctx, session.ID, "Artifact", nil)
	require.NoError(t, err)

	const questionText = "Anything?"
	question := &models.Question{
		ID:             uuid.New(),
		ArtifactID:     artifact.ID,
		SessionID:      session.ID,
		QuestionText:   questionText,
		QuestionSource: models.QuestionSourceText,
	}
	require.NoError(t, db.CreateQuestion(ctx, question))

	const cachedText = "FRESH_CACHED_NOT_COVERED"
	cachedAnswer := &models.Answer{
		ID:           uuid.New(),
		QuestionID:   question.ID,
		AnswerText:   cachedText,
		AnswerStatus: models.AnswerStatusNotCovered,
		Confidence:   0,
		Citations:    []models.Citation{},
	}
	require.NoError(t, db.CreateAnswer(ctx, cachedAnswer))

	// session.index_updated_at remains nil (never indexed) — should serve cache.

	body, _ := json.Marshal(map[string]string{"question_text": questionText})
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/"+session.ID.String()+"/ask", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.SessionAsk(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp SessionAskResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, cachedAnswer.ID.String(), resp.Answer.ID)
	assert.Equal(t, cachedText, resp.Answer.AnswerText, "cache should be served when index has not advanced")
}

// SCRUM-297 — sessionHasUnindexedSources detects the partial-index race: a
// verified link or active material with extracted_text but no rows in
// session_chunks for that source.
func TestSessionHasUnindexedSources_DetectsPartialIndex(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	db := h.DB
	ctx := context.Background()

	session := createTestSessionForHandlers(t, db, "SCRUM-297 partial index")

	// No links, no materials → false.
	assert.False(t, sessionHasUnindexedSources(ctx, db, session.ID), "empty session should not be flagged")

	// Add a verified link with extracted_text but no chunks → true.
	extracted := "I solve really tough business problems"
	link := &models.SessionLink{
		ID:            uuid.New(),
		SessionID:     session.ID,
		URL:           "https://example.com/jerome",
		Status:        models.SessionLinkStatusVerified,
		ExtractedText: &extracted,
	}
	require.NoError(t, db.CreateSessionLink(ctx, link))
	assert.True(t, sessionHasUnindexedSources(ctx, db, session.ID), "verified link with no chunks should be flagged")

	// Add a chunk for that link → false again (link is now indexed).
	_, err := db.UpsertSessionChunk(ctx, database.SessionChunkInsert{
		SessionID:   session.ID,
		SourceType:  "link",
		SourceID:    &link.ID,
		ChunkIdx:    0,
		Text:        extracted,
		ContentHash: "test-hash-1",
	})
	require.NoError(t, err)
	assert.False(t, sessionHasUnindexedSources(ctx, db, session.ID), "link with chunks should not be flagged")
}
