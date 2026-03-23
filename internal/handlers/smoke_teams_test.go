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
	"github.com/psuthar/talkback/internal/models"
	"github.com/psuthar/talkback/internal/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedTeamsConnection inserts a TeamsConnection row with a valid (non-expired) encrypted token
// for the given creatorIdentity. Returns the inserted connection for verification.
func seedTeamsConnection(t *testing.T, h *Handlers, creatorIdentity string, teamsEmail string) *models.TeamsConnection {
	t.Helper()
	ctx := context.Background()
	enc, err := utils.EncryptToken([]byte("fake-access-token"), "default-key-change-in-production")
	require.NoError(t, err, "EncryptToken must succeed")
	email := teamsEmail
	conn := &models.TeamsConnection{
		ID:                    uuid.New(),
		CreatorIdentityID:     creatorIdentity,
		TenantID:              "common",
		TeamsUserID:           "teams-user-1",
		TeamsUserEmail:        &email,
		AccessTokenEncrypted:  enc,
		RefreshTokenEncrypted: enc,
		ExpiresAt:             time.Now().Add(time.Hour),
	}
	require.NoError(t, h.DB.CreateTeamsConnection(ctx, conn))
	return conn
}

// TestSmoke_TeamsAPIStatus groups all TeamsAPIStatus handler tests. Using a parent test with
// subtests serialises env-var mutations (t.Setenv) so they do not race against parallel tests
// in the package. The subtests themselves do not call t.Parallel() because they share env-var
// state via t.Setenv, which is safe within a serial subtest chain.
func TestSmoke_TeamsAPIStatus(t *testing.T) {
	// TeamsAPIStatus_DisabledReturnsDisabled: when ENABLE_TEAMS is not "true",
	// GET /api/teams/status must return 200 with enabled=false and connected=false.
	t.Run("DisabledReturnsDisabled", func(t *testing.T) {
		t.Setenv("ENABLE_TEAMS", "")
		h, cleanup := setupTestHandlersParallel(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodGet, "/api/teams/status", nil)
		w := httptest.NewRecorder()
		h.TeamsAPIStatus(w, req)

		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var resp TeamsStatusResponse
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		assert.False(t, resp.Enabled, "enabled must be false when ENABLE_TEAMS != true")
		assert.False(t, resp.Connected, "connected must be false when Teams is disabled")
	})

	// TeamsAPIStatus_EnabledNoIdentity: with ENABLE_TEAMS=true but no X-Creator-Identity header,
	// must return enabled=true, connected=false.
	t.Run("EnabledNoIdentityReturnsEnabledNotConnected", func(t *testing.T) {
		t.Setenv("ENABLE_TEAMS", "true")
		h, cleanup := setupTestHandlersParallel(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodGet, "/api/teams/status", nil)
		w := httptest.NewRecorder()
		h.TeamsAPIStatus(w, req)

		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var resp TeamsStatusResponse
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		assert.True(t, resp.Enabled, "enabled must be true when ENABLE_TEAMS=true")
		assert.False(t, resp.Connected, "connected must be false when no creator identity present")
	})

	// TeamsAPIStatus_Connected: with a seeded TeamsConnection for the creator identity,
	// must return enabled=true, connected=true, and teams_email.
	t.Run("ConnectedReturnsTeamsEmail", func(t *testing.T) {
		t.Setenv("ENABLE_TEAMS", "true")
		h, cleanup := setupTestHandlersParallel(t)
		defer cleanup()

		const creatorIdentity = "teams-status-connected@smoke.test"
		const teamsEmail = "user@contoso.com"
		seedTeamsConnection(t, h, creatorIdentity, teamsEmail)

		req := httptest.NewRequest(http.MethodGet, "/api/teams/status", nil)
		req.Header.Set("X-Creator-Identity", creatorIdentity)
		w := httptest.NewRecorder()
		h.TeamsAPIStatus(w, req)

		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var resp TeamsStatusResponse
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		assert.True(t, resp.Enabled)
		assert.True(t, resp.Connected, "connected must be true when a TeamsConnection row exists")
		assert.Equal(t, teamsEmail, resp.TeamsEmail, "teams_email must match the stored value")
	})
}

// TestSmoke_TeamsAPIDisconnect groups all TeamsAPIDisconnect handler tests, serialised
// so that env-var mutations do not race with parallel tests in the package.
func TestSmoke_TeamsAPIDisconnect(t *testing.T) {
	// TeamsAPIDisconnect_NoIdentity: POST /api/teams/disconnect with ENABLE_TEAMS=true
	// but no creator_identity must return 401 with code "teams_identity_required".
	t.Run("NoIdentityReturns401", func(t *testing.T) {
		t.Setenv("ENABLE_TEAMS", "true")
		h, cleanup := setupTestHandlersParallel(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodPost, "/api/teams/disconnect", nil)
		w := httptest.NewRecorder()
		h.TeamsAPIDisconnect(w, req)

		require.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
		var resp map[string]string
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		assert.Equal(t, "teams_identity_required", resp["code"])
	})

	// TeamsAPIDisconnect_RemovesConnection: seed a connection, POST with creator_identity
	// must delete the row and return 204.
	t.Run("RemovesConnection", func(t *testing.T) {
		t.Setenv("ENABLE_TEAMS", "true")
		h, cleanup := setupTestHandlersParallel(t)
		defer cleanup()

		const creatorIdentity = "teams-disconnect@smoke.test"
		seedTeamsConnection(t, h, creatorIdentity, "disc@contoso.com")

		// Confirm it exists before disconnect.
		ctx := context.Background()
		before, err := h.DB.GetTeamsConnectionByCreatorIdentity(ctx, creatorIdentity)
		require.NoError(t, err)
		require.NotNil(t, before, "connection must exist before disconnect")

		req := httptest.NewRequest(http.MethodPost, "/api/teams/disconnect", nil)
		req.Header.Set("X-Creator-Identity", creatorIdentity)
		w := httptest.NewRecorder()
		h.TeamsAPIDisconnect(w, req)

		require.Equal(t, http.StatusNoContent, w.Code, w.Body.String())

		// Verify the row was deleted.
		after, err := h.DB.GetTeamsConnectionByCreatorIdentity(ctx, creatorIdentity)
		require.NoError(t, err)
		assert.Nil(t, after, "TeamsConnection row must be absent after disconnect")
	})
}

// TestSmoke_TeamsImport groups all TeamsImport handler tests, serialised so that
// env-var mutations do not race with parallel tests in the package.
func TestSmoke_TeamsImport(t *testing.T) {
	// TeamsImport_Disabled: POST /api/teams/import when ENABLE_TEAMS != "true" must return 404.
	t.Run("DisabledReturns404", func(t *testing.T) {
		t.Setenv("ENABLE_TEAMS", "")
		h, cleanup := setupTestHandlersParallel(t)
		defer cleanup()

		req := httptest.NewRequest(http.MethodPost, "/api/teams/import", nil)
		w := httptest.NewRecorder()
		h.TeamsImport(w, req)

		require.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
	})

	// TeamsImport_NoAuth: ENABLE_TEAMS=true but no authenticated user must return 401.
	t.Run("NoAuthReturns401", func(t *testing.T) {
		t.Setenv("ENABLE_TEAMS", "true")
		h, cleanup := setupTestHandlersParallel(t)
		defer cleanup()

		body, _ := json.Marshal(map[string]any{
			"meeting_id":   "some-meeting",
			"recording_id": "some-recording",
		})
		req := httptest.NewRequest(http.MethodPost, "/api/teams/import", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		// RequireAuth will reject the request because there is no session cookie.
		h.RequireAuth(h.TeamsImport)(w, req)

		require.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
	})

	// TeamsImport_MissingFields: valid auth and ENABLE_TEAMS=true but empty meeting_id must return 400.
	t.Run("MissingFieldsReturns400", func(t *testing.T) {
		t.Setenv("ENABLE_TEAMS", "true")
		h, cleanup := setupTestHandlersParallel(t)
		defer cleanup()

		const creatorEmail = "teams-missing-fields@smoke.test"
		body, _ := json.Marshal(map[string]any{
			"title":        "Teams Missing Fields",
			"meeting_id":   "",
			"recording_id": "",
		})
		req := httptest.NewRequest(http.MethodPost, "/api/teams/import", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Creator-Identity", creatorEmail)
		user := addUserSessionCookie(t, h, req, creatorEmail)
		seedTeamsConnection(t, h, user.Email, "user@contoso.com")
		w := httptest.NewRecorder()
		h.RequireAuth(h.TeamsImport)(w, req)

		require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
		var resp map[string]string
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		assert.Contains(t, resp["message"], "required")
	})

	// TeamsImport_CreatesSessionAndJob: happy path — ENABLE_TEAMS=true, valid auth, seeded
	// TeamsConnection where creatorIdentity == user.Email → 202, session+job created in DB.
	t.Run("CreatesSessionAndJob", func(t *testing.T) {
		t.Setenv("ENABLE_TEAMS", "true")
		h, cleanup := setupTestHandlersParallel(t)
		defer cleanup()

		const creatorEmail = "teams-import-creator@smoke.test"
		const meetingID = "teams-meeting-abc123"
		const recordingID = "teams-recording-xyz789"
		const sessionTitle = "Teams Import Smoke Test"

		// Seed a TeamsConnection so GetValidTeamsAccessTokenContext succeeds.
		// creatorIdentity must equal user.Email (enforced by the TeamsImport handler).
		cookieReq := httptest.NewRequest(http.MethodPost, "/api/teams/import", nil)
		user := addUserSessionCookie(t, h, cookieReq, creatorEmail)
		seedTeamsConnection(t, h, user.Email, "creator@contoso.com")

		body, _ := json.Marshal(TeamsImportRequest{
			Title:       sessionTitle,
			MeetingID:   meetingID,
			RecordingID: recordingID,
		})
		req := httptest.NewRequest(http.MethodPost, "/api/teams/import", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Creator-Identity", user.Email)
		for _, c := range cookieReq.Cookies() {
			req.AddCookie(c)
		}
		w := httptest.NewRecorder()
		h.RequireAuth(h.TeamsImport)(w, req)

		require.Equal(t, http.StatusAccepted, w.Code, w.Body.String())
		var resp TeamsImportResponse
		require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
		assert.NotEmpty(t, resp.ID, "session ID must be non-empty")
		assert.NotEmpty(t, resp.JobID, "job ID must be non-empty")
		assert.Equal(t, models.ProcessingStateQueued, resp.State)

		// Verify DB: session row with source_provider=teams.
		ctx := context.Background()
		sessionID, err := uuid.Parse(resp.ID)
		require.NoError(t, err)
		sess, err := h.DB.GetSession(ctx, sessionID)
		require.NoError(t, err)
		require.NotNil(t, sess, "session must exist in DB")
		assert.Equal(t, models.SessionSourceTeams, sess.SourceProvider)
		assert.Equal(t, sessionTitle, sess.Title)

		// Verify DB: processing job with source="teams" in queued state.
		job, err := h.DB.GetSessionProcessingJobBySessionID(ctx, sessionID, "teams")
		require.NoError(t, err)
		require.NotNil(t, job, "teams processing job must exist in DB")
		assert.Equal(t, "teams", job.Source)
		assert.Equal(t, models.ProcessingStateQueued, job.State)
		assert.Equal(t, models.ProcessingStageFetch, job.Stage)
		require.NotNil(t, job.CreatorIdentity)
		assert.Equal(t, user.Email, *job.CreatorIdentity)
	})
}
