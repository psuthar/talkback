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
	"github.com/psuthar/talkback/internal/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGoogleMeetAPIRecordings_DisabledReturns404(t *testing.T) {
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	t.Setenv("ENABLE_GOOGLE_MEET", "")

	req := httptest.NewRequest(http.MethodGet, "/api/google-meet/recordings", nil)
	w := httptest.NewRecorder()
	h.GoogleMeetAPIRecordings(w, req)
	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestGoogleMeetAPIRecordings_NoIdentityReturns401(t *testing.T) {
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	t.Setenv("ENABLE_GOOGLE_MEET", "true")

	req := httptest.NewRequest(http.MethodGet, "/api/google-meet/recordings", nil)
	w := httptest.NewRecorder()
	h.GoogleMeetAPIRecordings(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code)
	var resp map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "google_meet_not_connected", resp["code"])
}

func TestGoogleMeetAPIRecordings_MissingDriveScopeReturns401(t *testing.T) {
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	t.Setenv("ENABLE_GOOGLE_MEET", "true")

	creator := "alice@example.com"
	access, err := utils.EncryptToken([]byte("a"), "default-key-change-in-production")
	require.NoError(t, err)
	refresh, err := utils.EncryptToken([]byte("r"), "default-key-change-in-production")
	require.NoError(t, err)
	scope := "openid email profile" // missing drive.readonly
	conn := &models.GoogleMeetConnection{
		ID:                    uuid.New(),
		CreatorIdentityID:     creator,
		GoogleUserID:          "google-sub",
		GrantedScope:          &scope,
		AccessTokenEncrypted:  access,
		RefreshTokenEncrypted: refresh,
		ExpiresAt:             time.Now().Add(time.Hour),
	}
	require.NoError(t, h.DB.CreateGoogleMeetConnection(context.Background(), conn))

	req := httptest.NewRequest(http.MethodGet, "/api/google-meet/recordings", nil)
	req.Header.Set("X-Creator-Identity", creator)
	w := httptest.NewRecorder()
	h.GoogleMeetAPIRecordings(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code)
	var resp map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "meet_missing_scopes", resp["code"])
}
