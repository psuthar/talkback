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
	"github.com/psuthar/talkback/internal/database"
	"github.com/psuthar/talkback/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func createTestVideoSourceForTitle(t *testing.T, db *database.DB, sessionID uuid.UUID) *models.VideoSource {
	t.Helper()
	ctx := context.Background()
	artifact, err := db.CreateArtifact(ctx, sessionID, "test video artifact", nil)
	require.NoError(t, err)
	embedURL := "https://example.com/share/abc"
	vs := &models.VideoSource{
		ID:               uuid.New(),
		ArtifactID:       artifact.ID,
		SessionID:        sessionID,
		Provider:         "other",
		VideoURL:         embedURL,
		PlaybackMode:     "embed",
		EmbedURL:         &embedURL,
		TranscriptStatus: models.VideoTranscriptStatusReady,
	}
	err = db.CreateVideoSource(ctx, vs)
	require.NoError(t, err)
	return vs
}

func TestUpdateVideoSourceDisplayTitle(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	db := h.DB

	creatorName := "creator@example.com"
	session := &models.Session{
		ID:        uuid.New(),
		Title:     "Display Title Test Session",
		CreatedBy: &creatorName,
		Status:    models.SessionStatusOpen,
	}
	err := db.CreateSession(context.Background(), session)
	require.NoError(t, err)
	vs := createTestVideoSourceForTitle(t, db, session.ID)

	makeReq := func(sessionID, videoSourceID, segment string, body any) *http.Request {
		b, _ := json.Marshal(body)
		path := "/api/sessions/" + sessionID + "/video-sources/" + videoSourceID + "/" + segment
		req := httptest.NewRequest(http.MethodPatch, path, bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Current-User", creatorName)
		return req
	}

	t.Run("sets display title", func(t *testing.T) {
		req := makeReq(session.ID.String(), vs.ID.String(), "display-title", map[string]any{"display_title": "My Custom Title"})
		w := httptest.NewRecorder()
		h.UpdateVideoSourceDisplayTitle(w, req)
		assert.Equal(t, http.StatusOK, w.Code)

		got, err := db.GetVideoSourceByID(context.Background(), vs.ID)
		require.NoError(t, err)
		require.NotNil(t, got.DisplayTitle)
		assert.Equal(t, "My Custom Title", *got.DisplayTitle)
	})

	t.Run("clears display title when null", func(t *testing.T) {
		req := makeReq(session.ID.String(), vs.ID.String(), "display-title", map[string]any{"display_title": nil})
		w := httptest.NewRecorder()
		h.UpdateVideoSourceDisplayTitle(w, req)
		assert.Equal(t, http.StatusOK, w.Code)

		got, err := db.GetVideoSourceByID(context.Background(), vs.ID)
		require.NoError(t, err)
		assert.Nil(t, got.DisplayTitle)
	})

	t.Run("returns 403 for non-creator", func(t *testing.T) {
		b, _ := json.Marshal(map[string]any{"display_title": "Hacker"})
		path := "/api/sessions/" + session.ID.String() + "/video-sources/" + vs.ID.String() + "/display-title"
		req := httptest.NewRequest(http.MethodPatch, path, bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Current-User", "other@example.com")
		w := httptest.NewRecorder()
		h.UpdateVideoSourceDisplayTitle(w, req)
		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("returns 404 for unknown session", func(t *testing.T) {
		req := makeReq(uuid.New().String(), vs.ID.String(), "display-title", map[string]any{"display_title": "x"})
		w := httptest.NewRecorder()
		h.UpdateVideoSourceDisplayTitle(w, req)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("returns 404 for video source in wrong session", func(t *testing.T) {
		otherSession := &models.Session{ID: uuid.New(), Title: "Other", Status: models.SessionStatusOpen, CreatedBy: &creatorName}
		err := db.CreateSession(context.Background(), otherSession)
		require.NoError(t, err)
		req := makeReq(otherSession.ID.String(), vs.ID.String(), "display-title", map[string]any{"display_title": "x"})
		w := httptest.NewRecorder()
		h.UpdateVideoSourceDisplayTitle(w, req)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("returns 400 for invalid path segment", func(t *testing.T) {
		path := "/api/sessions/" + session.ID.String() + "/video-sources/" + vs.ID.String() + "/wrong-segment"
		req := httptest.NewRequest(http.MethodPatch, path, nil)
		req.Header.Set("X-Current-User", creatorName)
		w := httptest.NewRecorder()
		h.UpdateVideoSourceDisplayTitle(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	// SCRUM-400: SPA hits the non-/api path (SessionsRouter dispatch arm).
	// Prior to the fix, the handler rejected this with 400 "Invalid path"
	// because its path parser hard-coded /api as the leading segment.
	t.Run("accepts the non-/api path the SessionsRouter dispatches with (SCRUM-400)", func(t *testing.T) {
		b, _ := json.Marshal(map[string]any{"display_title": "Renamed via SPA path"})
		path := "/sessions/" + session.ID.String() + "/video-sources/" + vs.ID.String() + "/display-title"
		req := httptest.NewRequest(http.MethodPatch, path, bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Current-User", creatorName)
		w := httptest.NewRecorder()
		h.UpdateVideoSourceDisplayTitle(w, req)
		assert.Equal(t, http.StatusOK, w.Code, w.Body.String())

		got, err := db.GetVideoSourceByID(context.Background(), vs.ID)
		require.NoError(t, err)
		require.NotNil(t, got.DisplayTitle)
		assert.Equal(t, "Renamed via SPA path", *got.DisplayTitle)
	})

	// SCRUM-436: the SPA sends credentials: 'include' (cookie auth) with NO
	// X-Current-User header. The handler must accept this — i.e. fall back to
	// UserFromContext when matching session.CreatedBy. Without this fix the
	// non-admin session creator gets 403 on every rename even though they own
	// the session. This test reproduces the SPA's actual call shape.
	t.Run("matches cookie-auth user when X-Current-User absent (SCRUM-436)", func(t *testing.T) {
		// Non-admin creator user authenticated via the request context (cookie path).
		creator := &models.User{Email: creatorName, GlobalRole: models.GlobalRoleCreator}
		b, _ := json.Marshal(map[string]any{"display_title": "Renamed via cookie auth"})
		path := "/sessions/" + session.ID.String() + "/video-sources/" + vs.ID.String() + "/display-title"
		req := httptest.NewRequest(http.MethodPatch, path, bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
		// Deliberately NO X-Current-User header — this is the SPA's shape.
		req = req.WithContext(context.WithValue(req.Context(), userContextKey, creator))
		w := httptest.NewRecorder()
		h.UpdateVideoSourceDisplayTitle(w, req)
		assert.Equal(t, http.StatusOK, w.Code, w.Body.String())

		got, err := db.GetVideoSourceByID(context.Background(), vs.ID)
		require.NoError(t, err)
		require.NotNil(t, got.DisplayTitle)
		assert.Equal(t, "Renamed via cookie auth", *got.DisplayTitle)
	})

	// SCRUM-436: cookie-auth user that is NOT the creator and NOT admin still
	// gets rejected. Defends against an over-permissive fallback that would
	// let any logged-in user rename anyone's video.
	t.Run("rejects cookie-auth non-creator non-admin with 403 (SCRUM-436)", func(t *testing.T) {
		stranger := &models.User{Email: "stranger@example.com", GlobalRole: models.GlobalRoleParticipant}
		b, _ := json.Marshal(map[string]any{"display_title": "Hacker"})
		path := "/sessions/" + session.ID.String() + "/video-sources/" + vs.ID.String() + "/display-title"
		req := httptest.NewRequest(http.MethodPatch, path, bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(context.WithValue(req.Context(), userContextKey, stranger))
		w := httptest.NewRecorder()
		h.UpdateVideoSourceDisplayTitle(w, req)
		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	// SCRUM-438: load-bearing end-to-end test for the SPA's actual call shape.
	// The /sessions/... route this handler runs under is NOT wrapped in
	// RequireAuth/OptionalAuth in main.go (the SPA hits SessionsRouter
	// directly), so UserFromContext is always nil when the handler runs in
	// production. The SCRUM-436 cookie-auth tests above shortcut this by
	// pre-populating userContextKey — useful as a unit test for the inner
	// logic, but not load-bearing for the route-wiring miss this ticket
	// catches.
	//
	// This test creates a REAL user + login_session in the DB and attaches the
	// session cookie via http.Cookie. No X-Current-User header. No
	// userContextKey pre-population. The handler must do the cookie → session
	// → user lookup inline. Asserts 200 + persisted title.
	t.Run("creator with real cookie session and no header gets 200 (SCRUM-438)", func(t *testing.T) {
		ctx := context.Background()
		// Create a fresh user that will be the session creator for this sub-test
		// so we don't collide with the outer fixture's CreatedBy.
		creator := &models.User{
			ID:          uuid.New(),
			Email:       "cookie-creator-" + uuid.NewString() + "@example.com",
			DisplayName: "Cookie Creator",
			Status:      models.UserStatusActive,
			GlobalRole:  models.GlobalRoleCreator,
		}
		require.NoError(t, db.CreateUser(ctx, creator))
		ownedSession := &models.Session{
			ID:        uuid.New(),
			Title:     "Cookie-Owned Session " + uuid.NewString(),
			CreatedBy: &creator.Email,
			Status:    models.SessionStatusOpen,
		}
		require.NoError(t, db.CreateSession(ctx, ownedSession))
		ownedVS := createTestVideoSourceForTitle(t, db, ownedSession.ID)

		// Real login_session row + cookie attached to the request.
		loginSession := &models.LoginSession{
			ID:        uuid.New(),
			UserID:    creator.ID,
			CreatedAt: time.Now(),
			ExpiresAt: time.Now().Add(24 * time.Hour),
		}
		require.NoError(t, db.CreateLoginSession(ctx, loginSession))

		b, _ := json.Marshal(map[string]any{"display_title": "Renamed via real cookie"})
		path := "/sessions/" + ownedSession.ID.String() + "/video-sources/" + ownedVS.ID.String() + "/display-title"
		req := httptest.NewRequest(http.MethodPatch, path, bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
		// Deliberately NO X-Current-User header and NO context.WithValue
		// pre-population — only the cookie carries identity.
		req.AddCookie(&http.Cookie{Name: auth.Config.SessionCookieName, Value: loginSession.ID.String()})
		w := httptest.NewRecorder()
		h.UpdateVideoSourceDisplayTitle(w, req)
		assert.Equal(t, http.StatusOK, w.Code, w.Body.String())

		got, err := db.GetVideoSourceByID(ctx, ownedVS.ID)
		require.NoError(t, err)
		require.NotNil(t, got.DisplayTitle)
		assert.Equal(t, "Renamed via real cookie", *got.DisplayTitle)
	})
}
