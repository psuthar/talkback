package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/googlemeet"
	"github.com/psuthar/talkback/internal/models"
	"github.com/psuthar/talkback/internal/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedGoogleMeetConnection inserts a GoogleMeetConnection for a creator with
// valid encrypted tokens (encrypted with the default key the handler reads
// when ENCRYPTION_KEY is unset). Returns the inserted connection.
func seedGoogleMeetConnection(t *testing.T, h *Handlers, creatorIdentity string) *models.GoogleMeetConnection {
	t.Helper()
	access, err := utils.EncryptToken([]byte("fake-access"), "default-key-change-in-production")
	require.NoError(t, err)
	refresh, err := utils.EncryptToken([]byte("fake-refresh"), "default-key-change-in-production")
	require.NoError(t, err)
	conn := &models.GoogleMeetConnection{
		ID:                    uuid.New(),
		CreatorIdentityID:     creatorIdentity,
		GoogleUserID:          "google-sub-test",
		AccessTokenEncrypted:  access,
		RefreshTokenEncrypted: refresh,
		ExpiresAt:             time.Now().Add(time.Hour),
	}
	require.NoError(t, h.DB.CreateGoogleMeetConnection(context.Background(), conn))
	return conn
}

// fakeGoogleServer is a single httptest server that handles all Google
// endpoints the SPA / pipeline call: OAuth token + userinfo + workspace
// probe (Meet API) + recordings + transcripts. Routes return canned JSON.
func startFakeGoogleServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"smoke-access","refresh_token":"smoke-refresh","expires_in":3600,"scope":"openid email profile https://www.googleapis.com/auth/meetings.space.readonly https://www.googleapis.com/auth/drive.readonly"}`))
		case "/userinfo":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"sub":"smoke-sub","email":"smoke@workspace.example"}`))
		case "/v2/conferenceRecords":
			// Used both by the workspace_eligible probe (pageSize=1) and ListConferenceRecords.
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"conferenceRecords":[{"name":"conferenceRecords/A","startTime":"2026-05-01T10:00:00Z"}]}`))
		case "/v2/conferenceRecords/A/recordings":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"recordings":[{"name":"conferenceRecords/A/recordings/r1","state":"FILE_GENERATED","startTime":"2026-05-01T10:00:00Z","driveDestination":{"file":"drive-1","exportUri":"https://drive.google.com/file/d/drive-1/view"}}]}`))
		case "/v2/conferenceRecords/A/transcripts":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"transcripts":[{"name":"conferenceRecords/A/transcripts/t1","state":"FILE_GENERATED"}]}`))
		default:
			t.Logf("fake google server: unexpected path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestSmoke_GoogleMeet_OAuthCallback drives the OAuth callback end-to-end
// against the fake Google endpoints: token exchange + userinfo + workspace
// probe → DB row persisted with refresh_token + workspace_eligible=true.
func TestSmoke_GoogleMeet_OAuthCallback(t *testing.T) {
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	t.Setenv("ENABLE_GOOGLE_MEET", "true")
	t.Setenv("GOOGLE_MEET_CLIENT_ID", "smoke-client")
	t.Setenv("GOOGLE_MEET_CLIENT_SECRET", "smoke-secret")
	t.Setenv("GOOGLE_MEET_REDIRECT_URL", "https://api.example.com/auth/google-meet/callback")
	t.Setenv("APP_REDIRECT_URL", "https://app.example.com")
	srv := startFakeGoogleServer(t)
	prevTok, prevUI, prevProbe := googleMeetTokenURL, googleMeetUserInfoURL, googleMeetProbeURL
	googleMeetTokenURL = srv.URL + "/token"
	googleMeetUserInfoURL = srv.URL + "/userinfo"
	googleMeetProbeURL = srv.URL + "/v2/conferenceRecords?pageSize=1"
	t.Cleanup(func() {
		googleMeetTokenURL = prevTok
		googleMeetUserInfoURL = prevUI
		googleMeetProbeURL = prevProbe
	})

	creator := "smoke@workspace.example"
	req := httptest.NewRequest(http.MethodGet, "/auth/google-meet/callback?code=abc&state="+creator, nil)
	w := httptest.NewRecorder()
	h.GoogleMeetAuthCallback(w, req)
	require.Equal(t, http.StatusFound, w.Code)
	assert.Contains(t, w.Header().Get("Location"), "google_meet=connected")

	conn, err := h.DB.GetGoogleMeetConnectionByCreatorIdentity(context.Background(), creator)
	require.NoError(t, err)
	require.NotNil(t, conn)
	require.NotNil(t, conn.WorkspaceEligible)
	assert.True(t, *conn.WorkspaceEligible, "workspace probe returns 200 → eligible=true")
	require.NotNil(t, conn.GrantedScope)
	assert.Contains(t, *conn.GrantedScope, "drive.readonly")
	// Tokens decrypt cleanly.
	access, _ := utils.DecryptToken(conn.AccessTokenEncrypted, "default-key-change-in-production")
	assert.Equal(t, "smoke-access", string(access))
	refresh, _ := utils.DecryptToken(conn.RefreshTokenEncrypted, "default-key-change-in-production")
	assert.Equal(t, "smoke-refresh", string(refresh))
}

// TestSmoke_GoogleMeet_StatusReflectsConnection covers the always-on status
// endpoint after a connection has been seeded: enabled=true + connected=true
// + workspace_eligible flows through.
func TestSmoke_GoogleMeet_StatusReflectsConnection(t *testing.T) {
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	t.Setenv("ENABLE_GOOGLE_MEET", "true")

	creator := "status-smoke@example.com"
	seedGoogleMeetConnection(t, h, creator)

	req := httptest.NewRequest(http.MethodGet, "/api/google-meet/status", nil)
	req.Header.Set("X-Creator-Identity", creator)
	w := httptest.NewRecorder()
	h.GoogleMeetAPIStatus(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var resp GoogleMeetStatusResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.True(t, resp.Enabled)
	assert.True(t, resp.Connected)
}

// TestSmoke_GoogleMeet_RecordingsListFlattening drives /api/google-meet/recordings
// against the fake Google server (Meet API). Verifies the list returns one
// flattened item with transcript_state=ready (transcripts list returned
// FILE_GENERATED).
func TestSmoke_GoogleMeet_RecordingsListFlattening(t *testing.T) {
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	t.Setenv("ENABLE_GOOGLE_MEET", "true")
	srv := startFakeGoogleServer(t)
	t.Cleanup(googlemeet.SetAPIBasesForTest(srv.URL+"/v2", srv.URL+"/drive/v3"))

	creator := "rec-smoke@workspace.example"
	scope := "openid email https://www.googleapis.com/auth/meetings.space.readonly https://www.googleapis.com/auth/drive.readonly"
	access, err := utils.EncryptToken([]byte("smoke-access"), "default-key-change-in-production")
	require.NoError(t, err)
	refresh, err := utils.EncryptToken([]byte("smoke-refresh"), "default-key-change-in-production")
	require.NoError(t, err)
	conn := &models.GoogleMeetConnection{
		ID:                    uuid.New(),
		CreatorIdentityID:     creator,
		GoogleUserID:          "sub-1",
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
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp GoogleMeetRecordingsResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Items, 1)
	// SCRUM-466: response is normalized to the Zoom-shaped fields the
	// SPA picker reads. RecordingName → InstanceUUID, DriveFileID is
	// mapped via HasVideo, TranscriptState=="ready" → HasTranscript.
	assert.Equal(t, "conferenceRecords/A/recordings/r1", resp.Items[0].InstanceUUID)
	assert.Equal(t, "conferenceRecords/A", resp.Items[0].MeetingUUID)
	assert.True(t, resp.Items[0].HasVideo)
	assert.True(t, resp.Items[0].HasTranscript)
}

