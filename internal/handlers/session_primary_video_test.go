package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/auth"
	"github.com/psuthar/talkback/internal/database"
	"github.com/psuthar/talkback/internal/models"
	"github.com/psuthar/talkback/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// SCRUM-439: load-bearing tests for SetSessionPrimaryVideoSource and
// SetSessionPrimaryVideoArtifact. Both handlers have the same SCRUM-438
// route-wiring gap — their routes are not wrapped in RequireAuth, so the
// SPA's cookie-only call would have always returned 403 (for VideoSource)
// or, worse, succeeded without any authorization (for VideoArtifact).
//
// Each sub-test uses the SCRUM-438 discipline:
//   - Real User row + real LoginSession row + real http.Cookie attached
//   - NO X-Current-User header, NO context.WithValue userContextKey shortcut
//
// A future regression that breaks the inline cookie lookup or the ownership
// check fails these tests before merge.

func makePrimaryVideoTestSession(t *testing.T, db *database.DB, creatorEmail string) *models.Session {
	t.Helper()
	sess := &models.Session{
		ID:        uuid.New(),
		Title:     "SCRUM-439 Primary Video Test " + uuid.NewString(),
		CreatedBy: &creatorEmail,
		Status:    models.SessionStatusOpen,
	}
	require.NoError(t, db.CreateSession(context.Background(), sess))
	return sess
}

func makePrimaryVideoTestUser(t *testing.T, db *database.DB, email string, role models.GlobalRole) *models.User {
	t.Helper()
	u := &models.User{
		ID:          uuid.New(),
		Email:       email,
		DisplayName: email,
		Status:      models.UserStatusActive,
		GlobalRole:  role,
	}
	require.NoError(t, db.CreateUser(context.Background(), u))
	return u
}

func makePrimaryVideoTestCookie(t *testing.T, db *database.DB, userID uuid.UUID) *http.Cookie {
	t.Helper()
	ls := &models.LoginSession{
		ID:        uuid.New(),
		UserID:    userID,
		CreatedAt: time.Now(),
		ExpiresAt: time.Now().Add(24 * time.Hour),
	}
	require.NoError(t, db.CreateLoginSession(context.Background(), ls))
	return &http.Cookie{Name: auth.Config.SessionCookieName, Value: ls.ID.String()}
}

func makePrimaryVideoTestVideoSource(t *testing.T, db *database.DB, sessionID uuid.UUID) *models.VideoSource {
	t.Helper()
	artifact, err := db.CreateArtifact(context.Background(), sessionID, "primary-video test artifact", nil)
	require.NoError(t, err)
	embedURL := "https://example.com/share/" + uuid.NewString()
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
	require.NoError(t, db.CreateVideoSource(context.Background(), vs))
	return vs
}

func makePrimaryVideoTestFileArtifact(t *testing.T, db *database.DB, sessionID uuid.UUID) *models.FileArtifact {
	t.Helper()
	filename := "primary-video-test.mp4"
	size := int64(1024)
	relKey := filepath.ToSlash(filepath.Join(storage.SessionStorageRoot, sessionID.String(), "videos", filename))
	fa := &models.FileArtifact{
		ID:              uuid.New(),
		SessionID:       &sessionID,
		Kind:            models.FileArtifactKindVideo,
		Filename:        &filename,
		ContentType:     "video/mp4",
		SizeBytes:       &size,
		StorageProvider: "local",
		StorageBucket:   "local",
		StorageKey:      relKey,
		Status:          models.FileArtifactStatusReady,
	}
	require.NoError(t, db.CreateFileArtifact(context.Background(), fa))
	return fa
}

// TestSetSessionPrimaryVideoSource_CookieAuth verifies SCRUM-439 Finding 1:
// the handler now resolves the cookie-authenticated user inline (since the
// /api/sessions/.../set-primary-video route is not wrapped in RequireAuth)
// and accepts a non-admin session creator without an X-Current-User header.
func TestSetSessionPrimaryVideoSource_CookieAuth(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	db := h.DB

	creator := makePrimaryVideoTestUser(t, db, "primary-source-creator-"+uuid.NewString()+"@example.com", models.GlobalRoleCreator)
	session := makePrimaryVideoTestSession(t, db, creator.Email)
	vs := makePrimaryVideoTestVideoSource(t, db, session.ID)

	makeReq := func(cookie *http.Cookie) *http.Request {
		body, _ := json.Marshal(SetPrimaryVideoSourceRequest{VideoSourceID: vs.ID.String()})
		path := "/api/sessions/" + session.ID.String() + "/set-primary-video"
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		// SCRUM-439: deliberately NO X-Current-User header — SPA shape.
		if cookie != nil {
			req.AddCookie(cookie)
		}
		return req
	}

	t.Run("creator with real cookie session and no header gets 200 (SCRUM-439 Finding 1)", func(t *testing.T) {
		cookie := makePrimaryVideoTestCookie(t, db, creator.ID)
		w := httptest.NewRecorder()
		h.SetSessionPrimaryVideoSource(w, makeReq(cookie))
		assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
	})

	t.Run("admin non-creator with real cookie session gets 200 (admin override)", func(t *testing.T) {
		// Reset the primary so we can flip it again.
		require.NoError(t, db.SetVideoSourceVideoRole(context.Background(), session.ID, vs.ID, models.VideoRoleSecondary))
		admin := makePrimaryVideoTestUser(t, db, "primary-source-admin-"+uuid.NewString()+"@example.com", models.GlobalRoleAdmin)
		cookie := makePrimaryVideoTestCookie(t, db, admin.ID)
		w := httptest.NewRecorder()
		h.SetSessionPrimaryVideoSource(w, makeReq(cookie))
		assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
	})

	t.Run("non-creator non-admin with real cookie gets 403", func(t *testing.T) {
		stranger := makePrimaryVideoTestUser(t, db, "primary-source-stranger-"+uuid.NewString()+"@example.com", models.GlobalRoleParticipant)
		cookie := makePrimaryVideoTestCookie(t, db, stranger.ID)
		w := httptest.NewRecorder()
		h.SetSessionPrimaryVideoSource(w, makeReq(cookie))
		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("no cookie and no header returns 403", func(t *testing.T) {
		w := httptest.NewRecorder()
		h.SetSessionPrimaryVideoSource(w, makeReq(nil))
		assert.Equal(t, http.StatusForbidden, w.Code)
	})
}

// TestSetSessionPrimaryVideoArtifact_CookieAuth verifies SCRUM-439 Finding 2:
// this handler previously had NO ownership check at all. Anyone who knew a
// session_id + artifact_id could swap that session's primary video. The new
// check uses the same SCRUM-438 fallback chain as SetSessionPrimaryVideoSource.
func TestSetSessionPrimaryVideoArtifact_CookieAuth(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	db := h.DB

	creator := makePrimaryVideoTestUser(t, db, "primary-artifact-creator-"+uuid.NewString()+"@example.com", models.GlobalRoleCreator)
	session := makePrimaryVideoTestSession(t, db, creator.Email)
	fa := makePrimaryVideoTestFileArtifact(t, db, session.ID)

	makeReq := func(cookie *http.Cookie) *http.Request {
		body, _ := json.Marshal(SetPrimaryVideoArtifactRequest{ArtifactID: fa.ID.String()})
		path := "/api/sessions/" + session.ID.String() + "/primary-video-artifact"
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		// SCRUM-439: deliberately NO X-Current-User header — SPA shape.
		if cookie != nil {
			req.AddCookie(cookie)
		}
		return req
	}

	t.Run("creator with real cookie session and no header gets 200 (SCRUM-439 Finding 2)", func(t *testing.T) {
		cookie := makePrimaryVideoTestCookie(t, db, creator.ID)
		w := httptest.NewRecorder()
		h.SetSessionPrimaryVideoArtifact(w, makeReq(cookie))
		assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
	})

	t.Run("admin non-creator with real cookie session gets 200 (admin override)", func(t *testing.T) {
		admin := makePrimaryVideoTestUser(t, db, "primary-artifact-admin-"+uuid.NewString()+"@example.com", models.GlobalRoleAdmin)
		cookie := makePrimaryVideoTestCookie(t, db, admin.ID)
		w := httptest.NewRecorder()
		h.SetSessionPrimaryVideoArtifact(w, makeReq(cookie))
		assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
	})

	// This is the critical regression case — pre-fix, this returned 200 and
	// the stranger's call mutated session state. Post-fix, the ownership check
	// rejects it with 403. If this test ever passes with 200 again, the auth
	// gap is back.
	t.Run("non-creator non-admin with real cookie gets 403 (was the security gap)", func(t *testing.T) {
		stranger := makePrimaryVideoTestUser(t, db, "primary-artifact-stranger-"+uuid.NewString()+"@example.com", models.GlobalRoleParticipant)
		cookie := makePrimaryVideoTestCookie(t, db, stranger.ID)
		w := httptest.NewRecorder()
		h.SetSessionPrimaryVideoArtifact(w, makeReq(cookie))
		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("no cookie and no header returns 403 (was completely unprotected)", func(t *testing.T) {
		w := httptest.NewRecorder()
		h.SetSessionPrimaryVideoArtifact(w, makeReq(nil))
		assert.Equal(t, http.StatusForbidden, w.Code)
	})
}
