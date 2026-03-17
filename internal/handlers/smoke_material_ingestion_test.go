package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
