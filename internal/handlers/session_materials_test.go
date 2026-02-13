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

func TestListSessionMaterials(t *testing.T) {
	h, cleanup := setupTestHandlers(t)
	defer cleanup()

	session := createTestSessionForHandlers(t, h.DB, "Test Session")

	t.Run("returns empty list when no materials", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/sessions/"+session.ID.String()+"/materials", nil)
		w := httptest.NewRecorder()
		h.ListSessionMaterials(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		var list []*models.Material
		err := json.NewDecoder(w.Body).Decode(&list)
		require.NoError(t, err)
		assert.NotNil(t, list)
		assert.Len(t, list, 0)
	})

	t.Run("returns 400 for invalid session ID", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/sessions/bad-id/materials", nil)
		w := httptest.NewRecorder()
		h.ListSessionMaterials(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("returns 404 for non-existent session", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/sessions/"+uuid.New().String()+"/materials", nil)
		w := httptest.NewRecorder()
		h.ListSessionMaterials(w, req)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})
}

func TestSessionPasteMaterial(t *testing.T) {
	h, cleanup := setupTestHandlers(t)
	defer cleanup()

	session := createTestSessionForHandlers(t, h.DB, "Test Session")

	t.Run("creates pasted material successfully", func(t *testing.T) {
		body := bytes.NewBufferString(`{"title":"My note","text":"Some pasted content here."}`)
		req := httptest.NewRequest(http.MethodPost, "/sessions/"+session.ID.String()+"/materials/paste", body)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.SessionPasteMaterial(w, req)
		assert.Equal(t, http.StatusCreated, w.Code)
		var m map[string]interface{}
		err := json.NewDecoder(w.Body).Decode(&m)
		require.NoError(t, err)
		assert.Equal(t, "My note", m["title"])
		assert.Equal(t, "ready", m["text_status"])
	})

	t.Run("returns 400 when text is empty", func(t *testing.T) {
		body := bytes.NewBufferString(`{"title":"Empty","text":"   "}`)
		req := httptest.NewRequest(http.MethodPost, "/sessions/"+session.ID.String()+"/materials/paste", body)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.SessionPasteMaterial(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("returns 404 for non-existent session", func(t *testing.T) {
		body := bytes.NewBufferString(`{"title":"X","text":"y"}`)
		req := httptest.NewRequest(http.MethodPost, "/sessions/"+uuid.New().String()+"/materials/paste", body)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		h.SessionPasteMaterial(w, req)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})
}

func TestSessionUploadMaterial(t *testing.T) {
	h, cleanup := setupTestHandlers(t)
	defer cleanup()

	session := createTestSessionForHandlers(t, h.DB, "Test Session")

	t.Run("uploads txt file successfully", func(t *testing.T) {
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		fileWriter, err := writer.CreateFormFile("file", "note.txt")
		require.NoError(t, err)
		_, _ = fileWriter.Write([]byte("Hello world"))
		require.NoError(t, writer.Close())

		req := httptest.NewRequest(http.MethodPost, "/sessions/"+session.ID.String()+"/materials/upload", body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		w := httptest.NewRecorder()
		h.SessionUploadMaterial(w, req)
		assert.Equal(t, http.StatusCreated, w.Code)
		var m map[string]interface{}
		err = json.NewDecoder(w.Body).Decode(&m)
		require.NoError(t, err)
		assert.Equal(t, "note.txt", m["filename"])
		assert.Equal(t, "ready", m["text_status"])
	})

	t.Run("returns 400 when file is missing", func(t *testing.T) {
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		writer.WriteField("title", "No file")
		writer.Close()
		req := httptest.NewRequest(http.MethodPost, "/sessions/"+session.ID.String()+"/materials/upload", body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		w := httptest.NewRecorder()
		h.SessionUploadMaterial(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

func TestDeleteSessionMaterial(t *testing.T) {
	h, cleanup := setupTestHandlers(t)
	defer cleanup()
	ctx := context.Background()

	session := createTestSessionForHandlers(t, h.DB, "Test Session")
	artifact, err := h.DB.CreateArtifact(ctx, session.ID, "Session materials", nil)
	require.NoError(t, err)

	material := &models.Material{
		ID:            uuid.New(),
		ArtifactID:    artifact.ID,
		SessionID:     session.ID,
		Kind:          "document",
		Filename:      "doc.txt",
		ContentType:   "text/plain",
		StorageURL:    "",
		TextStatus:    models.MaterialTextStatusReady,
		ExtractedText: strPtr("content"),
	}
	require.NoError(t, h.DB.CreateMaterial(ctx, material))

	t.Run("deletes material successfully", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/sessions/"+session.ID.String()+"/materials/"+material.ID.String(), nil)
		w := httptest.NewRecorder()
		h.DeleteSessionMaterial(w, req)
		assert.Equal(t, http.StatusNoContent, w.Code)

		got, err := h.DB.GetMaterialByID(ctx, material.ID)
		assert.Error(t, err)
		assert.Nil(t, got)
	})

	t.Run("returns 404 when material does not exist", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/sessions/"+session.ID.String()+"/materials/"+uuid.New().String(), nil)
		w := httptest.NewRecorder()
		h.DeleteSessionMaterial(w, req)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})
}

func strPtr(s string) *string { return &s }
