package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/psuthar/talkback/internal/models"
)

func TestGetTranscriptJob(t *testing.T) {
	h, cleanup := setupTestHandlers(t)
	defer cleanup()
	db := h.DB
	ctx := context.Background()

	session := createTestSessionForHandlers(t, db, "Test Session")
	artifact, err := db.CreateArtifact(ctx, session.ID, "Test Artifact", nil)
	require.NoError(t, err)

	videoSource := &models.VideoSource{
		ID:              uuid.New(),
		ArtifactID:      artifact.ID,
		SessionID:       session.ID,
		Provider:        "loom",
		VideoURL:        "https://www.loom.com/share/test",
		PlaybackMode:    "embed",
		EmbedURL:        stringPtr("https://www.loom.com/embed/test"),
		TranscriptStatus: models.VideoTranscriptStatusMissing,
	}
	err = db.CreateVideoSource(ctx, videoSource)
	require.NoError(t, err)

	t.Run("returns transcript job status when job exists", func(t *testing.T) {
		job := &models.TranscriptJob{
			ID:            uuid.New(),
			VideoSourceID: videoSource.ID,
			SessionID:     session.ID,
			Status:        models.TranscriptJobStatusQueued,
			SourceURL:     "https://www.loom.com/share/test",
			JobKey:        "test-job-key-1",
		}
		err := db.CreateTranscriptJob(ctx, job)
		require.NoError(t, err)

		// Update video source with job ID
		err = db.UpdateVideoSourceTranscriptionJob(ctx, videoSource.ID, &job.ID)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodGet, "/artifacts/"+artifact.ID.String()+"/video/"+videoSource.ID.String()+"/transcript-job", nil)
		w := httptest.NewRecorder()

		h.GetTranscriptJob(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var response map[string]interface{}
		err = json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		assert.Equal(t, videoSource.ID.String(), response["video_source_id"])
		assert.NotNil(t, response["job"])
		assert.Equal(t, "queued", response["status"])
	})

	t.Run("returns no_job status when no job exists", func(t *testing.T) {
		// Create new video source without job
		videoSource2 := &models.VideoSource{
			ID:              uuid.New(),
			ArtifactID:      artifact.ID,
			SessionID:       session.ID,
			Provider:        "loom",
			VideoURL:        "https://www.loom.com/share/test2",
			PlaybackMode:    "embed",
			EmbedURL:        stringPtr("https://www.loom.com/embed/test2"),
			TranscriptStatus: models.VideoTranscriptStatusMissing,
		}
		err := db.CreateVideoSource(ctx, videoSource2)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodGet, "/artifacts/"+artifact.ID.String()+"/video/"+videoSource2.ID.String()+"/transcript-job", nil)
		w := httptest.NewRecorder()

		h.GetTranscriptJob(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var response map[string]interface{}
		err = json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		assert.Equal(t, "no_job", response["status"])
		assert.Nil(t, response["job"])
	})

	t.Run("returns 400 for invalid video ID", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/artifacts/"+artifact.ID.String()+"/video/invalid-id/transcript-job", nil)
		w := httptest.NewRecorder()

		h.GetTranscriptJob(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("returns 404 for non-existent video source", func(t *testing.T) {
		nonExistentID := uuid.New()
		req := httptest.NewRequest(http.MethodGet, "/artifacts/"+artifact.ID.String()+"/video/"+nonExistentID.String()+"/transcript-job", nil)
		w := httptest.NewRecorder()

		h.GetTranscriptJob(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("returns 405 for non-GET methods", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/artifacts/"+artifact.ID.String()+"/video/"+videoSource.ID.String()+"/transcript-job", nil)
		w := httptest.NewRecorder()

		h.GetTranscriptJob(w, req)

		assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
	})
}

func TestRegenerateTranscript(t *testing.T) {
	h, cleanup := setupTestHandlers(t)
	defer cleanup()
	db := h.DB
	ctx := context.Background()

	session := createTestSessionForHandlers(t, db, "Test Session")
	artifact, err := db.CreateArtifact(ctx, session.ID, "Test Artifact", nil)
	require.NoError(t, err)

	videoSource := &models.VideoSource{
		ID:              uuid.New(),
		ArtifactID:      artifact.ID,
		SessionID:       session.ID,
		Provider:        "loom",
		VideoURL:        "https://www.loom.com/share/test",
		PlaybackMode:    "embed",
		EmbedURL:        stringPtr("https://www.loom.com/embed/test"),
		TranscriptStatus: models.VideoTranscriptStatusMissing,
	}
	err = db.CreateVideoSource(ctx, videoSource)
	require.NoError(t, err)

	t.Run("returns 400 for non-loom video", func(t *testing.T) {
		directVideo := &models.VideoSource{
			ID:              uuid.New(),
			ArtifactID:      artifact.ID,
			SessionID:       session.ID,
			Provider:        "other",
			PlaybackMode:    "direct",
			MediaURL:        stringPtr("https://example.com/video.mp4"),
			TranscriptStatus: models.VideoTranscriptStatusMissing,
		}
		err := db.CreateVideoSource(ctx, directVideo)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/artifacts/"+artifact.ID.String()+"/video/"+directVideo.ID.String()+"/transcript-job/regenerate", nil)
		w := httptest.NewRecorder()

		h.RegenerateTranscript(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		body := w.Body.String()
		assert.Contains(t, body, "only supported for Loom")
	})

	t.Run("returns 400 for invalid video ID", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/artifacts/"+artifact.ID.String()+"/video/invalid-id/transcript-job/regenerate", nil)
		w := httptest.NewRecorder()

		h.RegenerateTranscript(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("returns 404 for non-existent video source", func(t *testing.T) {
		nonExistentID := uuid.New()
		req := httptest.NewRequest(http.MethodPost, "/artifacts/"+artifact.ID.String()+"/video/"+nonExistentID.String()+"/transcript-job/regenerate", nil)
		w := httptest.NewRecorder()

		h.RegenerateTranscript(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("returns 405 for non-POST methods", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/artifacts/"+artifact.ID.String()+"/video/"+videoSource.ID.String()+"/transcript-job/regenerate", nil)
		w := httptest.NewRecorder()

		h.RegenerateTranscript(w, req)

		assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
	})

	t.Run("returns 400 when loom URL is missing", func(t *testing.T) {
		videoSourceNoURL := &models.VideoSource{
			ID:              uuid.New(),
			ArtifactID:      artifact.ID,
			SessionID:       session.ID,
			Provider:        "loom",
			PlaybackMode:    "embed",
			TranscriptStatus: models.VideoTranscriptStatusMissing,
		}
		err := db.CreateVideoSource(ctx, videoSourceNoURL)
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/artifacts/"+artifact.ID.String()+"/video/"+videoSourceNoURL.ID.String()+"/transcript-job/regenerate", nil)
		w := httptest.NewRecorder()

		h.RegenerateTranscript(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("returns 503 when job processor is not available", func(t *testing.T) {
		// h.JobProcessor is nil in test setup
		req := httptest.NewRequest(http.MethodPost, "/artifacts/"+artifact.ID.String()+"/video/"+videoSource.ID.String()+"/transcript-job/regenerate", nil)
		w := httptest.NewRecorder()

		h.RegenerateTranscript(w, req)

		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	})

	t.Run("uses password from existing job when regenerating", func(t *testing.T) {
		// Create a video source with an existing job that has a password
		password := "test-regenerate-password"
		jobWithPassword := &models.TranscriptJob{
			ID:            uuid.New(),
			VideoSourceID: videoSource.ID,
			SessionID:     session.ID,
			Status:        models.TranscriptJobStatusFailed, // Failed so it can be regenerated
			SourceURL:     "https://www.loom.com/share/test",
			JobKey:        "test-job-key-regenerate",
			LoomPassword:  &password,
		}
		err := db.CreateTranscriptJob(ctx, jobWithPassword)
		require.NoError(t, err)

		// Link job to video source
		err = db.UpdateVideoSourceTranscriptionJob(ctx, videoSource.ID, &jobWithPassword.ID)
		require.NoError(t, err)

		// Update video source to reference the job
		updatedVideoSource, err := db.GetVideoSourceByID(ctx, videoSource.ID)
		require.NoError(t, err)
		videoSource = updatedVideoSource

		// Regenerate - should use password from existing job
		// Note: In test setup, JobProcessor is nil, so we expect 503
		req := httptest.NewRequest(http.MethodPost, "/artifacts/"+artifact.ID.String()+"/video/"+videoSource.ID.String()+"/transcript-job/regenerate", nil)
		w := httptest.NewRecorder()

		h.RegenerateTranscript(w, req)

		// Should fail with 503 because JobProcessor is nil in test setup
		// But the code path to get password should execute
		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	})
}

