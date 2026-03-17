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

// TestSmoke_AccessControl_CreatorForbiddenFromAdminDelete verifies that an authenticated
// creator (non-admin) gets 403 when attempting to delete a session via the admin-only endpoint.
func TestSmoke_AccessControl_CreatorForbiddenFromAdminDelete(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	session := createTestSessionForHandlers(t, h.DB, "Admin Only Session")

	req := httptest.NewRequest(http.MethodDelete, "/api/sessions/"+session.ID.String(), nil)
	req.URL.Path = "/api/sessions/" + session.ID.String()
	addUserSessionCookie(t, h, req, "creator+admintest@smoke.test")
	w := httptest.NewRecorder()
	h.RequireAuth(h.RequireAdmin(h.DeleteSession))(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

// TestSmoke_AccessControl_ParticipantForbiddenFromAdminDelete verifies that a participant
// also gets 403 (not 401) when hitting the admin-only delete endpoint.
func TestSmoke_AccessControl_ParticipantForbiddenFromAdminDelete(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	session := createTestSessionForHandlers(t, h.DB, "Admin Only Session Participant")

	req := httptest.NewRequest(http.MethodDelete, "/api/sessions/"+session.ID.String(), nil)
	req.URL.Path = "/api/sessions/" + session.ID.String()
	addParticipantSessionCookie(t, h, req)
	w := httptest.NewRecorder()
	h.RequireAuth(h.RequireAdmin(h.DeleteSession))(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

// TestSmoke_AccessControl_ListSessionsCreatorScopeIsolation verifies that a creator's
// GET /api/sessions returns only sessions they created, not sessions owned by others.
func TestSmoke_AccessControl_ListSessionsCreatorScopeIsolation(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	ctx := context.Background()
	callerEmail := "caller+scopetest@smoke.test"
	otherEmail := "other+scopetest@smoke.test"

	// Create two users.
	caller := &models.User{
		ID:          uuid.New(),
		Email:       callerEmail,
		DisplayName: "Caller",
		Status:      models.UserStatusActive,
		GlobalRole:  models.GlobalRoleCreator,
	}
	require.NoError(t, h.DB.CreateUser(ctx, caller))
	callerLoginSess := &models.LoginSession{
		ID:        uuid.New(),
		UserID:    caller.ID,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}
	require.NoError(t, h.DB.CreateLoginSession(ctx, callerLoginSess))

	// Session owned by caller.
	mySession := &models.Session{
		ID:        uuid.New(),
		Title:     "Caller Owned Session",
		CreatedBy: &callerEmail,
		Status:    models.SessionStatusOpen,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	require.NoError(t, h.DB.CreateSession(ctx, mySession))

	// Session owned by someone else — must not appear in caller's list.
	otherSession := &models.Session{
		ID:        uuid.New(),
		Title:     "Other Owned Session",
		CreatedBy: &otherEmail,
		Status:    models.SessionStatusOpen,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	require.NoError(t, h.DB.CreateSession(ctx, otherSession))

	req := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	req.URL.Path = "/api/sessions"
	req.AddCookie(&http.Cookie{Name: auth.Config.SessionCookieName, Value: callerLoginSess.ID.String()})
	w := httptest.NewRecorder()
	h.RequireAuth(h.ListSessions)(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp []SessionWithRole
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))

	// Caller should see only their own session.
	var seenIDs []string
	for _, sr := range resp {
		seenIDs = append(seenIDs, sr.Session.ID.String())
	}
	assert.Contains(t, seenIDs, mySession.ID.String(), "caller's session must appear in list")
	assert.NotContains(t, seenIDs, otherSession.ID.String(), "other creator's session must not appear")
}

// TestSmoke_AccessControl_ListSessionsAdminSeesAll verifies that an admin's GET /api/sessions
// includes sessions owned by other creators.
func TestSmoke_AccessControl_ListSessionsAdminSeesAll(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	ctx := context.Background()
	otherEmail := "other+adminlisttest@smoke.test"

	// Session owned by someone else.
	otherSession := &models.Session{
		ID:        uuid.New(),
		Title:     "Other Creator Session Admin List",
		CreatedBy: &otherEmail,
		Status:    models.SessionStatusOpen,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	require.NoError(t, h.DB.CreateSession(ctx, otherSession))

	req := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	req.URL.Path = "/api/sessions"
	addAdminSessionCookie(t, h, req)
	w := httptest.NewRecorder()
	h.RequireAuth(h.ListSessions)(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp []SessionWithRole
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))

	var seenIDs []string
	for _, sr := range resp {
		seenIDs = append(seenIDs, sr.Session.ID.String())
	}
	assert.Contains(t, seenIDs, otherSession.ID.String(), "admin must see sessions owned by other creators")
}

// TestSmoke_AccessControl_CrossSessionQuestionIsolation verifies that
// GET /sessions/{id}/questions only returns questions for that specific session,
// even when another session has questions seeded.
func TestSmoke_AccessControl_CrossSessionQuestionIsolation(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	ctx := context.Background()

	// Session A — target.
	sessionA := createTestSessionForHandlers(t, h.DB, "Cross Session Isolation A")
	artifactA, err := h.DB.CreateArtifact(ctx, sessionA.ID, "Artifact A", nil)
	require.NoError(t, err)

	qA := &models.Question{
		ID:             uuid.New(),
		ArtifactID:     artifactA.ID,
		SessionID:      sessionA.ID,
		QuestionText:   "Question in session A?",
		QuestionSource: models.QuestionSourceText,
	}
	require.NoError(t, h.DB.CreateQuestion(ctx, qA))

	// Session B — must not bleed into session A's endpoint.
	sessionB := createTestSessionForHandlers(t, h.DB, "Cross Session Isolation B")
	artifactB, err := h.DB.CreateArtifact(ctx, sessionB.ID, "Artifact B", nil)
	require.NoError(t, err)

	qB := &models.Question{
		ID:             uuid.New(),
		ArtifactID:     artifactB.ID,
		SessionID:      sessionB.ID,
		QuestionText:   "Question in session B?",
		QuestionSource: models.QuestionSourceText,
	}
	require.NoError(t, h.DB.CreateQuestion(ctx, qB))

	// Call GET /sessions/{sessionA}/questions — must not return session B's question.
	req := httptest.NewRequest(http.MethodGet, "/sessions/"+sessionA.ID.String()+"/questions", nil)
	w := httptest.NewRecorder()
	h.GetSessionQuestions(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp GetQuestionsResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))

	require.Len(t, resp.Questions, 1, "should return exactly 1 question for session A")
	assert.Equal(t, qA.ID, resp.Questions[0].ID, "returned question must belong to session A")
	assert.Equal(t, sessionA.ID, resp.Questions[0].SessionID)

	for _, q := range resp.Questions {
		assert.NotEqual(t, qB.ID, q.ID, "session B's question must not appear in session A's list")
	}
}

// TestSmoke_AccessControl_DeleteMaterialSoftDeletes verifies that a creator can delete a
// session material and that the material no longer appears in the active list.
func TestSmoke_AccessControl_DeleteMaterialSoftDeletes(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	ctx := context.Background()
	session := createTestSessionForHandlers(t, h.DB, "Delete Material Session")

	// Paste a material so we have a real material to delete.
	pasteBody, _ := json.Marshal(map[string]any{
		"title": "To Be Deleted",
		"text":  "Some text content for deletion test.",
	})
	pasteReq := httptest.NewRequest(http.MethodPost, "/sessions/"+session.ID.String()+"/materials/paste", bytes.NewReader(pasteBody))
	pasteReq.Header.Set("Content-Type", "application/json")
	pasteW := httptest.NewRecorder()
	h.SessionPasteMaterial(pasteW, pasteReq)
	require.Equal(t, http.StatusCreated, pasteW.Code, pasteW.Body.String())

	var mat map[string]any
	require.NoError(t, json.NewDecoder(pasteW.Body).Decode(&mat))
	materialID, ok := mat["id"].(string)
	require.True(t, ok, "expected string id in paste response")
	require.NotEmpty(t, materialID)

	// Confirm it appears in the active list.
	listReq1 := httptest.NewRequest(http.MethodGet, "/sessions/"+session.ID.String()+"/materials", nil)
	listW1 := httptest.NewRecorder()
	h.ListSessionMaterials(listW1, listReq1)
	require.Equal(t, http.StatusOK, listW1.Code)
	var listBefore []*models.Material
	require.NoError(t, json.NewDecoder(listW1.Body).Decode(&listBefore))
	require.Len(t, listBefore, 1)

	// Delete via handler.
	delReq := httptest.NewRequest(http.MethodDelete, "/sessions/"+session.ID.String()+"/materials/"+materialID, nil)
	delReq.URL.Path = "/sessions/" + session.ID.String() + "/materials/" + materialID
	delW := httptest.NewRecorder()
	h.DeleteSessionMaterial(delW, delReq)
	require.Equal(t, http.StatusNoContent, delW.Code, delW.Body.String())

	// Confirm it no longer appears in the active list.
	listReq2 := httptest.NewRequest(http.MethodGet, "/sessions/"+session.ID.String()+"/materials", nil)
	listW2 := httptest.NewRecorder()
	h.ListSessionMaterials(listW2, listReq2)
	require.Equal(t, http.StatusOK, listW2.Code)
	var listAfter []*models.Material
	require.NoError(t, json.NewDecoder(listW2.Body).Decode(&listAfter))
	assert.Len(t, listAfter, 0, "deleted material must not appear in active list")

	// Confirm soft-delete in DB — GetMaterialByID returns the row but DeletedAt is set.
	matUUID, err := uuid.Parse(materialID)
	require.NoError(t, err)
	dbMat, err := h.DB.GetMaterialByID(ctx, matUUID)
	require.NoError(t, err)
	require.NotNil(t, dbMat, "soft-deleted material row must still exist in DB")
	assert.NotNil(t, dbMat.DeletedAt, "soft-deleted material must have DeletedAt set")
}
