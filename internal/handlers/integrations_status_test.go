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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestZoomEnabled pins the SCRUM-418 default-on semantics for ENABLE_ZOOM
// (unlike googleMeetEnabled / teamsEnabled which default to off).
func TestZoomEnabled(t *testing.T) {
	cases := []struct {
		name string
		env  string
		want bool
	}{
		{"unset is enabled (legacy default-on)", "", true},
		{"true is enabled", "true", true},
		{"arbitrary non-false is enabled", "yes", true},
		{"explicit false is disabled", "false", false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("ENABLE_ZOOM", tc.env)
			assert.Equal(t, tc.want, zoomEnabled())
		})
	}
}

// TestIntegrationsStatus_AllCombinations covers the (enabled × connected)
// matrix for one platform and spot-checks the others — per SCRUM-418 spec.
func TestIntegrationsStatus_AllCombinations(t *testing.T) {
	t.Setenv("ENABLE_GOOGLE_MEET", "true")
	t.Setenv("ENABLE_TEAMS", "true")
	t.Setenv("ENABLE_ZOOM", "true")
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	ctx := context.Background()
	connectedUser := &models.User{
		ID:          uuid.New(),
		Email:       "user-" + uuid.NewString() + "@example.com",
		DisplayName: "U",
		GlobalRole:  models.GlobalRoleCreator,
		Status:      models.UserStatusActive,
	}
	require.NoError(t, h.DB.CreateUser(ctx, connectedUser))

	// Seed a Zoom + Meet + Teams connection for connectedUser.
	zoomEmail := "zoom-account@example.com"
	require.NoError(t, h.DB.CreateZoomConnection(ctx, &models.ZoomConnection{
		ID:                    uuid.New(),
		CreatorIdentityID:     connectedUser.Email,
		ZoomUserID:            "zoom-user-id",
		ZoomUserEmail:         &zoomEmail,
		AccessTokenEncrypted:  []byte("at"),
		RefreshTokenEncrypted: []byte("rt"),
		ExpiresAt:             time.Now().Add(time.Hour),
	}))
	meetEmail := "meet-account@example.com"
	require.NoError(t, h.DB.CreateGoogleMeetConnection(ctx, &models.GoogleMeetConnection{
		ID:                    uuid.New(),
		CreatorIdentityID:     connectedUser.Email,
		GoogleUserID:          "google-user-id",
		GoogleUserEmail:       &meetEmail,
		AccessTokenEncrypted:  []byte("at"),
		RefreshTokenEncrypted: []byte("rt"),
		ExpiresAt:             time.Now().Add(time.Hour),
	}))
	teamsEmail := "teams-account@example.com"
	require.NoError(t, h.DB.CreateTeamsConnection(ctx, &models.TeamsConnection{
		ID:                    uuid.New(),
		CreatorIdentityID:     connectedUser.Email,
		TeamsUserID:           "teams-user-id",
		TeamsUserEmail:        &teamsEmail,
		AccessTokenEncrypted:  []byte("at"),
		RefreshTokenEncrypted: []byte("rt"),
		ExpiresAt:             time.Now().Add(time.Hour),
	}))

	t.Run("all enabled + all connected returns each platform's account email", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/integrations/status", nil)
		req = req.WithContext(context.WithValue(req.Context(), userContextKey, connectedUser))
		w := httptest.NewRecorder()
		h.IntegrationsStatus(w, req)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var resp IntegrationsStatusResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.True(t, resp.Zoom.Enabled)
		assert.True(t, resp.Zoom.Connected)
		require.NotNil(t, resp.Zoom.AccountEmail)
		assert.Equal(t, zoomEmail, *resp.Zoom.AccountEmail)
		assert.True(t, resp.GoogleMeet.Enabled)
		assert.True(t, resp.GoogleMeet.Connected)
		require.NotNil(t, resp.GoogleMeet.AccountEmail)
		assert.Equal(t, meetEmail, *resp.GoogleMeet.AccountEmail)
		assert.True(t, resp.Teams.Enabled)
		assert.True(t, resp.Teams.Connected)
		require.NotNil(t, resp.Teams.AccountEmail)
		assert.Equal(t, teamsEmail, *resp.Teams.AccountEmail)
	})

	t.Run("Teams disabled returns enabled=false connected=false without hitting DB", func(t *testing.T) {
		t.Setenv("ENABLE_TEAMS", "")
		req := httptest.NewRequest(http.MethodGet, "/api/integrations/status", nil)
		req = req.WithContext(context.WithValue(req.Context(), userContextKey, connectedUser))
		w := httptest.NewRecorder()
		h.IntegrationsStatus(w, req)
		require.Equal(t, http.StatusOK, w.Code)
		var resp IntegrationsStatusResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.False(t, resp.Teams.Enabled)
		assert.False(t, resp.Teams.Connected)
		assert.Nil(t, resp.Teams.AccountEmail)
	})

	t.Run("Zoom disabled honors ENABLE_ZOOM=false", func(t *testing.T) {
		t.Setenv("ENABLE_ZOOM", "false")
		req := httptest.NewRequest(http.MethodGet, "/api/integrations/status", nil)
		req = req.WithContext(context.WithValue(req.Context(), userContextKey, connectedUser))
		w := httptest.NewRecorder()
		h.IntegrationsStatus(w, req)
		require.Equal(t, http.StatusOK, w.Code)
		var resp IntegrationsStatusResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.False(t, resp.Zoom.Enabled)
		assert.False(t, resp.Zoom.Connected)
	})

	t.Run("unconnected user gets enabled=true connected=false everywhere", func(t *testing.T) {
		other := &models.User{
			ID:          uuid.New(),
			Email:       "no-connections-" + uuid.NewString() + "@example.com",
			DisplayName: "Other",
			GlobalRole:  models.GlobalRoleCreator,
			Status:      models.UserStatusActive,
		}
		require.NoError(t, h.DB.CreateUser(ctx, other))
		req := httptest.NewRequest(http.MethodGet, "/api/integrations/status", nil)
		req = req.WithContext(context.WithValue(req.Context(), userContextKey, other))
		w := httptest.NewRecorder()
		h.IntegrationsStatus(w, req)
		require.Equal(t, http.StatusOK, w.Code)
		var resp IntegrationsStatusResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.True(t, resp.Zoom.Enabled)
		assert.False(t, resp.Zoom.Connected)
		assert.True(t, resp.GoogleMeet.Enabled)
		assert.False(t, resp.GoogleMeet.Connected)
		assert.True(t, resp.Teams.Enabled)
		assert.False(t, resp.Teams.Connected)
	})

	t.Run("no user in context returns 401", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/integrations/status", nil)
		w := httptest.NewRecorder()
		h.IntegrationsStatus(w, req)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("wrong method returns 405", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/integrations/status", nil)
		req = req.WithContext(context.WithValue(req.Context(), userContextKey, connectedUser))
		w := httptest.NewRecorder()
		h.IntegrationsStatus(w, req)
		assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
	})
}
