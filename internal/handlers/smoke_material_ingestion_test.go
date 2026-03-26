package handlers

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/psuthar/talkback/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// smokeDocText is a deterministic fixture with distinctive keywords for citation/keyword smoke assertions.
const smokeDocText = "Meridian proposal approved. APAC expansion budget confirmed at 2.4M. Churn reduced to below 6% this quarter."

// TestSmoke_MaterialIngestion_PasteCreatesReadyMaterial covers Flow 2 using the paste endpoint,
// which synchronously sets text_status="ready" without requiring a storage driver or async worker.
// This is the preferred smoke path for material ingestion.
func TestSmoke_MaterialIngestion_PasteCreatesReadyMaterial(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	session := createTestSessionForHandlers(t, h.DB, "Material Smoke Session")

	// --- Step 1: Paste material ---
	body, _ := json.Marshal(map[string]any{
		"title": "Meridian Report",
		"text":  smokeDocText,
	})
	req := httptest.NewRequest(http.MethodPost, "/sessions/"+session.ID.String()+"/materials/paste", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.SessionPasteMaterial(w, req)

	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	var m map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&m))
	assert.Equal(t, "Meridian Report", m["title"])
	assert.Equal(t, "ready", m["text_status"])
	assert.NotEmpty(t, m["id"])

	// --- Step 2: List materials confirms persistence ---
	listReq := httptest.NewRequest(http.MethodGet, "/sessions/"+session.ID.String()+"/materials", nil)
	listW := httptest.NewRecorder()
	h.ListSessionMaterials(listW, listReq)

	require.Equal(t, http.StatusOK, listW.Code, listW.Body.String())
	var list []*models.Material
	require.NoError(t, json.NewDecoder(listW.Body).Decode(&list))
	require.Len(t, list, 1)
	require.NotNil(t, list[0].Title)
	assert.Equal(t, "Meridian Report", *list[0].Title)
	assert.Equal(t, models.MaterialTextStatusReady, list[0].TextStatus)
}

// TestSmoke_ExtractionContent_PasteTextIsReadableViaListEndpoint verifies that the extracted
// text content of a pasted material is actually stored and readable — not just that the pipeline
// reached text_status="ready". This covers the upload_extraction readiness gate at the API level:
// the 201 response must include extracted_text, and the list endpoint must return that same content
// from the database, proving the text was both extracted and persisted correctly.
func TestSmoke_ExtractionContent_PasteTextIsReadableViaListEndpoint(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	session := createTestSessionForHandlers(t, h.DB, "Extraction Content Smoke Session")

	// --- Step 1: Paste material with a distinctive keyword ---
	body, _ := json.Marshal(map[string]any{
		"title": "Extraction Verification Report",
		"text":  smokeDocText,
	})
	req := httptest.NewRequest(http.MethodPost, "/sessions/"+session.ID.String()+"/materials/paste", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.SessionPasteMaterial(w, req)

	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	var created map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&created))

	// Assert extracted_text is present and contains the known keyword in the 201 response.
	// This proves the extraction completed synchronously during the paste request, not just
	// that text_status was set to "ready".
	assert.Equal(t, "ready", created["text_status"], "paste must return text_status=ready")
	assert.NotEmpty(t, created["extracted_text"], "paste 201 response must include non-empty extracted_text")
	if et, ok := created["extracted_text"].(string); ok {
		assert.Contains(t, et, "Meridian", "extracted_text must contain the known keyword from smokeDocText")
	}

	// --- Step 2: Re-fetch via ListSessionMaterials to verify DB persistence ---
	listReq := httptest.NewRequest(http.MethodGet, "/sessions/"+session.ID.String()+"/materials", nil)
	listW := httptest.NewRecorder()
	h.ListSessionMaterials(listW, listReq)

	require.Equal(t, http.StatusOK, listW.Code, listW.Body.String())
	var list []*models.Material
	require.NoError(t, json.NewDecoder(listW.Body).Decode(&list))
	require.Len(t, list, 1, "session must have exactly one material after paste")

	m := list[0]
	assert.Equal(t, models.MaterialTextStatusReady, m.TextStatus, "persisted material must have text_status=ready")
	require.NotNil(t, m.ExtractedText, "persisted material must have non-nil extracted_text")
	assert.NotEmpty(t, *m.ExtractedText, "persisted material must have non-empty extracted_text")
	assert.Contains(t, *m.ExtractedText, "Meridian",
		"persisted extracted_text must contain the known keyword — proves content was extracted and stored, not just status updated")
}

// TestSmoke_MaterialIngestion_EmptyTextRejected verifies that empty paste is rejected with 400.
func TestSmoke_MaterialIngestion_EmptyTextRejected(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	session := createTestSessionForHandlers(t, h.DB, "Material Smoke Empty")

	body, _ := json.Marshal(map[string]any{"title": "Empty", "text": "   "})
	req := httptest.NewRequest(http.MethodPost, "/sessions/"+session.ID.String()+"/materials/paste", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.SessionPasteMaterial(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestSmoke_MaterialIngestion_ListEmptyForNewSession verifies list returns empty array for a new session.
func TestSmoke_MaterialIngestion_ListEmptyForNewSession(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	session := createTestSessionForHandlers(t, h.DB, "Material Smoke Empty List")

	req := httptest.NewRequest(http.MethodGet, "/sessions/"+session.ID.String()+"/materials", nil)
	w := httptest.NewRecorder()
	h.ListSessionMaterials(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var list []*models.Material
	require.NoError(t, json.NewDecoder(w.Body).Decode(&list))
	assert.Len(t, list, 0)
}

// TestSmoke_MaterialIngestion_PPTXUploadReturnsReady verifies that uploading a PPTX file returns
// text_status="ready" synchronously in the 201 response. PPTX text extraction uses a pure-Go
// parser and completes during the upload request without an async job.
func TestSmoke_MaterialIngestion_PPTXUploadReturnsReady(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()

	session := createTestSessionForHandlers(t, h.DB, "PPTX Smoke Session")

	// Build a minimal valid PPTX file with known content.
	tmp := t.TempDir()
	pptxPath := filepath.Join(tmp, "smoke_deck.pptx")
	createMinimalPptx(t, pptxPath, "Meridian PPTX smoke content")

	data, err := os.ReadFile(pptxPath)
	require.NoError(t, err)

	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	fw, err := mw.CreateFormFile("file", "smoke_deck.pptx")
	require.NoError(t, err)
	_, err = fw.Write(data)
	require.NoError(t, err)
	require.NoError(t, mw.Close())

	req := httptest.NewRequest(http.MethodPost, "/sessions/"+session.ID.String()+"/materials/upload", body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	w := httptest.NewRecorder()
	h.SessionUploadMaterial(w, req)

	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	var m map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&m))

	// Sync extraction contract: text_status must be "ready" and extracted_text non-empty
	// on the immediate 201 response — no async job should be needed for PPTX.
	assert.Equal(t, "ready", m["text_status"], "PPTX upload must return text_status=ready synchronously (not pending or processing)")
	assert.NotEmpty(t, m["extracted_text"], "PPTX upload must return extracted_text in the 201 response")
	assert.NotEmpty(t, m["id"], "material ID must be present in the 201 response")
}
