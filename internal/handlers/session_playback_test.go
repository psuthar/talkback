package handlers

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// SCRUM-472: when a non-upload VideoSource (e.g. Teams or Meet recording, which
// the pipelines persist as source_type=embed_url with a Ready file_artifact)
// has its file_artifact_id pointing at a Ready local/R2 video, the per-recording
// stream endpoint must serve from that file_artifact. Before SCRUM-472 the
// branch only covered source_type=upload, so non-primary Teams/Meet selections
// fell through to the legacy Zoom-proxy code path and never rendered.
func TestZoomVideoStream_FileArtifactBacked(t *testing.T) {
	// Not t.Parallel: t.Setenv is incompatible with parallel tests; this test
	// mutates TALKBACK_UPLOAD_ROOT so the local-storage branch resolves to a
	// per-test tmpdir.
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	ctx := context.Background()

	// Isolate filesystem so the test's local file lookup resolves under tmpdir.
	root := t.TempDir()
	t.Setenv("TALKBACK_UPLOAD_ROOT", root)

	session := createTestSessionForHandlers(t, h.DB, "SCRUM-472 multi-recording stream")
	artifact, err := h.DB.CreateArtifact(ctx, session.ID, "Test Artifact", nil)
	require.NoError(t, err)

	// Write a fake mp4 body to disk under TALKBACK_UPLOAD_ROOT/<storage_key>.
	storageKey := "sessions/" + session.ID.String() + "/videos/teams-recording.mp4"
	abs := filepath.Join(root, filepath.FromSlash(storageKey))
	require.NoError(t, os.MkdirAll(filepath.Dir(abs), 0o755))
	body := []byte("PRETEND-MP4-BYTES-FOR-TEAMS-RECORDING")
	require.NoError(t, os.WriteFile(abs, body, 0o644))

	filename := "teams-recording.mp4"
	size := int64(len(body))
	fa := &models.FileArtifact{
		ID:              uuid.New(),
		SessionID:       &session.ID,
		Kind:            models.FileArtifactKindVideo,
		Filename:        &filename,
		ContentType:     "video/mp4",
		SizeBytes:       &size,
		StorageProvider: "local",
		StorageBucket:   "local",
		StorageKey:      storageKey,
		Status:          models.FileArtifactStatusReady,
	}
	require.NoError(t, h.DB.CreateFileArtifact(ctx, fa))

	t.Run("non-upload VideoSource with Ready file_artifact streams the file", func(t *testing.T) {
		vs := &models.VideoSource{
			ID:             uuid.New(),
			ArtifactID:     artifact.ID,
			SessionID:      session.ID,
			Provider:       "teams", // not Zoom — used to hit the legacy proxy path
			VideoURL:       "https://graph.microsoft.com/v1.0/.../playbackUrl",
			PlaybackMode:     "embed",
			SourceType:       models.VideoSourceTypeEmbedURL,
			TranscriptStatus: models.VideoTranscriptStatusReady,
			FileArtifactID:   &fa.ID,
		}
		require.NoError(t, h.DB.CreateVideoSource(ctx, vs))

		path := "/sessions/" + session.ID.String() + "/video-sources/" + vs.ID.String() + "/stream"
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		h.ZoomVideoStream(w, req)

		resp := w.Result()
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "video/mp4", resp.Header.Get("Content-Type"))
		got, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		assert.True(t, bytes.Equal(got, body), "served body must equal the file_artifact's on-disk bytes")
	})

	t.Run("same flow works for source_type=embed_url with Meet provider", func(t *testing.T) {
		vs := &models.VideoSource{
			ID:             uuid.New(),
			ArtifactID:     artifact.ID,
			SessionID:      session.ID,
			Provider:       "google_meet",
			VideoURL:       "https://drive.google.com/file/d/.../view",
			PlaybackMode:     "embed",
			SourceType:       models.VideoSourceTypeEmbedURL,
			TranscriptStatus: models.VideoTranscriptStatusReady,
			FileArtifactID:   &fa.ID, // reuse the same fa — both recordings point at it for the test
		}
		require.NoError(t, h.DB.CreateVideoSource(ctx, vs))

		path := "/sessions/" + session.ID.String() + "/video-sources/" + vs.ID.String() + "/stream"
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		h.ZoomVideoStream(w, req)

		resp := w.Result()
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		got, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		assert.True(t, bytes.Equal(got, body))
	})

	t.Run("VideoSource without file_artifact_id does not enter the new branch", func(t *testing.T) {
		// No FileArtifactID and not source_type=upload → must fall through to
		// the legacy path. We don't assert exact downstream behavior (it 410s
		// when session has a primary, or 403s for legacy Zoom). We only assert
		// the new branch did NOT serve our body — guarding against a regression
		// where the new code accidentally serves arbitrary file_artifacts.
		vs := &models.VideoSource{
			ID:           uuid.New(),
			ArtifactID:   artifact.ID,
			SessionID:    session.ID,
			Provider:     "teams",
			VideoURL:     "https://graph.microsoft.com/v1.0/.../playbackUrl",
			PlaybackMode:     "embed",
			SourceType:       models.VideoSourceTypeEmbedURL,
			TranscriptStatus: models.VideoTranscriptStatusReady,
			// FileArtifactID: nil
		}
		require.NoError(t, h.DB.CreateVideoSource(ctx, vs))

		path := "/sessions/" + session.ID.String() + "/video-sources/" + vs.ID.String() + "/stream"
		req := httptest.NewRequest(http.MethodGet, path, nil)
		w := httptest.NewRecorder()
		h.ZoomVideoStream(w, req)

		resp := w.Result()
		defer resp.Body.Close()
		assert.NotEqual(t, http.StatusOK, resp.StatusCode, "must not stream when file_artifact_id is nil")
		got, _ := io.ReadAll(resp.Body)
		assert.False(t, bytes.Equal(got, body), "must not leak the file_artifact bytes")
	})
}
