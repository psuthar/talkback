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
	"testing"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/xuri/excelize/v2"
)

func TestListSessionMaterials(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
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
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
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
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
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

	t.Run("uploads docx and returns 201 with ready (sync extraction)", func(t *testing.T) {
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
		// DOCX uses pure-Go extraction and is now extracted synchronously during upload (no async job needed).
		assert.Equal(t, "ready", m["text_status"], "DOCX extraction runs synchronously during upload")
		assert.NotNil(t, m["extracted_text"], "extracted_text should be set immediately for DOCX")
	})

	t.Run("uploads xlsx and returns 201 with ready (sync extraction)", func(t *testing.T) {
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
		// XLSX uses pure-Go extraction and is now extracted synchronously during upload (no async job needed).
		assert.Equal(t, "ready", m["text_status"], "XLSX extraction runs synchronously during upload")
		assert.NotNil(t, m["extracted_text"], "extracted_text should be set immediately for XLSX")
	})

	t.Run("uploads pptx and returns 201 with ready (sync extraction)", func(t *testing.T) {
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
		// PPTX uses pure-Go extraction and is now extracted synchronously during upload (no async job needed).
		assert.Equal(t, "ready", m["text_status"], "PPTX extraction runs synchronously during upload")
		assert.NotNil(t, m["extracted_text"], "extracted_text should be set immediately for PPTX")
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
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
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

func TestGetMaterialSlides(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	ctx := context.Background()

	session := createTestSessionForHandlers(t, h.DB, "Test Session")
	artifact, err := h.DB.CreateArtifact(ctx, session.ID, "Session materials", nil)
	require.NoError(t, err)

	t.Run("returns 404 when material not found", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/sessions/"+session.ID.String()+"/materials/"+uuid.New().String()+"/slides", nil)
		w := httptest.NewRecorder()
		h.GetMaterialSlides(w, req)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("returns 404 for non-slides material", func(t *testing.T) {
		doc := &models.Material{
			ID:            uuid.New(),
			ArtifactID:    artifact.ID,
			SessionID:     session.ID,
			Kind:          string(models.MaterialKindDocument),
			Filename:      "doc.pdf",
			TextStatus:    models.MaterialTextStatusReady,
		}
		require.NoError(t, h.DB.CreateMaterial(ctx, doc))
		req := httptest.NewRequest(http.MethodGet, "/sessions/"+session.ID.String()+"/materials/"+doc.ID.String()+"/slides", nil)
		w := httptest.NewRecorder()
		h.GetMaterialSlides(w, req)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("returns 404 when session does not match material", func(t *testing.T) {
		otherSession := createTestSessionForHandlers(t, h.DB, "Other Session")
		otherArtifact, err := h.DB.CreateArtifact(ctx, otherSession.ID, "Other", nil)
		require.NoError(t, err)
		slideMat := &models.Material{
			ID:            uuid.New(),
			ArtifactID:    otherArtifact.ID,
			SessionID:     otherSession.ID,
			Kind:          string(models.MaterialKindDocument),
			Filename:      "deck.pptx",
			TextStatus:    models.MaterialTextStatusReady,
		}
		require.NoError(t, h.DB.CreateMaterial(ctx, slideMat))
		// Request with session ID that does not own the material
		req := httptest.NewRequest(http.MethodGet, "/sessions/"+session.ID.String()+"/materials/"+slideMat.ID.String()+"/slides", nil)
		w := httptest.NewRecorder()
		h.GetMaterialSlides(w, req)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("returns 200 with empty slides when no manifest yet (e.g. async slide gen not done or failed)", func(t *testing.T) {
		// PPT/PPTX material with no StorageURL/StorageKey and no _slides manifest; GetMaterialSlides returns 200 + empty list.
		slideMat := &models.Material{
			ID:            uuid.New(),
			ArtifactID:    artifact.ID,
			SessionID:     session.ID,
			Kind:          string(models.MaterialKindDocument),
			Filename:      "deck.pptx",
			TextStatus:    models.MaterialTextStatusReady,
		}
		require.NoError(t, h.DB.CreateMaterial(ctx, slideMat))
		req := httptest.NewRequest(http.MethodGet, "/sessions/"+session.ID.String()+"/materials/"+slideMat.ID.String()+"/slides", nil)
		w := httptest.NewRecorder()
		h.GetMaterialSlides(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		var resp struct {
			MaterialID string `json:"material_id"`
			Slides     []struct {
				Index    int    `json:"index"`
				ImageURL string `json:"image_url"`
			} `json:"slides"`
		}
		err := json.NewDecoder(w.Body).Decode(&resp)
		require.NoError(t, err)
		assert.Equal(t, slideMat.ID.String(), resp.MaterialID)
		assert.Len(t, resp.Slides, 0)
	})

	t.Run("returns 400 for invalid session ID", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/sessions/bad-session-id/materials/"+uuid.New().String()+"/slides", nil)
		w := httptest.NewRecorder()
		h.GetMaterialSlides(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("returns 400 for invalid material ID", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/sessions/"+session.ID.String()+"/materials/bad-material-id/slides", nil)
		w := httptest.NewRecorder()
		h.GetMaterialSlides(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}

func strPtr(s string) *string { return &s }
