package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSessionPolishQuestion(t *testing.T) {
	h, cleanup := setupTestHandlers(t)
	defer cleanup()

	session := createTestSessionForHandlers(t, h.DB, "Polish Session")

	t.Run("returns 405 for non-POST", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/sessions/"+session.ID.String()+"/questions/polish", nil)
		w := httptest.NewRecorder()
		h.SessionPolishQuestion(w, req)
		assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
	})

	t.Run("returns 400 for invalid session id", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{"text": "um what was the point"})
		req := httptest.NewRequest(http.MethodPost, "/api/sessions/invalid-id/questions/polish", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.SessionPolishQuestion(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("returns 404 for non-existent session", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{"text": "um what was the point"})
		req := httptest.NewRequest(http.MethodPost, "/api/sessions/00000000-0000-0000-0000-000000000001/questions/polish", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.SessionPolishQuestion(w, req)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("returns 400 when text is empty", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{"text": ""})
		req := httptest.NewRequest(http.MethodPost, "/api/sessions/"+session.ID.String()+"/questions/polish", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.SessionPolishQuestion(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("returns 200 and polished_text with rule-based polish", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{"text": "um so what was uh the point"})
		req := httptest.NewRequest(http.MethodPost, "/api/sessions/"+session.ID.String()+"/questions/polish", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.SessionPolishQuestion(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		var res struct {
			PolishedText string `json:"polished_text"`
		}
		require.NoError(t, json.NewDecoder(w.Body).Decode(&res))
		assert.Equal(t, "so what was the point", res.PolishedText)
	})

	t.Run("returns 200 and polished_text for repeated words", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{"text": "the the main topic was was discussed"})
		req := httptest.NewRequest(http.MethodPost, "/api/sessions/"+session.ID.String()+"/questions/polish", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.SessionPolishQuestion(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		var res struct {
			PolishedText string `json:"polished_text"`
		}
		require.NoError(t, json.NewDecoder(w.Body).Decode(&res))
		assert.Equal(t, "the main topic was discussed", res.PolishedText)
	})
}
