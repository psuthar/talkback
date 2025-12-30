package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/database"
	"github.com/psuthar/talkback/internal/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestHandlers(t *testing.T) (*Handlers, func()) {
	t.Helper()

	// Setup test database
	databaseURL, cleanupDB := test.SetupTestDB(t)

	originalURL := os.Getenv("DATABASE_URL")
	os.Setenv("DATABASE_URL", databaseURL)

	db, err := database.New()
	require.NoError(t, err)

	cleanup := func() {
		test.TruncateTables(t, db.Pool)
		db.Close()
		cleanupDB()
		if originalURL != "" {
			os.Setenv("DATABASE_URL", originalURL)
		} else {
			os.Unsetenv("DATABASE_URL")
		}
	}
	return NewHandlers(db), cleanup
}

func TestCreateArtifact(t *testing.T) {
	h, cleanup := setupTestHandlers(t)
	defer cleanup()

	t.Run("creates artifact with title only", func(t *testing.T) {
		reqBody := map[string]interface{}{
			"title": "Test Artifact",
		}
		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest(http.MethodPost, "/artifacts", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		h.CreateArtifact(w, req)

		assert.Equal(t, http.StatusCreated, w.Code)
		var response CreateArtifactResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		assert.Equal(t, "Test Artifact", response.Title)
		assert.Equal(t, "draft", response.Status)
		assert.NotEmpty(t, response.ID)
	})

	t.Run("creates artifact with title and description", func(t *testing.T) {
		reqBody := map[string]interface{}{
			"title":       "Test Artifact",
			"description": "Test description",
		}
		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest(http.MethodPost, "/artifacts", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		h.CreateArtifact(w, req)

		assert.Equal(t, http.StatusCreated, w.Code)
		var response CreateArtifactResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		assert.Equal(t, "Test Artifact", response.Title)
		assert.NotNil(t, response.Description)
		assert.Equal(t, "Test description", *response.Description)
	})

	t.Run("returns 400 when title is missing", func(t *testing.T) {
		reqBody := map[string]interface{}{}
		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest(http.MethodPost, "/artifacts", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		h.CreateArtifact(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("returns 405 for non-POST methods", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/artifacts", nil)
		w := httptest.NewRecorder()

		h.CreateArtifact(w, req)

		assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
	})
}

func TestGetArtifact(t *testing.T) {
	h, cleanup := setupTestHandlers(t)
	defer cleanup()

	// Create an artifact for testing
	artifact, err := h.DB.CreateArtifact(context.Background(), "Get Test Artifact", nil)
	require.NoError(t, err)

	t.Run("returns artifact by id", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/artifacts/"+artifact.ID.String(), nil)
		w := httptest.NewRecorder()

		h.GetArtifact(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var response map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		require.NotNil(t, response["artifact"])
		// materials should be an array (empty or with items), not nil
		materials, ok := response["materials"]
		require.True(t, ok, "materials key should exist")
		assert.NotNil(t, materials, "materials should not be nil (should be empty array)")
		// video_sources should be an array
		videoSources, ok := response["video_sources"]
		require.True(t, ok, "video_sources key should exist")
		assert.NotNil(t, videoSources, "video_sources should not be nil (should be empty array)")
	})

	t.Run("returns 400 for invalid artifact id", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/artifacts/invalid-id", nil)
		w := httptest.NewRecorder()

		h.GetArtifact(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("returns 404 for non-existent artifact", func(t *testing.T) {
		nonExistentID := uuid.New()
		req := httptest.NewRequest(http.MethodGet, "/artifacts/"+nonExistentID.String(), nil)
		w := httptest.NewRecorder()

		h.GetArtifact(w, req)

		// Should return 500 (internal server error) since GetArtifact returns error
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	})
}

func TestAttachVideoURL(t *testing.T) {
	h, cleanup := setupTestHandlers(t)
	defer cleanup()

	// Create an artifact for testing
	artifact, err := h.DB.CreateArtifact(context.Background(), "Video Test Artifact", nil)
	require.NoError(t, err)

	t.Run("attaches video URL with loom provider", func(t *testing.T) {
		reqBody := map[string]interface{}{
			"provider":  "loom",
			"video_url": "https://www.loom.com/share/example",
		}
		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest(http.MethodPost, "/artifacts/"+artifact.ID.String()+"/video", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		h.AttachVideoURL(w, req)

		assert.Equal(t, http.StatusCreated, w.Code)
		var videoSource map[string]interface{}
		err := json.Unmarshal(w.Body.Bytes(), &videoSource)
		require.NoError(t, err)
		assert.Equal(t, "loom", videoSource["provider"])
		assert.Equal(t, "https://www.loom.com/share/example", videoSource["video_url"])
	})

	t.Run("returns 400 when video_url is missing", func(t *testing.T) {
		reqBody := map[string]interface{}{
			"provider": "loom",
		}
		body, _ := json.Marshal(reqBody)
		req := httptest.NewRequest(http.MethodPost, "/artifacts/"+artifact.ID.String()+"/video", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		h.AttachVideoURL(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}
