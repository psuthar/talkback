package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/models"
	"github.com/stretchr/testify/require"
)

func TestGoogleMeetAPIStatus_FlagOff_ReturnsEnabledFalse(t *testing.T) {
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	t.Setenv("ENABLE_GOOGLE_MEET", "")

	req := httptest.NewRequest(http.MethodGet, "/api/google-meet/status", nil)
	w := httptest.NewRecorder()
	h.GoogleMeetAPIStatus(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var resp GoogleMeetStatusResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.False(t, resp.Enabled)
	require.False(t, resp.Connected)
}

func TestGoogleMeetAPIStatus_EnabledNoConnection(t *testing.T) {
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	t.Setenv("ENABLE_GOOGLE_MEET", "true")

	req := httptest.NewRequest(http.MethodGet, "/api/google-meet/status?creator_identity=alice@example.com", nil)
	w := httptest.NewRecorder()
	h.GoogleMeetAPIStatus(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var resp GoogleMeetStatusResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.True(t, resp.Enabled)
	require.False(t, resp.Connected)
}

func TestGoogleMeetAPIStatus_EnabledWithConnection(t *testing.T) {
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	t.Setenv("ENABLE_GOOGLE_MEET", "true")

	creator := "bob@example.com"
	email := "bob@workspace.example"
	eligible := true
	conn := &models.GoogleMeetConnection{
		ID:                    uuid.New(),
		CreatorIdentityID:     creator,
		GoogleUserID:          "google-sub-bob",
		GoogleUserEmail:       &email,
		WorkspaceEligible:     &eligible,
		AccessTokenEncrypted:  []byte("a"),
		RefreshTokenEncrypted: []byte("r"),
		ExpiresAt:             time.Now().Add(time.Hour),
	}
	require.NoError(t, h.DB.CreateGoogleMeetConnection(context.Background(), conn))

	req := httptest.NewRequest(http.MethodGet, "/api/google-meet/status", nil)
	req.Header.Set("X-Creator-Identity", creator)
	w := httptest.NewRecorder()
	h.GoogleMeetAPIStatus(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var resp GoogleMeetStatusResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.True(t, resp.Enabled)
	require.True(t, resp.Connected)
	require.Equal(t, "bob@workspace.example", resp.GoogleEmail)
	require.Equal(t, "google-sub-bob", resp.GoogleUserID)
	require.NotNil(t, resp.WorkspaceEligible)
	require.True(t, *resp.WorkspaceEligible)
}

func TestGoogleMeetAPIConnect_ReturnsAuthURLWithConsent(t *testing.T) {
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	t.Setenv("ENABLE_GOOGLE_MEET", "true")
	t.Setenv("GOOGLE_MEET_CLIENT_ID", "test-client")
	t.Setenv("GOOGLE_MEET_CLIENT_SECRET", "test-secret")
	t.Setenv("GOOGLE_MEET_REDIRECT_URL", "https://api.example.com/auth/google-meet/callback")

	req := httptest.NewRequest(http.MethodPost, "/api/google-meet/connect?creator_identity=alice@example.com", nil)
	w := httptest.NewRecorder()
	h.GoogleMeetAPIConnect(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var resp GoogleMeetConnectResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Contains(t, resp.AuthURL, "prompt=consent")
	require.Contains(t, resp.AuthURL, "access_type=offline")
	require.Contains(t, resp.AuthURL, "client_id=test-client")
	require.Contains(t, resp.AuthURL, "state=alice%40example.com")
}

func TestGoogleMeetAPIConnect_FlagOffReturns404(t *testing.T) {
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	t.Setenv("ENABLE_GOOGLE_MEET", "")

	req := httptest.NewRequest(http.MethodPost, "/api/google-meet/connect", nil)
	w := httptest.NewRecorder()
	h.GoogleMeetAPIConnect(w, req)
	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestGoogleMeetAPIDisconnect_RemovesConnection(t *testing.T) {
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	t.Setenv("ENABLE_GOOGLE_MEET", "true")

	creator := "carol@example.com"
	conn := &models.GoogleMeetConnection{
		ID:                    uuid.New(),
		CreatorIdentityID:     creator,
		GoogleUserID:          "x",
		AccessTokenEncrypted:  []byte("a"),
		RefreshTokenEncrypted: []byte("r"),
		ExpiresAt:             time.Now().Add(time.Hour),
	}
	require.NoError(t, h.DB.CreateGoogleMeetConnection(context.Background(), conn))

	req := httptest.NewRequest(http.MethodPost, "/api/google-meet/disconnect", nil)
	req.Header.Set("X-Creator-Identity", creator)
	w := httptest.NewRecorder()
	h.GoogleMeetAPIDisconnect(w, req)
	require.Equal(t, http.StatusNoContent, w.Code)

	got, err := h.DB.GetGoogleMeetConnectionByCreatorIdentity(context.Background(), creator)
	require.NoError(t, err)
	require.Nil(t, got)
}

func TestGoogleMeetAPIDisconnect_RequiresCreatorIdentity(t *testing.T) {
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	t.Setenv("ENABLE_GOOGLE_MEET", "true")

	req := httptest.NewRequest(http.MethodPost, "/api/google-meet/disconnect", nil)
	w := httptest.NewRecorder()
	h.GoogleMeetAPIDisconnect(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code)
}
