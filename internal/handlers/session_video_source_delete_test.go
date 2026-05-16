package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDeleteSessionVideoSource_AuthzAndCrossSession exercises every branch
// of the SCRUM-426 DELETE endpoint:
//   - 204 happy path (editor removes a recording in their session)
//   - 204 idempotent (deleting an already-gone row)
//   - 403 non-editor
//   - 401 missing user
//   - 400 cross-session video_source_id
//   - 400 invalid UUIDs
//   - 404 missing session
//   - 405 wrong method
func TestDeleteSessionVideoSource_AuthzAndCrossSession(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	ctx := context.Background()

	editor := &models.User{
		ID:          uuid.New(),
		Email:       "editor-" + uuid.NewString() + "@example.com",
		DisplayName: "Editor",
		GlobalRole:  models.GlobalRoleCreator,
		Status:      models.UserStatusActive,
	}
	require.NoError(t, h.DB.CreateUser(ctx, editor))

	outsider := &models.User{
		ID:          uuid.New(),
		Email:       "outsider-" + uuid.NewString() + "@example.com",
		DisplayName: "Outsider",
		GlobalRole:  models.GlobalRoleCreator,
		Status:      models.UserStatusActive,
	}
	require.NoError(t, h.DB.CreateUser(ctx, outsider))

	sessionA := createTestSessionForHandlers(t, h.DB, "vs-delete session A")
	sessionB := createTestSessionForHandlers(t, h.DB, "vs-delete session B")
	require.NoError(t, h.DB.CreateSessionMembership(ctx, sessionA.ID, editor.ID, "creator", nil))
	require.NoError(t, h.DB.CreateSessionMembership(ctx, sessionB.ID, editor.ID, "creator", nil))

	insertArtifact := func(t *testing.T, sessionID uuid.UUID) uuid.UUID {
		t.Helper()
		id := uuid.New()
		_, err := h.DB.Pool.Exec(ctx, `
			INSERT INTO artifacts (id, session_id, title, status, created_at, updated_at)
			VALUES ($1, $2, 'a', 'ready', now(), now())
		`, id, sessionID)
		require.NoError(t, err)
		return id
	}
	insertRecording := func(t *testing.T, sessionID, artifactID uuid.UUID, label string) uuid.UUID {
		t.Helper()
		id := uuid.New()
		_, err := h.DB.Pool.Exec(ctx, `
			INSERT INTO video_sources (id, artifact_id, session_id, provider, video_url, source_type)
			VALUES ($1, $2, $3, 'zoom', $4, 'upload')
		`, id, artifactID, sessionID, "https://example.com/"+label+".mp4")
		require.NoError(t, err)
		return id
	}

	artifactA := insertArtifact(t, sessionA.ID)
	vsA1 := insertRecording(t, sessionA.ID, artifactA, "a1")
	vsA2 := insertRecording(t, sessionA.ID, artifactA, "a2")

	artifactB := insertArtifact(t, sessionB.ID)
	vsB1 := insertRecording(t, sessionB.ID, artifactB, "b1")

	del := func(t *testing.T, sessionID, vsID string, user *models.User) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodDelete, "/api/sessions/"+sessionID+"/video-sources/"+vsID, nil)
		if user != nil {
			req = req.WithContext(context.WithValue(req.Context(), userContextKey, user))
		}
		w := httptest.NewRecorder()
		h.DeleteSessionVideoSource(w, req)
		return w
	}

	t.Run("happy path: editor removes recording in their session (204)", func(t *testing.T) {
		w := del(t, sessionA.ID.String(), vsA1.String(), editor)
		require.Equal(t, http.StatusNoContent, w.Code, w.Body.String())

		// Row is gone.
		var n int
		require.NoError(t, h.DB.Pool.QueryRow(ctx, `SELECT count(*) FROM video_sources WHERE id = $1`, vsA1).Scan(&n))
		assert.Equal(t, 0, n)
	})

	t.Run("idempotent: deleting an already-gone row returns 204", func(t *testing.T) {
		w := del(t, sessionA.ID.String(), vsA1.String(), editor)
		assert.Equal(t, http.StatusNoContent, w.Code)
	})

	t.Run("non-editor returns 403", func(t *testing.T) {
		w := del(t, sessionA.ID.String(), vsA2.String(), outsider)
		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("missing user in context returns 401", func(t *testing.T) {
		w := del(t, sessionA.ID.String(), vsA2.String(), nil)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("cross-session video_source_id returns 400", func(t *testing.T) {
		// vsB1 belongs to sessionB; sending it against sessionA must reject.
		w := del(t, sessionA.ID.String(), vsB1.String(), editor)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("missing session returns 404", func(t *testing.T) {
		w := del(t, uuid.New().String(), vsA2.String(), editor)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("invalid session id returns 400", func(t *testing.T) {
		w := del(t, "not-a-uuid", vsA2.String(), editor)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("invalid video_source_id returns 400", func(t *testing.T) {
		w := del(t, sessionA.ID.String(), "not-a-uuid", editor)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("wrong method returns 405", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/sessions/"+sessionA.ID.String()+"/video-sources/"+vsA2.String(), nil)
		req = req.WithContext(context.WithValue(req.Context(), userContextKey, editor))
		w := httptest.NewRecorder()
		h.DeleteSessionVideoSource(w, req)
		assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
	})
}
