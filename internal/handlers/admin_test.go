package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResetAllData(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	db := h.DB

	t.Run("returns 403 when ALLOW_DEV_RESET is not set", func(t *testing.T) {
		// Ensure flag is not set
		originalFlag := os.Getenv("ALLOW_DEV_RESET")
		os.Unsetenv("ALLOW_DEV_RESET")
		defer func() {
			if originalFlag != "" {
				os.Setenv("ALLOW_DEV_RESET", originalFlag)
			}
		}()

		req := httptest.NewRequest(http.MethodPost, "/admin/reset", nil)
		w := httptest.NewRecorder()

		h.ResetAllData(w, req)

		assert.Equal(t, http.StatusForbidden, w.Code)
		var response ResetErrorResponse
		err := json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		assert.Equal(t, "forbidden", response.Error)
		assert.Contains(t, response.Message, "ALLOW_DEV_RESET")
	})

	t.Run("returns 403 when ALLOW_DEV_RESET is false", func(t *testing.T) {
		originalFlag := os.Getenv("ALLOW_DEV_RESET")
		os.Setenv("ALLOW_DEV_RESET", "false")
		defer func() {
			if originalFlag != "" {
				os.Setenv("ALLOW_DEV_RESET", originalFlag)
			} else {
				os.Unsetenv("ALLOW_DEV_RESET")
			}
		}()

		req := httptest.NewRequest(http.MethodPost, "/admin/reset", nil)
		w := httptest.NewRecorder()

		h.ResetAllData(w, req)

		assert.Equal(t, http.StatusForbidden, w.Code)
	})

	t.Run("returns 200 and deletes all data when ALLOW_DEV_RESET is true", func(t *testing.T) {
		// Set flag to enable reset
		originalFlag := os.Getenv("ALLOW_DEV_RESET")
		os.Setenv("ALLOW_DEV_RESET", "true")
		defer func() {
			if originalFlag != "" {
				os.Setenv("ALLOW_DEV_RESET", originalFlag)
			} else {
				os.Unsetenv("ALLOW_DEV_RESET")
			}
		}()

		// Create some test data
		ctx := context.Background()
		session := &models.Session{
			ID:     uuid.New(),
			Title:  "Test Session",
			Status: models.SessionStatusOpen,
		}
		err := db.CreateSession(ctx, session)
		require.NoError(t, err)
		artifact, err := db.CreateArtifact(ctx, session.ID, "Test Artifact", nil)
		require.NoError(t, err)

		material := &models.Material{
			ID:          uuid.New(),
			ArtifactID:  artifact.ID,
			SessionID:   session.ID,
			Kind:        "document",
			Filename:    "test.txt",
			ContentType: "text/plain",
			StorageURL:  "data/uploads/test.txt",
			TextStatus:  models.MaterialTextStatusReady,
		}
		require.NoError(t, db.CreateMaterial(ctx, material))

		videoSource := &models.VideoSource{
			ID:               uuid.New(),
			ArtifactID:       artifact.ID,
			SessionID:        session.ID,
			Provider:         "other",
			VideoURL:         "https://example.com/video",
			TranscriptStatus: models.VideoTranscriptStatusReady,
		}
		require.NoError(t, db.CreateVideoSource(ctx, videoSource))

		question := &models.Question{
			ID:              uuid.New(),
			ArtifactID:      artifact.ID,
			SessionID:       session.ID,
			QuestionText:    "Test question",
			QuestionSource:  models.QuestionSourceText,
			VideoTimeSeconds: nil,
		}
		require.NoError(t, db.CreateQuestion(ctx, question))

		answer := &models.Answer{
			ID:           uuid.New(),
			QuestionID:   question.ID,
			AnswerText:   "Test answer",
			AnswerStatus: models.AnswerStatusAnswered,
			Confidence:   0.8,
			Citations:    []models.Citation{},
		}
		require.NoError(t, db.CreateAnswer(ctx, answer))

		// Call reset endpoint
		req := httptest.NewRequest(http.MethodPost, "/admin/reset", nil)
		w := httptest.NewRecorder()

		h.ResetAllData(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var response ResetResponse
		err = json.Unmarshal(w.Body.Bytes(), &response)
		require.NoError(t, err)
		assert.True(t, response.OK)
		assert.NotNil(t, response.Deleted)
		assert.Greater(t, response.Deleted["artifacts"], 0)
		assert.Greater(t, response.Deleted["materials"], 0)
		assert.Greater(t, response.Deleted["video_sources"], 0)
		assert.Greater(t, response.Deleted["questions"], 0)
		assert.Greater(t, response.Deleted["answers"], 0)

		// Verify data is actually deleted
		_, err = db.GetArtifact(ctx, artifact.ID)
		assert.Error(t, err) // Should not exist
	})
}
