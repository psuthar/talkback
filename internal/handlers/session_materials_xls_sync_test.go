package handlers

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/psuthar/talkback/internal/models"
)

// SCRUM-395: handler-level tests for the synchronous pure-Go legacy .xls
// extraction path and the "no extraction handler" fail-safe. Companion
// unit tests for extractXls live in internal/utils/office_extractor_test.go.
//
// Before SCRUM-395, a .xls upload matched no case in SessionUploadMaterial's
// type switch (isOfficeFile() doesn't recognize the legacy binary format and
// the only branch that touched .xls was the markitdown-sidecar gate, which is
// off in production), so the material was created with text_status=pending and
// no extraction job was ever enqueued — the sidebar showed a permanent
// "Processing…" with no error. These tests pin the fix.

func readXlsFixture(t *testing.T) []byte {
	t.Helper()
	// Shared 2-sheet fixture; same bytes the e2e suite uses.
	b, err := os.ReadFile(filepath.Join("testdata", "test.xls"))
	require.NoError(t, err, "read testdata/test.xls")
	return b
}

// A real legacy .xls is parsed synchronously at upload: text_status=ready,
// extracted_text populated from the BIFF reader, and — the regression guard —
// NO transcript_jobs row (no sidecar dependency, no stuck pending).
func TestSessionUploadMaterial_XLS_SyncExtraction_Success(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	session := createTestSessionForHandlers(t, h.DB, "SCRUM-395 xls sync success")

	w, matID := uploadCSVMaterial(t, h, session.ID, readXlsFixture(t), "customers.xls")
	require.Equal(t, http.StatusCreated, w.Code)
	require.NotNil(t, matID)

	mat, err := h.DB.GetMaterialByID(context.Background(), *matID)
	require.NoError(t, err)
	require.NotNil(t, mat)
	assert.Equal(t, models.MaterialTextStatusReady, mat.TextStatus,
		".xls must reach ready synchronously, not stay pending forever (the production bug shape)")
	require.NotNil(t, mat.ExtractedText, "extracted_text must be populated")
	assert.Contains(t, *mat.ExtractedText, "Alex Chen", "expected a known cell value from the .xls fixture")
	assert.Contains(t, *mat.ExtractedText, "leads notification platform", "expected a cell from the second sheet")
	// SCRUM-396: extracted_text is a GFM markdown table (so SpreadsheetViewer
	// renders a table, not a run-on paragraph) — header row + `| --- |` separator.
	assert.Contains(t, *mat.ExtractedText, "| --- |", "extracted_text must be a markdown table (alignment separator present)")
	assert.True(t, strings.HasPrefix(strings.TrimSpace(*mat.ExtractedText), "| "), "extracted_text must start with a markdown table row")
	assert.Nil(t, mat.ErrorMessage, "successful extraction must not set error_message")

	assert.Equal(t, 0, transcriptJobsForMaterial(t, h, *matID),
		"sync-success .xls must not enqueue a transcript_jobs row")
}

// Explicit negative guard on the original bug: a .xls upload must never land
// in text_status=pending.
func TestSessionUploadMaterial_XLS_NoStuckPending(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	session := createTestSessionForHandlers(t, h.DB, "SCRUM-395 xls no-stuck-pending")

	w, matID := uploadCSVMaterial(t, h, session.ID, readXlsFixture(t), "report.xls")
	require.Equal(t, http.StatusCreated, w.Code)
	mat, err := h.DB.GetMaterialByID(context.Background(), *matID)
	require.NoError(t, err)
	require.NotNil(t, mat)
	assert.NotEqual(t, models.MaterialTextStatusPending, mat.TextStatus,
		".xls must never land in `pending` — that's the permanent-\"Processing…\" bug")
}

// A .xls-named upload that isn't actually a BIFF/OLE2 workbook must fail
// loudly (text_status=failed + error_message), never silent pending.
func TestSessionUploadMaterial_XLS_Corrupt_FailsNotPending(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	session := createTestSessionForHandlers(t, h.DB, "SCRUM-395 corrupt xls → failed")

	w, matID := uploadCSVMaterial(t, h, session.ID, []byte("name,role\nAlex,Eng\n"), "not-really.xls")
	require.Equal(t, http.StatusCreated, w.Code)
	mat, err := h.DB.GetMaterialByID(context.Background(), *matID)
	require.NoError(t, err)
	require.NotNil(t, mat)
	assert.Equal(t, models.MaterialTextStatusFailed, mat.TextStatus,
		"a corrupt .xls must land at failed, not pending")
	require.NotNil(t, mat.ErrorMessage, "error_message must be populated for diagnosis")
	assert.Contains(t, strings.ToLower(*mat.ErrorMessage), "xls", "error_message should mention xls extraction")
	assert.Equal(t, 0, transcriptJobsForMaterial(t, h, *matID),
		"a failed-fast .xls must not enqueue a transcript_jobs row")
}

// SCRUM-395 fail-safe: any upload type with no extraction handler (e.g. an
// unrecognized extension/content-type) must be created as failed with a clear
// error_message — never left in the initial text_status=pending, which the UI
// renders as a permanent "Processing…" spinner.
func TestSessionUploadMaterial_UnsupportedType_FailsNotPending(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	session := createTestSessionForHandlers(t, h.DB, "SCRUM-395 unsupported type → failed")

	w, matID := uploadCSVMaterial(t, h, session.ID, []byte{0x00, 0x01, 0x02, 0x03, 0x04, 0x05}, "mystery.bin")
	require.Equal(t, http.StatusCreated, w.Code)
	mat, err := h.DB.GetMaterialByID(context.Background(), *matID)
	require.NoError(t, err)
	require.NotNil(t, mat)
	assert.Equal(t, models.MaterialTextStatusFailed, mat.TextStatus,
		"an upload with no extraction handler must be failed, not pending")
	require.NotNil(t, mat.ErrorMessage, "error_message must explain why")
	assert.Contains(t, strings.ToLower(*mat.ErrorMessage), "no text-extraction handler",
		"error_message should name the missing-handler reason")
	assert.Equal(t, 0, transcriptJobsForMaterial(t, h, *matID),
		"a no-handler upload must not enqueue a transcript_jobs row")
}
