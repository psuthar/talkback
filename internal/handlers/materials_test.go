package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUploadMaterial(t *testing.T) {
	h, cleanup := setupTestHandlers(t)
	defer cleanup()
	db := h.DB
	ctx := context.Background()

	// Create a session and artifact for testing
	session := createTestSessionForHandlers(t, h.DB, "Test Session")
	artifact, err := db.CreateArtifact(ctx, session.ID, "Material Upload Test", nil)
	require.NoError(t, err)

	t.Run("uploads material file successfully", func(t *testing.T) {
		// Create multipart form with file
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		
		// Add file field
		fileWriter, err := writer.CreateFormFile("file", "test.txt")
		require.NoError(t, err)
		_, err = fileWriter.Write([]byte("This is test content"))
		require.NoError(t, err)
		
		// Add kind field
		err = writer.WriteField("kind", "document")
		require.NoError(t, err)
		
		err = writer.Close()
		require.NoError(t, err)

		req := httptest.NewRequest(http.MethodPost, "/artifacts/"+artifact.ID.String()+"/materials", body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		w := httptest.NewRecorder()

		h.UploadMaterial(w, req)

		assert.Equal(t, http.StatusCreated, w.Code)
		var material map[string]interface{}
		err = json.Unmarshal(w.Body.Bytes(), &material)
		require.NoError(t, err)
		assert.Equal(t, "test.txt", material["filename"])
		assert.Equal(t, "document", material["kind"])
	})

	t.Run("returns 400 for invalid artifact ID", func(t *testing.T) {
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		fileWriter, err := writer.CreateFormFile("file", "test.txt")
		require.NoError(t, err)
		_, err = fileWriter.Write([]byte("content"))
		require.NoError(t, err)
		writer.Close()

		req := httptest.NewRequest(http.MethodPost, "/artifacts/invalid-id/materials", body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		w := httptest.NewRecorder()

		h.UploadMaterial(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("returns 404 for non-existent artifact", func(t *testing.T) {
		nonExistentID := uuid.New()
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		fileWriter, err := writer.CreateFormFile("file", "test.txt")
		require.NoError(t, err)
		_, err = fileWriter.Write([]byte("content"))
		require.NoError(t, err)
		writer.Close()

		req := httptest.NewRequest(http.MethodPost, "/artifacts/"+nonExistentID.String()+"/materials", body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		w := httptest.NewRecorder()

		h.UploadMaterial(w, req)

		// Should return 404 when artifact doesn't exist
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("returns 400 when file is missing", func(t *testing.T) {
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		// Don't add file field
		err := writer.WriteField("kind", "document")
		require.NoError(t, err)
		writer.Close()

		req := httptest.NewRequest(http.MethodPost, "/artifacts/"+artifact.ID.String()+"/materials", body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		w := httptest.NewRecorder()

		h.UploadMaterial(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "Failed to get file")
	})

	t.Run("returns 405 for non-POST methods", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/artifacts/"+artifact.ID.String()+"/materials", nil)
		w := httptest.NewRecorder()

		h.UploadMaterial(w, req)

		assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
	})

	t.Run("defaults kind to document when not provided", func(t *testing.T) {
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		
		fileWriter, err := writer.CreateFormFile("file", "test.txt")
		require.NoError(t, err)
		_, err = fileWriter.Write([]byte("Test content"))
		require.NoError(t, err)
		// Don't add kind field
		writer.Close()

		req := httptest.NewRequest(http.MethodPost, "/artifacts/"+artifact.ID.String()+"/materials", body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		w := httptest.NewRecorder()

		h.UploadMaterial(w, req)

		assert.Equal(t, http.StatusCreated, w.Code)
		var material map[string]interface{}
		err = json.Unmarshal(w.Body.Bytes(), &material)
		require.NoError(t, err)
		assert.Equal(t, "document", material["kind"]) // Should default to "document"
	})
}

func TestUploadTranscript(t *testing.T) {
	h, cleanup := setupTestHandlers(t)
	defer cleanup()
	db := h.DB
	ctx := context.Background()

	// Create a session, artifact and video source
	session := createTestSessionForHandlers(t, h.DB, "Test Session")
	artifact, err := db.CreateArtifact(ctx, session.ID, "Transcript Test", nil)
	require.NoError(t, err)

		embedURL := "https://www.loom.com/share/test"
		videoSource := &models.VideoSource{
			ID:               uuid.New(),
			ArtifactID:       artifact.ID,
			SessionID:        session.ID,
			Provider:         "loom",
			VideoURL:         embedURL,
			PlaybackMode:     "embed",
			EmbedURL:         &embedURL,
			TranscriptStatus: models.VideoTranscriptStatusMissing,
		}
		err = db.CreateVideoSource(ctx, videoSource)
		require.NoError(t, err)

	t.Run("uploads transcript successfully", func(t *testing.T) {
		reqBody := map[string]interface{}{
			"transcript_text": "This is the full transcript of the video.",
		}
		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest(http.MethodPost, "/artifacts/"+artifact.ID.String()+"/video/"+videoSource.ID.String()+"/transcript", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		h.UploadTranscript(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var updatedVideoSource map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &updatedVideoSource)
		require.NoError(t, err)
		assert.Equal(t, "ready", updatedVideoSource["transcript_status"])
		assert.NotNil(t, updatedVideoSource["transcript_text"])
	})

	t.Run("returns 400 for invalid artifact ID", func(t *testing.T) {
		reqBody := map[string]interface{}{
			"transcript_text": "Test transcript",
		}
		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest(http.MethodPost, "/artifacts/invalid-id/video/"+videoSource.ID.String()+"/transcript", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		h.UploadTranscript(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("returns 400 for invalid video ID", func(t *testing.T) {
		reqBody := map[string]interface{}{
			"transcript_text": "Test transcript",
		}
		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest(http.MethodPost, "/artifacts/"+artifact.ID.String()+"/video/invalid-id/transcript", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		h.UploadTranscript(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("returns 404 for non-existent video source", func(t *testing.T) {
		nonExistentVideoID := uuid.New()
		reqBody := map[string]interface{}{
			"transcript_text": "Test transcript",
		}
		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest(http.MethodPost, "/artifacts/"+artifact.ID.String()+"/video/"+nonExistentVideoID.String()+"/transcript", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		h.UploadTranscript(w, req)

		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.Contains(t, w.Body.String(), "Video source not found")
	})

	t.Run("returns 400 when video source belongs to different artifact", func(t *testing.T) {
		// Create another session and artifact
		otherSession := createTestSessionForHandlers(t, h.DB, "Other Session")
		otherArtifact, err := db.CreateArtifact(ctx, otherSession.ID, "Other Artifact", nil)
		require.NoError(t, err)

		reqBody := map[string]interface{}{
			"transcript_text": "Test transcript",
		}
		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest(http.MethodPost, "/artifacts/"+otherArtifact.ID.String()+"/video/"+videoSource.ID.String()+"/transcript", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		h.UploadTranscript(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "does not belong to this artifact")
	})

	t.Run("returns 400 when transcript_text is missing", func(t *testing.T) {
		reqBody := map[string]interface{}{}
		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest(http.MethodPost, "/artifacts/"+artifact.ID.String()+"/video/"+videoSource.ID.String()+"/transcript", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		h.UploadTranscript(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "transcript_text is required")
	})

	t.Run("returns 405 for non-POST methods", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/artifacts/"+artifact.ID.String()+"/video/"+videoSource.ID.String()+"/transcript", nil)
		w := httptest.NewRecorder()

		h.UploadTranscript(w, req)

		assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
	})

	t.Run("returns 400 for invalid URL path", func(t *testing.T) {
		reqBody := map[string]interface{}{
			"transcript_text": "Test transcript",
		}
		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest(http.MethodPost, "/artifacts/"+artifact.ID.String()+"/invalid/path", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		h.UploadTranscript(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "Invalid URL path")
	})
}
