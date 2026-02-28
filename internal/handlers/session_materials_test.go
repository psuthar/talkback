package handlers

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"
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

	t.Run("uploads docx and extracts text", func(t *testing.T) {
		tmp := t.TempDir()
		docxPath := filepath.Join(tmp, "minimal.docx")
		createMinimalDocx(t, docxPath, "Session DOCX content")
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		fileWriter, err := writer.CreateFormFile("file", "minimal.docx")
		require.NoError(t, err)
		data, err := os.ReadFile(docxPath)
		require.NoError(t, err)
		_, err = fileWriter.Write(data)
		require.NoError(t, err)
		require.NoError(t, writer.Close())

		req := httptest.NewRequest(http.MethodPost, "/sessions/"+session.ID.String()+"/materials/upload", body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		w := httptest.NewRecorder()
		h.SessionUploadMaterial(w, req)
		assert.Equal(t, http.StatusCreated, w.Code)
		var m map[string]interface{}
		err = json.NewDecoder(w.Body).Decode(&m)
		require.NoError(t, err)
		assert.Equal(t, "ready", m["text_status"])
		ext, ok := m["extracted_text"].(string)
		require.True(t, ok)
		assert.True(t, strings.Contains(ext, "Session DOCX content"), "extracted_text should contain expected string: %q", ext)
	})

	t.Run("uploads xlsx and extracts text", func(t *testing.T) {
		tmp := t.TempDir()
		xlsxPath := filepath.Join(tmp, "minimal.xlsx")
		f := excelize.NewFile()
		require.NoError(t, f.SetCellValue("Sheet1", "A1", "Session XLSX content"))
		require.NoError(t, f.SaveAs(xlsxPath))
		f.Close()

		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		fileWriter, err := writer.CreateFormFile("file", "minimal.xlsx")
		require.NoError(t, err)
		data, err := os.ReadFile(xlsxPath)
		require.NoError(t, err)
		_, err = fileWriter.Write(data)
		require.NoError(t, err)
		require.NoError(t, writer.Close())

		req := httptest.NewRequest(http.MethodPost, "/sessions/"+session.ID.String()+"/materials/upload", body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		w := httptest.NewRecorder()
		h.SessionUploadMaterial(w, req)
		assert.Equal(t, http.StatusCreated, w.Code)
		var m map[string]interface{}
		err = json.NewDecoder(w.Body).Decode(&m)
		require.NoError(t, err)
		assert.Equal(t, "ready", m["text_status"])
		ext, ok := m["extracted_text"].(string)
		require.True(t, ok)
		assert.True(t, strings.Contains(ext, "Session XLSX content"), "extracted_text should contain expected string: %q", ext)
	})

	t.Run("uploads pptx and extracts text", func(t *testing.T) {
		tmp := t.TempDir()
		pptxPath := filepath.Join(tmp, "minimal.pptx")
		createMinimalPptx(t, pptxPath, "Session PPTX content")
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		fileWriter, err := writer.CreateFormFile("file", "minimal.pptx")
		require.NoError(t, err)
		data, err := os.ReadFile(pptxPath)
		require.NoError(t, err)
		_, err = fileWriter.Write(data)
		require.NoError(t, err)
		require.NoError(t, writer.Close())

		req := httptest.NewRequest(http.MethodPost, "/sessions/"+session.ID.String()+"/materials/upload", body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		w := httptest.NewRecorder()
		h.SessionUploadMaterial(w, req)
		assert.Equal(t, http.StatusCreated, w.Code)
		var m map[string]interface{}
		err = json.NewDecoder(w.Body).Decode(&m)
		require.NoError(t, err)
		assert.Equal(t, "ready", m["text_status"])
		ext, ok := m["extracted_text"].(string)
		require.True(t, ok)
		assert.True(t, strings.Contains(ext, "Session PPTX content"), "extracted_text should contain expected string: %q", ext)
	})
}

func createMinimalDocx(t *testing.T, path, text string) {
	t.Helper()
	w, err := os.Create(path)
	require.NoError(t, err)
	zw := zip.NewWriter(w)
	docXML := `<?xml version="1.0"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:body><w:p><w:r><w:t>` + text + `</w:t></w:r></w:p></w:body>
</w:document>`
	fw, err := zw.Create("word/document.xml")
	require.NoError(t, err)
	_, err = fw.Write([]byte(docXML))
	require.NoError(t, err)
	require.NoError(t, zw.Close())
	require.NoError(t, w.Close())
}

func createMinimalPptx(t *testing.T, path, text string) {
	t.Helper()
	w, err := os.Create(path)
	require.NoError(t, err)
	zw := zip.NewWriter(w)
	slideXML := `<?xml version="1.0"?>
<sld xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main">
  <cSld><spTree><sp><txBody><a:p><a:r><a:t>` + text + `</a:t></a:r></a:p></txBody></sp></spTree></cSld>
</sld>`
	fw, err := zw.Create("ppt/slides/slide1.xml")
	require.NoError(t, err)
	_, err = fw.Write([]byte(slideXML))
	require.NoError(t, err)
	require.NoError(t, zw.Close())
	require.NoError(t, w.Close())
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

	t.Run("deletes material successfully (soft-delete tombstone)", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/sessions/"+session.ID.String()+"/materials/"+material.ID.String(), nil)
		w := httptest.NewRecorder()
		h.DeleteSessionMaterial(w, req)
		assert.Equal(t, http.StatusNoContent, w.Code)

		// Row still exists as tombstone; deleted_at set, storage_key cleared
		got, err := h.DB.GetMaterialByID(ctx, material.ID)
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.NotNil(t, got.DeletedAt, "expected tombstone: deleted_at set")
		assert.Empty(t, got.StorageKey, "expected storage_key cleared")
		// Active list should not include deleted material
		active, err := h.DB.GetActiveMaterialsBySessionID(ctx, session.ID)
		require.NoError(t, err)
		assert.Len(t, active, 0)
	})

	t.Run("returns 404 when material does not exist", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodDelete, "/sessions/"+session.ID.String()+"/materials/"+uuid.New().String(), nil)
		w := httptest.NewRecorder()
		h.DeleteSessionMaterial(w, req)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})
}

func strPtr(s string) *string { return &s }
