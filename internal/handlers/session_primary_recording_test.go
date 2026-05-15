package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSessionPatchPrimaryRecording_AuthzAndCrossSession exercises every
// 4xx/200 branch of the SCRUM-412 endpoint.
func TestSessionPatchPrimaryRecording_AuthzAndCrossSession(t *testing.T) {
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

	sessionA := createTestSessionForHandlers(t, h.DB, "primary-recording session A")
	sessionB := createTestSessionForHandlers(t, h.DB, "primary-recording session B")
	require.NoError(t, h.DB.CreateSessionMembership(ctx, sessionA.ID, editor.ID, "creator", nil))
	require.NoError(t, h.DB.CreateSessionMembership(ctx, sessionB.ID, editor.ID, "creator", nil))

	// Helper: insert an artifact + two recordings into sessionA so we can
	// flip primary between them.
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

	patch := func(t *testing.T, sessionID string, user *models.User, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodPatch, "/api/sessions/"+sessionID+"/primary-recording", bytes.NewReader([]byte(body)))
		req.Header.Set("Content-Type", "application/json")
		if user != nil {
			req = req.WithContext(context.WithValue(req.Context(), userContextKey, user))
		}
		w := httptest.NewRecorder()
		h.SessionPatchPrimaryRecording(w, req)
		return w
	}

	t.Run("happy path: editor flips primary to a recording in their session", func(t *testing.T) {
		body := `{"video_source_id":"` + vsA1.String() + `"}`
		w := patch(t, sessionA.ID.String(), editor, body)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())

		var resp PrimaryRecordingResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Equal(t, vsA1.String(), resp.VideoSourceID)
		assert.Equal(t, string(models.VideoRolePrimary), resp.VideoRole)

		// DB reflects the new primary.
		var role *string
		require.NoError(t, h.DB.Pool.QueryRow(ctx, `SELECT video_role FROM video_sources WHERE id = $1`, vsA1).Scan(&role))
		require.NotNil(t, role)
		assert.Equal(t, "primary", *role)
	})

	t.Run("flipping primary demotes the previous primary", func(t *testing.T) {
		// vsA1 was set primary above; now flip to vsA2.
		body := `{"video_source_id":"` + vsA2.String() + `"}`
		w := patch(t, sessionA.ID.String(), editor, body)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())

		var roleA1, roleA2 *string
		require.NoError(t, h.DB.Pool.QueryRow(ctx, `SELECT video_role FROM video_sources WHERE id = $1`, vsA1).Scan(&roleA1))
		require.NoError(t, h.DB.Pool.QueryRow(ctx, `SELECT video_role FROM video_sources WHERE id = $1`, vsA2).Scan(&roleA2))
		require.NotNil(t, roleA1)
		require.NotNil(t, roleA2)
		assert.Equal(t, "secondary", *roleA1, "old primary must be demoted to secondary")
		assert.Equal(t, "primary", *roleA2)
	})

	t.Run("non-editor returns 403", func(t *testing.T) {
		body := `{"video_source_id":"` + vsA1.String() + `"}`
		w := patch(t, sessionA.ID.String(), outsider, body)
		assert.Equal(t, http.StatusForbidden, w.Code)
		assert.Contains(t, strings.ToLower(w.Body.String()), "editor")
	})

	t.Run("missing user in context returns 401", func(t *testing.T) {
		body := `{"video_source_id":"` + vsA1.String() + `"}`
		w := patch(t, sessionA.ID.String(), nil, body)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("cross-session video_source_id returns 400", func(t *testing.T) {
		// sessionB's recording sent against sessionA — must reject so editors
		// of session A cannot flip primary on recordings owned by another
		// session.
		body := `{"video_source_id":"` + vsB1.String() + `"}`
		w := patch(t, sessionA.ID.String(), editor, body)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, strings.ToLower(w.Body.String()), "session")
	})

	t.Run("missing session returns 404", func(t *testing.T) {
		body := `{"video_source_id":"` + vsA1.String() + `"}`
		w := patch(t, uuid.New().String(), editor, body)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("invalid session id returns 400", func(t *testing.T) {
		body := `{"video_source_id":"` + vsA1.String() + `"}`
		w := patch(t, "not-a-uuid", editor, body)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("invalid video_source_id body returns 400", func(t *testing.T) {
		w := patch(t, sessionA.ID.String(), editor, `{"video_source_id":"not-a-uuid"}`)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("malformed body returns 400", func(t *testing.T) {
		w := patch(t, sessionA.ID.String(), editor, `not json`)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("wrong method returns 405", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/sessions/"+sessionA.ID.String()+"/primary-recording", nil)
		req = req.WithContext(context.WithValue(req.Context(), userContextKey, editor))
		w := httptest.NewRecorder()
		h.SessionPatchPrimaryRecording(w, req)
		assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
	})

	t.Run("admin (global role) passes without explicit membership", func(t *testing.T) {
		admin := &models.User{
			ID:          uuid.New(),
			Email:       "admin-" + uuid.NewString() + "@example.com",
			DisplayName: "Admin",
			GlobalRole:  models.GlobalRoleAdmin,
			Status:      models.UserStatusActive,
		}
		require.NoError(t, h.DB.CreateUser(ctx, admin))
		body := `{"video_source_id":"` + vsA1.String() + `"}`
		w := patch(t, sessionA.ID.String(), admin, body)
		assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
	})
}
