package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/psuthar/talkback/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUploadVideoFileCreatesFileArtifact (SCRUM-295) verifies that a /sessions/:id/video/upload
// POST creates not only the legacy artifacts/video_sources rows but also a
// file_artifacts row (status=ready) linked to the new video_source via
// file_artifact_id. This is the linkage the SCRUM-271/272 primary system
// uses for kind="video" PATCHes.
func TestUploadVideoFileCreatesFileArtifact(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	session := createTestSessionForHandlers(t, h.DB, "SCRUM-295 video upload")
	ctx := context.Background()

	// Build a multipart body with a tiny "video" payload — content doesn't
	// have to be a real MP4, just have an .mp4 extension so the handler
	// accepts it via the extension fallback.
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	header := make(map[string][]string)
	header["Content-Disposition"] = []string{`form-data; name="file"; filename="test.mp4"`}
	header["Content-Type"] = []string{"video/mp4"}
	part, err := mw.CreatePart(header)
	require.NoError(t, err)
	_, err = io.Copy(part, strings.NewReader("not-a-real-mp4-but-fine-for-this-test"))
	require.NoError(t, err)
	require.NoError(t, mw.Close())

	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/sessions/%s/video/upload", session.ID), &buf)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	w := httptest.NewRecorder()

	h.UploadVideoFile(w, req)

	require.Equal(t, http.StatusCreated, w.Code, "upload should return 201; body=%s", w.Body.String())

	var resp UploadVideoFileResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.NotNil(t, resp.VideoSource, "response should include video_source")

	// SCRUM-295: VideoSource.FileArtifactID is the new linkage column.
	require.NotNil(t, resp.VideoSource.FileArtifactID, "expected video_sources.file_artifact_id to be set after upload")

	// And the file_artifacts row should be looked up successfully and have
	// the expected fields.
	fa, err := h.DB.GetFileArtifactByID(ctx, *resp.VideoSource.FileArtifactID)
	require.NoError(t, err)
	require.NotNil(t, fa, "file_artifact row must exist")
	assert.Equal(t, models.FileArtifactKindVideo, fa.Kind)
	assert.Equal(t, models.FileArtifactStatusReady, fa.Status, "file_artifact must be born ready so SCRUM-272 PATCH primary kind=video does not fail PRIMARY_NOT_READY")
	require.NotNil(t, fa.SessionID)
	assert.Equal(t, session.ID, *fa.SessionID)
	assert.NotEmpty(t, fa.StorageKey, "storage_key must be non-empty for downstream playback / R2 lookup")
	assert.NotEmpty(t, fa.StorageProvider, "storage_provider must be set")
	assert.NotEmpty(t, fa.StorageBucket, "storage_bucket must be set (NOT NULL constraint)")
	require.NotNil(t, fa.Filename)
	assert.Equal(t, "test.mp4", *fa.Filename)
}
