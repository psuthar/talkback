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

// seedCreatorAndSession creates a creator user with login session and a session owned by them.
// Returns the creator, the login session, and the session.
func seedCreatorAndSession(t *testing.T, h *Handlers, suffix string) (*models.User, *models.LoginSession, *models.Session) {
	t.Helper()
	ctx := context.Background()
	email := "creator+" + suffix + "@smoke.test"
	creator := &models.User{
		ID:          uuid.New(),
		Email:       email,
		DisplayName: "Smoke Creator " + suffix,
		Status:      models.UserStatusActive,
		GlobalRole:  models.GlobalRoleCreator,
	}
	require.NoError(t, h.DB.CreateUser(ctx, creator))
	ls := &models.LoginSession{
		ID:        uuid.New(),
		UserID:    creator.ID,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}
	require.NoError(t, h.DB.CreateLoginSession(ctx, ls))
	sess := &models.Session{
		ID:        uuid.New(),
		Title:     "Smoke Session " + suffix,
		CreatedBy: &email,
		Status:    models.SessionStatusOpen,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	require.NoError(t, h.DB.CreateSession(ctx, sess))
	return creator, ls, sess
}

// TestSmoke_SessionUpdate_CreatorCanUpdateTitleAndDecisionFields verifies that a creator can
// update title, premise, primary_decision, and decision_outcome via PATCH /sessions/{id}.
func TestSmoke_SessionUpdate_CreatorCanUpdateTitleAndDecisionFields(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	_, ls, sess := seedCreatorAndSession(t, h, "update-decision")

	body, _ := json.Marshal(map[string]any{
		"title":            "Revised Title",
		"premise":          "We need to decide on the APAC expansion.",
		"primary_decision": "Approve 2.4M budget for APAC.",
	})
	req := httptest.NewRequest(http.MethodPatch, "/sessions/"+sess.ID.String(), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: auth.Config.SessionCookieName, Value: ls.ID.String()})
	w := httptest.NewRecorder()
	h.RequireAuth(h.UpdateSessionStatus)(w, req)

	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var updated models.Session
	require.NoError(t, json.NewDecoder(w.Body).Decode(&updated))
	assert.Equal(t, "Revised Title", updated.Title)
	require.NotNil(t, updated.Premise)
	assert.Equal(t, "We need to decide on the APAC expansion.", *updated.Premise)
	require.NotNil(t, updated.PrimaryDecision)
	assert.Equal(t, "Approve 2.4M budget for APAC.", *updated.PrimaryDecision)

	// Verify persisted in DB.
	dbSess, err := h.DB.GetSession(context.Background(), sess.ID)
	require.NoError(t, err)
	require.NotNil(t, dbSess)
	assert.Equal(t, "Revised Title", dbSess.Title)
	assert.NotNil(t, dbSess.Premise)
	assert.NotNil(t, dbSess.PrimaryDecision)
}

// TestSmoke_SessionUpdate_CreatorCanSetDecisionOutcome verifies that a creator can record
// a decision_outcome, transitioning the session toward resolved state.
func TestSmoke_SessionUpdate_CreatorCanSetDecisionOutcome(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	_, ls, sess := seedCreatorAndSession(t, h, "set-outcome")

	// First set a primary_decision so outcome is meaningful.
	body1, _ := json.Marshal(map[string]any{"primary_decision": "Approve APAC budget."})
	req1 := httptest.NewRequest(http.MethodPatch, "/sessions/"+sess.ID.String(), bytes.NewReader(body1))
	req1.Header.Set("Content-Type", "application/json")
	req1.AddCookie(&http.Cookie{Name: auth.Config.SessionCookieName, Value: ls.ID.String()})
	w1 := httptest.NewRecorder()
	h.RequireAuth(h.UpdateSessionStatus)(w1, req1)
	require.Equal(t, http.StatusOK, w1.Code, w1.Body.String())

	// Now record the outcome.
	body2, _ := json.Marshal(map[string]any{"decision_outcome": "Approved at board meeting."})
	req2 := httptest.NewRequest(http.MethodPatch, "/sessions/"+sess.ID.String(), bytes.NewReader(body2))
	req2.Header.Set("Content-Type", "application/json")
	req2.AddCookie(&http.Cookie{Name: auth.Config.SessionCookieName, Value: ls.ID.String()})
	w2 := httptest.NewRecorder()
	h.RequireAuth(h.UpdateSessionStatus)(w2, req2)

	require.Equal(t, http.StatusOK, w2.Code, w2.Body.String())
	var updated models.Session
	require.NoError(t, json.NewDecoder(w2.Body).Decode(&updated))
	require.NotNil(t, updated.DecisionOutcome)
	assert.Equal(t, "Approved at board meeting.", *updated.DecisionOutcome)
}

// TestSmoke_SessionUpdate_NonCreatorForbidden verifies that a creator cannot update another
// creator's session — returns 403.
func TestSmoke_SessionUpdate_NonCreatorForbidden(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	// Session is owned by someone else.
	_, _, sess := seedCreatorAndSession(t, h, "update-owner")

	// Different creator tries to update it.
	req := httptest.NewRequest(http.MethodPatch, "/sessions/"+sess.ID.String(), bytes.NewReader([]byte(`{"title":"Stolen"}`)))
	req.Header.Set("Content-Type", "application/json")
	addUserSessionCookie(t, h, req, "intruder+update@smoke.test")
	w := httptest.NewRecorder()
	h.RequireAuth(h.UpdateSessionStatus)(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

// TestSmoke_SessionUpdate_UnauthenticatedForbidden verifies 401 without a session cookie.
func TestSmoke_SessionUpdate_UnauthenticatedForbidden(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	_, _, sess := seedCreatorAndSession(t, h, "update-unauth")

	req := httptest.NewRequest(http.MethodPatch, "/sessions/"+sess.ID.String(), bytes.NewReader([]byte(`{"title":"Sneaky"}`)))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.RequireAuth(h.UpdateSessionStatus)(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// TestSmoke_SessionUpdate_DuplicateTitleConflicts verifies that updating a session's title to
// one already used by another of the same creator's sessions returns 409.
func TestSmoke_SessionUpdate_DuplicateTitleConflicts(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	ctx := context.Background()
	creator, ls, sess1 := seedCreatorAndSession(t, h, "title-conflict-1")

	// Create a second session with a distinct title.
	sess2 := &models.Session{
		ID:        uuid.New(),
		Title:     "Unique Second Session",
		CreatedBy: &creator.Email,
		Status:    models.SessionStatusOpen,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	require.NoError(t, h.DB.CreateSession(ctx, sess2))

	// Try to rename sess2 to sess1's title — should conflict.
	body, _ := json.Marshal(map[string]any{"title": sess1.Title})
	req := httptest.NewRequest(http.MethodPatch, "/sessions/"+sess2.ID.String(), bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(&http.Cookie{Name: auth.Config.SessionCookieName, Value: ls.ID.String()})
	w := httptest.NewRecorder()
	h.RequireAuth(h.UpdateSessionStatus)(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
}
