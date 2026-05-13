package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
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
}
