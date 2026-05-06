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

// makeImportTestUser inserts a creator user and returns it for context injection.
func makeImportTestUser(t *testing.T, h *Handlers, email string) *models.User {
	t.Helper()
	user := &models.User{
		ID:          uuid.New(),
		Email:       email,
		DisplayName: email,
		Status:      models.UserStatusActive,
		GlobalRole:  models.GlobalRoleCreator,
	}
	require.NoError(t, h.DB.CreateUser(context.Background(), user))
	return user
}

// seedGoogleMeetConnection inserts a GoogleMeetConnection for a creator with valid
// encrypted tokens (encrypted with the default key the handler reads when
// ENCRYPTION_KEY is unset). Returns the inserted connection.
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

func TestGoogleMeetImport_DisabledReturns404(t *testing.T) {
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	t.Setenv("ENABLE_GOOGLE_MEET", "")

	req := httptest.NewRequest(http.MethodPost, "/api/google-meet/import", nil)
	w := httptest.NewRecorder()
	h.GoogleMeetImport(w, req)
	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestGoogleMeetImport_NoAuthReturns401(t *testing.T) {
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	t.Setenv("ENABLE_GOOGLE_MEET", "true")

	body, _ := json.Marshal(GoogleMeetImportRequest{
		Title:            "Test",
		ConferenceRecord: "conferenceRecords/A",
		Recording:        "conferenceRecords/A/recordings/r1",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/google-meet/import", bytes.NewReader(body))
	w := httptest.NewRecorder()
	// RequireAuth rejects without session; calling the wrapper directly.
	h.RequireAuth(h.GoogleMeetImport)(w, req)
	require.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestGoogleMeetImport_MissingFieldsReturns400(t *testing.T) {
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	t.Setenv("ENABLE_GOOGLE_MEET", "true")

	creator := "alice@example.com"
	user := makeImportTestUser(t, h, creator)
	seedGoogleMeetConnection(t, h, creator)

	body, _ := json.Marshal(GoogleMeetImportRequest{Title: "X"}) // no conference_record/recording
	req := httptest.NewRequest(http.MethodPost, "/api/google-meet/import", bytes.NewReader(body))
	req.Header.Set("X-Creator-Identity", creator)
	req = req.WithContext(context.WithValue(req.Context(), userContextKey, user))
	w := httptest.NewRecorder()
	h.GoogleMeetImport(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestGoogleMeetImport_HappyPathReturns202AndCreatesJob(t *testing.T) {
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	t.Setenv("ENABLE_GOOGLE_MEET", "true")

	creator := "bob@example.com"
	user := makeImportTestUser(t, h, creator)
	seedGoogleMeetConnection(t, h, creator)

	body, _ := json.Marshal(GoogleMeetImportRequest{
		Title:            "Standup recording",
		ConferenceRecord: "conferenceRecords/A",
		Recording:        "conferenceRecords/A/recordings/r1",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/google-meet/import", bytes.NewReader(body))
	req.Header.Set("X-Creator-Identity", creator)
	req = req.WithContext(context.WithValue(req.Context(), userContextKey, user))
	w := httptest.NewRecorder()
	h.GoogleMeetImport(w, req)
	require.Equal(t, http.StatusAccepted, w.Code, w.Body.String())

	var resp GoogleMeetImportResponse
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	require.NotEmpty(t, resp.ID)
	require.NotEmpty(t, resp.JobID)
	assert.Equal(t, models.ProcessingStateQueued, resp.State)

	// Session row exists with SourceProvider=google_meet.
	sessID, err := uuid.Parse(resp.ID)
	require.NoError(t, err)
	sess, err := h.DB.GetSession(context.Background(), sessID)
	require.NoError(t, err)
	require.NotNil(t, sess)
	assert.Equal(t, models.SessionSourceGoogleMeet, sess.SourceProvider)
}

func TestGoogleMeetImport_DuplicateTitleReturns409(t *testing.T) {
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	t.Setenv("ENABLE_GOOGLE_MEET", "true")

	creator := "carol@example.com"
	user := makeImportTestUser(t, h, creator)
	seedGoogleMeetConnection(t, h, creator)

	body, _ := json.Marshal(GoogleMeetImportRequest{
		Title:            "Sprint Demo",
		ConferenceRecord: "conferenceRecords/A",
		Recording:        "conferenceRecords/A/recordings/r1",
	})
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodPost, "/api/google-meet/import", bytes.NewReader(body))
		req.Header.Set("X-Creator-Identity", creator)
		req = req.WithContext(context.WithValue(req.Context(), userContextKey, user))
		w := httptest.NewRecorder()
		h.GoogleMeetImport(w, req)
		if i == 0 {
			require.Equal(t, http.StatusAccepted, w.Code, w.Body.String())
		} else {
			assert.Equal(t, http.StatusConflict, w.Code, "second import with same title must return 409")
		}
	}
}
