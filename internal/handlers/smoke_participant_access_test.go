package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSmoke_ParticipantAccess_CanReadSessionAndArtifacts covers Flow 4: participant view access.
// Participant can read a session and its artifacts list.
func TestSmoke_ParticipantAccess_CanReadSessionAndArtifacts(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	session := createTestSessionForHandlers(t, h.DB, "Participant Access Smoke")
	// Add an artifact so the artifacts list is non-trivial.
	_, err := h.DB.CreateArtifact(context.Background(), session.ID, "Smoke Artifact", nil)
	require.NoError(t, err)

	// --- GET /sessions/{id} ---
	getReq := httptest.NewRequest(http.MethodGet, "/sessions/"+session.ID.String(), nil)
	getW := httptest.NewRecorder()
	h.GetSession(getW, getReq)

	require.Equal(t, http.StatusOK, getW.Code, getW.Body.String())
	var sessionResp GetSessionResponse
	require.NoError(t, json.NewDecoder(getW.Body).Decode(&sessionResp))
	require.NotNil(t, sessionResp.Session)
	assert.Equal(t, session.ID, sessionResp.Session.ID)

	// --- GET /sessions/{id}/artifacts ---
	artReq := httptest.NewRequest(http.MethodGet, "/sessions/"+session.ID.String()+"/artifacts", nil)
	artW := httptest.NewRecorder()
	h.GetArtifactsBySession(artW, artReq)

	require.Equal(t, http.StatusOK, artW.Code, artW.Body.String())
	var artResp map[string]any
	require.NoError(t, json.NewDecoder(artW.Body).Decode(&artResp))
	artifacts, _ := artResp["artifacts"].([]any)
	assert.Len(t, artifacts, 1)
}

// TestSmoke_ParticipantAccess_CannotCreateSession verifies that an authenticated participant
// gets 403 when attempting to create a session.
func TestSmoke_ParticipantAccess_CannotCreateSession(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	req := httptest.NewRequest(http.MethodPost, "/sessions", bytes.NewReader([]byte(`{"title":"Unauthorized"}`)))
	req.Header.Set("Content-Type", "application/json")
	addParticipantSessionCookie(t, h, req)
	w := httptest.NewRecorder()
	h.RequireAuth(h.CreateSession)(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
}

// TestSmoke_ParticipantAccess_AdminCanDeleteSession verifies that an admin can delete a session.
func TestSmoke_ParticipantAccess_AdminCanDeleteSession(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	session := createTestSessionForHandlers(t, h.DB, "Delete Smoke Session")

	req := httptest.NewRequest(http.MethodDelete, "/api/sessions/"+session.ID.String(), nil)
	req.URL.Path = "/api/sessions/" + session.ID.String()
	addAdminSessionCookie(t, h, req)
	w := httptest.NewRecorder()
	h.RequireAuth(h.RequireAdmin(h.DeleteSession))(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)

	// Confirm gone from DB.
	got, _ := h.DB.GetSession(context.Background(), session.ID)
	assert.Nil(t, got)
}

