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
	"github.com/psuthar/talkback/internal/auth"
	"github.com/psuthar/talkback/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSmoke_EdgeCase_PasteToNonexistentSessionReturns404 verifies that posting a material paste
// to a session that does not exist returns 404 (not 500 or 201).
func TestSmoke_EdgeCase_PasteToNonexistentSessionReturns404(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	nonexistentID := uuid.New()
	body, _ := json.Marshal(map[string]any{"title": "Ghost Material", "text": "Some content."})
	req := httptest.NewRequest(http.MethodPost, "/sessions/"+nonexistentID.String()+"/materials/paste", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.SessionPasteMaterial(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

// TestSmoke_EdgeCase_PasteMissingTitleDefaultsToTitle verifies that paste with a blank title
// field (but valid text) is accepted and defaults the title to "Pasted text".
func TestSmoke_EdgeCase_PasteMissingTitleDefaultsToTitle(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	session := createTestSessionForHandlers(t, h.DB, "Paste No Title Session")

	body, _ := json.Marshal(map[string]any{"text": "Valid content without explicit title."})
	req := httptest.NewRequest(http.MethodPost, "/sessions/"+session.ID.String()+"/materials/paste", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.SessionPasteMaterial(w, req)

	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	var m map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&m))
	// Handler defaults blank title to "Pasted text".
	assert.Equal(t, "Pasted text", m["title"])
}

// TestSmoke_EdgeCase_AskQuestionEmptyTextReturns400 verifies that submitting a question with
// an empty question_text field is rejected with 400.
func TestSmoke_EdgeCase_AskQuestionEmptyTextReturns400(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	session := createTestSessionForHandlers(t, h.DB, "Ask Empty Q Session")
	artifact, err := h.DB.CreateArtifact(context.Background(), session.ID, "Empty Q Artifact", nil)
	require.NoError(t, err)

	body, _ := json.Marshal(AskQuestionRequest{QuestionText: ""})
	req := httptest.NewRequest(http.MethodPost, "/artifacts/"+artifact.ID.String()+"/questions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.AskQuestion(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestSmoke_EdgeCase_ResolveNonexistentTokenReturns400 verifies that GET /api/invitations/resolve
// with a random invalid token returns 400 (not 500 or 404).
func TestSmoke_EdgeCase_ResolveNonexistentTokenReturns400(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersWithInvitations(t)
	defer cleanup()

	bogusToken := "completely-invalid-token-that-does-not-exist"
	req := httptest.NewRequest(http.MethodGet, "/api/invitations/resolve?token="+bogusToken, nil)
	w := httptest.NewRecorder()
	h.ResolveInvitation(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestSmoke_EdgeCase_ResolveWithMissingTokenParamReturns400 verifies that calling the resolve
// endpoint with no token query parameter returns 400.
func TestSmoke_EdgeCase_ResolveWithMissingTokenParamReturns400(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersWithInvitations(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/api/invitations/resolve", nil)
	w := httptest.NewRecorder()
	h.ResolveInvitation(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestSmoke_EdgeCase_GetQuestionsForNonexistentArtifactReturnsEmpty verifies that
// GET /artifacts/{id}/questions for a nonexistent artifact returns 200 with empty arrays
// (not null), matching the handler's permissive design for non-existent artifacts.
func TestSmoke_EdgeCase_GetQuestionsForNonexistentArtifactReturnsEmpty(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	nonexistentID := uuid.New()
	req := httptest.NewRequest(http.MethodGet, "/artifacts/"+nonexistentID.String()+"/questions", nil)
	w := httptest.NewRecorder()
	h.GetQuestions(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp GetQuestionsResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))

	// Must be empty slices, not nil — so the frontend gets [] not null.
	assert.NotNil(t, resp.Questions)
	assert.NotNil(t, resp.Answers)
	assert.Len(t, resp.Questions, 0)
	assert.Len(t, resp.Answers, 0)
}

// TestSmoke_EdgeCase_ArtifactsResponseFieldsPresent verifies that
// GET /sessions/{id}/artifacts returns a well-structured response with "artifacts" key
// and each artifact has id, session_id, title, and status fields.
func TestSmoke_EdgeCase_ArtifactsResponseFieldsPresent(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	session := createTestSessionForHandlers(t, h.DB, "Artifacts Shape Session")
	_, err := h.DB.CreateArtifact(context.Background(), session.ID, "Shape Test Artifact", nil)
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodGet, "/sessions/"+session.ID.String()+"/artifacts", nil)
	w := httptest.NewRecorder()
	h.GetArtifactsBySession(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))

	artifacts, ok := resp["artifacts"].([]any)
	require.True(t, ok, "response must have 'artifacts' array key")
	require.Len(t, artifacts, 1)

	a, ok := artifacts[0].(map[string]any)
	require.True(t, ok)
	assert.NotEmpty(t, a["id"], "artifact must have id")
	assert.NotEmpty(t, a["session_id"], "artifact must have session_id")
	assert.NotEmpty(t, a["title"], "artifact must have title")
	assert.NotEmpty(t, a["status"], "artifact must have status")
}

// TestSmoke_EdgeCase_CreateSessionDuplicateTitleConflicts verifies that creating two sessions
// with the same title by the same creator returns 409 on the second attempt.
func TestSmoke_EdgeCase_CreateSessionDuplicateTitleConflicts(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	ctx := context.Background()
	const title = "Duplicate Title Session"
	const creatorEmail = "dup+creator@smoke.test"

	// First request: create the creator user and a login session, then create the session.
	body1, _ := json.Marshal(map[string]any{"title": title})
	req1 := httptest.NewRequest(http.MethodPost, "/sessions", bytes.NewReader(body1))
	req1.Header.Set("Content-Type", "application/json")
	addUserSessionCookie(t, h, req1, creatorEmail)
	w1 := httptest.NewRecorder()
	h.RequireAuth(h.CreateSession)(w1, req1)
	require.Equal(t, http.StatusCreated, w1.Code, w1.Body.String())

	// Second request: same creator, same title.  The user already exists; create a new
	// login session directly rather than calling addUserSessionCookie (which would try to
	// INSERT the same email a second time and fail on the unique constraint).
	existingUser, err := h.DB.GetUserByEmail(ctx, creatorEmail)
	require.NoError(t, err)
	require.NotNil(t, existingUser)

	ls2 := &models.LoginSession{
		ID:        uuid.New(),
		UserID:    existingUser.ID,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}
	require.NoError(t, h.DB.CreateLoginSession(ctx, ls2))

	body2, _ := json.Marshal(map[string]any{"title": title})
	req2 := httptest.NewRequest(http.MethodPost, "/sessions", bytes.NewReader(body2))
	req2.Header.Set("Content-Type", "application/json")
	req2.AddCookie(&http.Cookie{Name: auth.Config.SessionCookieName, Value: ls2.ID.String()})
	w2 := httptest.NewRecorder()
	h.RequireAuth(h.CreateSession)(w2, req2)

	assert.Equal(t, http.StatusConflict, w2.Code)
}
