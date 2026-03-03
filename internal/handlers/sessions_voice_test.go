package handlers

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTranscribeSessionQuestionVoice cannot use t.Parallel() because a subtest uses t.Setenv (incompatible with parallel).
func TestTranscribeSessionQuestionVoice(t *testing.T) {
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	ctx := context.Background()
	session := createTestSessionForHandlers(t, h.DB, "Voice Session")

	t.Run("returns 405 for non-POST", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/sessions/"+session.ID.String()+"/questions/voice", nil)
		w := httptest.NewRecorder()

		h.TranscribeSessionQuestionVoice(w, req)
		assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
	})

	t.Run("returns 400 for invalid session id", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/sessions/invalid-id/questions/voice", nil)
		w := httptest.NewRecorder()

		h.TranscribeSessionQuestionVoice(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("returns 404 for non-existent session", func(t *testing.T) {
		nonExistentID := uuid.New()
		req := httptest.NewRequest(http.MethodPost, "/sessions/"+nonExistentID.String()+"/questions/voice", nil)
		w := httptest.NewRecorder()

		h.TranscribeSessionQuestionVoice(w, req)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("returns 400 when file is missing", func(t *testing.T) {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		require.NoError(t, writer.Close())

		req := httptest.NewRequest(http.MethodPost, "/sessions/"+session.ID.String()+"/questions/voice", &body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		w := httptest.NewRecorder()

		h.TranscribeSessionQuestionVoice(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("returns 503 when no STT backend is available", func(t *testing.T) {
		t.Setenv("OPENAI_API_KEY", "") // force CLI path
		t.Setenv("WHISPER_CLI", "talkback_nonexistent_whisper_cli")

		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		part, err := writer.CreateFormFile("file", "voice.webm")
		require.NoError(t, err)
		_, err = part.Write([]byte("not real audio"))
		require.NoError(t, err)
		require.NoError(t, writer.Close())

		req := httptest.NewRequest(http.MethodPost, "/sessions/"+session.ID.String()+"/questions/voice", &body)
		req = req.WithContext(ctx)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		w := httptest.NewRecorder()

		h.TranscribeSessionQuestionVoice(w, req)
		assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	})
}

