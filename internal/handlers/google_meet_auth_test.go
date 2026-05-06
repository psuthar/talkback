package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/models"
	"github.com/psuthar/talkback/internal/utils"
	"github.com/stretchr/testify/require"
)

// stubGoogleEndpoints starts a single httptest server that handles token, userinfo
// and conferenceRecords-probe paths. probeStatus controls the workspace probe response.
type fakeTokenResp struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	ExpiresIn    int    `json:"expires_in"`
	Scope        string `json:"scope,omitempty"`
}

func stubGoogleEndpoints(t *testing.T, tokenResp fakeTokenResp, probeStatus int) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(tokenResp)
		case "/userinfo":
			require.Equal(t, "Bearer "+tokenResp.AccessToken, r.Header.Get("Authorization"))
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"sub":"google-sub-123","email":"alice@example.com"}`))
		case "/probe":
			require.Equal(t, "Bearer "+tokenResp.AccessToken, r.Header.Get("Authorization"))
			w.WriteHeader(probeStatus)
		default:
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
	}))
	t.Cleanup(srv.Close)
	prevTok, prevUI, prevProbe := googleMeetTokenURL, googleMeetUserInfoURL, googleMeetProbeURL
	googleMeetTokenURL = srv.URL + "/token"
	googleMeetUserInfoURL = srv.URL + "/userinfo"
	googleMeetProbeURL = srv.URL + "/probe"
	t.Cleanup(func() {
		googleMeetTokenURL = prevTok
		googleMeetUserInfoURL = prevUI
		googleMeetProbeURL = prevProbe
	})
	return srv.URL
}

func setMeetEnv(t *testing.T) {
	t.Helper()
	t.Setenv("ENABLE_GOOGLE_MEET", "true")
	t.Setenv("GOOGLE_MEET_CLIENT_ID", "test-client")
	t.Setenv("GOOGLE_MEET_CLIENT_SECRET", "test-secret")
	t.Setenv("GOOGLE_MEET_REDIRECT_URL", "https://api.example.com/auth/google-meet/callback")
	t.Setenv("APP_REDIRECT_URL", "https://app.example.com")
	t.Setenv("ENCRYPTION_KEY", "test-encryption-key-must-be-32+chars")
}

func TestGoogleMeetAuthStart_AuthorizeURLContainsConsentAndOffline(t *testing.T) {
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	setMeetEnv(t)
	req := httptest.NewRequest(http.MethodGet, "/auth/google-meet/start?creator_identity=alice@example.com", nil)
	w := httptest.NewRecorder()
	h.GoogleMeetAuthStart(w, req)

	require.Equal(t, http.StatusFound, w.Code)
	loc := w.Header().Get("Location")
	require.Contains(t, loc, "access_type=offline")
	require.Contains(t, loc, "prompt=consent")
	require.Contains(t, loc, "include_granted_scopes=true")
	require.Contains(t, loc, "client_id=test-client")
	require.Contains(t, loc, "state=alice%40example.com")
	require.Contains(t, loc, "scope=")
	require.Contains(t, loc, "meetings.space.readonly")
	require.Contains(t, loc, "drive.readonly")
}

func TestGoogleMeetAuthCallback_PersistsConnectionAndProbe(t *testing.T) {
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	setMeetEnv(t)
	stubGoogleEndpoints(t, fakeTokenResp{
		AccessToken: "access-1", RefreshToken: "refresh-1", ExpiresIn: 3600, Scope: "openid email https://www.googleapis.com/auth/drive.readonly",
	}, http.StatusOK)

	creator := "alice@example.com"
	req := httptest.NewRequest(http.MethodGet, "/auth/google-meet/callback?code=abc&state="+creator, nil)
	w := httptest.NewRecorder()
	h.GoogleMeetAuthCallback(w, req)

	require.Equal(t, http.StatusFound, w.Code)
	loc := w.Header().Get("Location")
	require.Contains(t, loc, "google_meet=connected")
	require.Contains(t, loc, "creator_identity="+url.QueryEscape(creator))

	conn, err := h.DB.GetGoogleMeetConnectionByCreatorIdentity(context.Background(), creator)
	require.NoError(t, err)
	require.NotNil(t, conn)
	require.Equal(t, "google-sub-123", conn.GoogleUserID)
	require.NotNil(t, conn.GoogleUserEmail)
	require.Equal(t, "alice@example.com", *conn.GoogleUserEmail)
	require.NotNil(t, conn.WorkspaceEligible)
	require.True(t, *conn.WorkspaceEligible, "200 from probe should set eligible=true")
	require.NotNil(t, conn.GrantedScope)
	require.Contains(t, *conn.GrantedScope, "drive.readonly")
	// Encrypted tokens decrypt cleanly with the same key.
	access, err := utils.DecryptToken(conn.AccessTokenEncrypted, os.Getenv("ENCRYPTION_KEY"))
	require.NoError(t, err)
	require.Equal(t, "access-1", string(access))
	refresh, err := utils.DecryptToken(conn.RefreshTokenEncrypted, os.Getenv("ENCRYPTION_KEY"))
	require.NoError(t, err)
	require.Equal(t, "refresh-1", string(refresh))
}

func TestGoogleMeetAuthCallback_RefreshTokenMissingRedirectsWithError(t *testing.T) {
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	setMeetEnv(t)
	stubGoogleEndpoints(t, fakeTokenResp{
		AccessToken: "access-only", RefreshToken: "", ExpiresIn: 3600,
	}, http.StatusOK)

	req := httptest.NewRequest(http.MethodGet, "/auth/google-meet/callback?code=abc&state=bob@example.com", nil)
	w := httptest.NewRecorder()
	h.GoogleMeetAuthCallback(w, req)

	require.Equal(t, http.StatusFound, w.Code)
	require.Contains(t, w.Header().Get("Location"), "message=refresh_token_missing")
	// No connection should be persisted.
	conn, err := h.DB.GetGoogleMeetConnectionByCreatorIdentity(context.Background(), "bob@example.com")
	require.NoError(t, err)
	require.Nil(t, conn)
}

func TestGoogleMeetAuthCallback_WorkspaceProbe403SetsEligibleFalse(t *testing.T) {
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	setMeetEnv(t)
	stubGoogleEndpoints(t, fakeTokenResp{
		AccessToken: "a", RefreshToken: "r", ExpiresIn: 3600,
	}, http.StatusForbidden)

	req := httptest.NewRequest(http.MethodGet, "/auth/google-meet/callback?code=abc&state=carol@example.com", nil)
	w := httptest.NewRecorder()
	h.GoogleMeetAuthCallback(w, req)

	require.Equal(t, http.StatusFound, w.Code)
	conn, err := h.DB.GetGoogleMeetConnectionByCreatorIdentity(context.Background(), "carol@example.com")
	require.NoError(t, err)
	require.NotNil(t, conn)
	require.NotNil(t, conn.WorkspaceEligible)
	require.False(t, *conn.WorkspaceEligible, "403 from probe should set eligible=false")
}

func TestGetValidGoogleMeetAccessTokenContext_RefreshTokenMissingSentinel(t *testing.T) {
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	t.Setenv("ENCRYPTION_KEY", "test-encryption-key-must-be-32+chars")

	creator := "no-refresh@example.com"
	access, err := utils.EncryptToken([]byte("a"), os.Getenv("ENCRYPTION_KEY"))
	require.NoError(t, err)
	conn := &models.GoogleMeetConnection{
		ID:                    uuid.New(),
		CreatorIdentityID:     creator,
		GoogleUserID:          "x",
		AccessTokenEncrypted:  access,
		RefreshTokenEncrypted: []byte{}, // empty bytes simulate the "no refresh token" state we refuse on use
		ExpiresAt:             time.Now().Add(time.Hour),
	}
	require.NoError(t, h.DB.CreateGoogleMeetConnection(context.Background(), conn))

	_, _, err = h.GetValidGoogleMeetAccessTokenContext(context.Background(), creator)
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "meet_refresh_token_missing"))
}

func TestGoogleMeetAuthDisconnect_Idempotent(t *testing.T) {
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	t.Setenv("ENABLE_GOOGLE_MEET", "true")

	req := httptest.NewRequest(http.MethodPost, "/auth/google-meet/disconnect?creator_identity=missing@example.com", nil)
	w := httptest.NewRecorder()
	h.GoogleMeetAuthDisconnect(w, req)
	require.Equal(t, http.StatusOK, w.Code, "disconnect on missing row should be idempotent (200)")
}

