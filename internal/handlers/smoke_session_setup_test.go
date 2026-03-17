package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSmoke_SessionSetup_CreatorCanCreateAndRetrieve covers Flow 1: admin/session setup.
// Creator authenticates → creates session → retrieves it back.
func TestSmoke_SessionSetup_CreatorCanCreateAndRetrieve(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	// --- Step 1: Create session as a creator ---
	body, _ := json.Marshal(map[string]any{"title": "Q4 Planning"})
	req := httptest.NewRequest(http.MethodPost, "/sessions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	creator := addUserSessionCookie(t, h, req, "creator@smoke.test")
	w := httptest.NewRecorder()
	h.RequireAuth(h.CreateSession)(w, req)

	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	var created CreateSessionResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&created))
	assert.NotEmpty(t, created.ID)
	assert.Equal(t, "Q4 Planning", created.Title)
	assert.Equal(t, "open", created.Status)
	require.NotNil(t, created.CreatedBy)
	assert.Equal(t, creator.Email, *created.CreatedBy)

	// --- Step 2: Retrieve session ---
	req2 := httptest.NewRequest(http.MethodGet, "/sessions/"+created.ID, nil)
	w2 := httptest.NewRecorder()
	h.GetSession(w2, req2)

	require.Equal(t, http.StatusOK, w2.Code, w2.Body.String())
	var fetched GetSessionResponse
	require.NoError(t, json.NewDecoder(w2.Body).Decode(&fetched))
	require.NotNil(t, fetched.Session)
	assert.Equal(t, created.ID, fetched.Session.ID.String())
	assert.Equal(t, "Q4 Planning", fetched.Session.Title)
}

// TestSmoke_SessionSetup_ParticipantCannotCreateSession verifies that participants are
// blocked from creating sessions with 403.
func TestSmoke_SessionSetup_ParticipantCannotCreateSession(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	body, _ := json.Marshal(map[string]any{"title": "Unauthorized Session"})
	req := httptest.NewRequest(http.MethodPost, "/sessions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	addParticipantSessionCookie(t, h, req)
	w := httptest.NewRecorder()
	h.RequireAuth(h.CreateSession)(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

// TestSmoke_SessionSetup_UnauthenticatedCannotCreateSession verifies 401 without auth.
func TestSmoke_SessionSetup_UnauthenticatedCannotCreateSession(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	body, _ := json.Marshal(map[string]any{"title": "No Auth Session"})
	req := httptest.NewRequest(http.MethodPost, "/sessions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.RequireAuth(h.CreateSession)(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// TestSmoke_SessionSetup_NonExistentSessionReturns404 verifies clean 404 for missing sessions.
func TestSmoke_SessionSetup_NonExistentSessionReturns404(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodGet, "/sessions/00000000-0000-0000-0000-000000000099", nil)
	w := httptest.NewRecorder()
	h.GetSession(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}
