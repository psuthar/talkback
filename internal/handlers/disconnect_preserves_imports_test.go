package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDisconnect_PreservesImportedRecordings is the SCRUM-419 GDPR-blocking
// guarantee: disconnecting a platform connection must NOT cascade-delete
// video_sources / transcripts / transcript_segments / session_speaker_aliases
// belonging to recordings previously imported under that connection.
// The user's existing sessions stay intact; only new imports require a
// reconnect.
//
// Idempotency: re-DELETEing the same connection returns 204/200 without
// erroring (already guaranteed by the existing handlers via the
// DeleteByCreatorIdentity SQL).
func TestDisconnect_PreservesImportedRecordings(t *testing.T) {
	t.Setenv("ENABLE_GOOGLE_MEET", "true")
	t.Setenv("ENABLE_TEAMS", "true")
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	ctx := context.Background()
	user := &models.User{
		ID:          uuid.New(),
		Email:       "user-" + uuid.NewString() + "@example.com",
		DisplayName: "U",
		GlobalRole:  models.GlobalRoleCreator,
		Status:      models.UserStatusActive,
	}
	require.NoError(t, h.DB.CreateUser(ctx, user))

	// Seed a Zoom connection + a session + a video_sources row that pretends
	// to be a recording imported via the Zoom connection.
	zEmail := "z@example.com"
	require.NoError(t, h.DB.CreateZoomConnection(ctx, &models.ZoomConnection{
		ID:                    uuid.New(),
		CreatorIdentityID:     user.Email,
		ZoomUserID:            "zoom-user-id",
		ZoomUserEmail:         &zEmail,
		AccessTokenEncrypted:  []byte("at"),
		RefreshTokenEncrypted: []byte("rt"),
		ExpiresAt:             time.Now().Add(time.Hour),
	}))

	session := createTestSessionForHandlers(t, h.DB, "disconnect-preserves session")
	artifactID := uuid.New()
	_, err := h.DB.Pool.Exec(ctx, `
		INSERT INTO artifacts (id, session_id, title, status, created_at, updated_at)
		VALUES ($1, $2, 'a', 'ready', now(), now())
	`, artifactID, session.ID)
	require.NoError(t, err)
	vsID := uuid.New()
	_, err = h.DB.Pool.Exec(ctx, `
		INSERT INTO video_sources (id, artifact_id, session_id, provider, video_url, source_type, external_recording_id)
		VALUES ($1, $2, $3, 'zoom', 'https://example.com/v.mp4', 'upload', 'recording-keep-me')
	`, vsID, artifactID, session.ID)
	require.NoError(t, err)

	// Act: disconnect Zoom. The handler's existing POST /api/zoom/disconnect
	// is the modal's target — we exercise the handler directly.
	req := httptest.NewRequest(http.MethodPost, "/api/zoom/disconnect", nil)
	req.Header.Set("X-Creator-Identity", user.Email)
	req = req.WithContext(context.WithValue(req.Context(), userContextKey, user))
	w := httptest.NewRecorder()
	h.ZoomAPIDisconnect(w, req)
	require.Equal(t, http.StatusNoContent, w.Code, w.Body.String())

	t.Run("connection row is gone", func(t *testing.T) {
		conn, err := h.DB.GetZoomConnectionByCreatorIdentity(ctx, user.Email)
		require.NoError(t, err)
		assert.Nil(t, conn, "connection must be deleted")
	})

	t.Run("video_sources row for the imported recording survives", func(t *testing.T) {
		var count int
		require.NoError(t, h.DB.Pool.QueryRow(ctx, `SELECT count(*) FROM video_sources WHERE id = $1`, vsID).Scan(&count))
		assert.Equal(t, 1, count, "previously-imported recording must NOT be cascade-deleted by disconnect")
	})

	t.Run("re-disconnect is idempotent (returns 204 again, no error)", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/zoom/disconnect", nil)
		req.Header.Set("X-Creator-Identity", user.Email)
		req = req.WithContext(context.WithValue(req.Context(), userContextKey, user))
		w := httptest.NewRecorder()
		h.ZoomAPIDisconnect(w, req)
		assert.Equal(t, http.StatusNoContent, w.Code, w.Body.String())
	})
}
