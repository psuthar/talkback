package handlers

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/psuthar/talkback/internal/markitdown"
	"github.com/psuthar/talkback/internal/models"
	"github.com/psuthar/talkback/internal/utils"
)

// SCRUM-371: handler-level tests for the new synchronous Go-native CSV
// extraction path. Companion unit tests for CSVToMarkdown live in
// internal/utils/csv_to_markdown_test.go.

// uploadCSVMaterial is a focused helper for these tests: posts a
// multipart upload of `payload` as `filename` and returns the recorder
// plus the created material id (when 201). Mirrors the inline helper in
// the size-cap test so this file is self-contained.
func uploadCSVMaterial(t *testing.T, h *Handlers, sessionID uuid.UUID, payload []byte, filename string) (*httptest.ResponseRecorder, *uuid.UUID) {
	t.Helper()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	fw, err := writer.CreateFormFile("file", filename)
	require.NoError(t, err)
	_, _ = fw.Write(payload)
	require.NoError(t, writer.Close())
	req := httptest.NewRequest(http.MethodPost, "/sessions/"+sessionID.String()+"/materials/upload", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()
	h.SessionUploadMaterial(w, req)

	if w.Code != http.StatusCreated {
		return w, nil
	}
	// On 201 the handler returns the material; pull its id off the
	// session's materials list rather than parsing the response body
	// (whose schema is broader than this test needs).
	mats, err := h.DB.GetMaterialsBySessionID(context.Background(), sessionID)
	require.NoError(t, err)
	for i := range mats {
		if mats[i].Filename == filename {
			return w, &mats[i].ID
		}
	}
	t.Fatalf("uploaded material %q not found in session %s", filename, sessionID)
	return w, nil
}

func transcriptJobsForMaterial(t *testing.T, h *Handlers, materialID uuid.UUID) int {
	t.Helper()
	jobKey := "material_extract:" + materialID.String()
	job, err := h.DB.GetTranscriptJobByKey(context.Background(), jobKey)
	require.NoError(t, err)
	if job == nil {
		return 0
	}
	return 1
}

// 1. Sidecar disabled (default): a well-formed CSV is extracted
// synchronously and lands at text_status=ready with the rendered
// markdown table in extracted_text. Critically, NO transcript_jobs row
// is created — the regression guard for the production failure mode.
func TestSessionUploadMaterial_CSV_SyncExtraction_Success(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	session := createTestSessionForHandlers(t, h.DB, "SCRUM-371 CSV sync success")

	csv := []byte("name,role\nAlex,Eng\nJordan,SRE\n")
	w, matID := uploadCSVMaterial(t, h, session.ID, csv, "team.csv")
	require.Equal(t, http.StatusCreated, w.Code)
	require.NotNil(t, matID)

	mat, err := h.DB.GetMaterialByID(context.Background(), *matID)
	require.NoError(t, err)
	require.NotNil(t, mat)
	assert.Equal(t, models.MaterialTextStatusReady, mat.TextStatus,
		"CSV with sidecar disabled must reach ready synchronously, not stay pending")
	require.NotNil(t, mat.ExtractedText, "extracted_text must be populated")
	assert.Contains(t, *mat.ExtractedText, "| name | role |", "expected markdown header row")
	assert.Contains(t, *mat.ExtractedText, "| Alex | Eng |", "expected first body row")
	assert.Contains(t, *mat.ExtractedText, "| Jordan | SRE |", "expected second body row")

	// Regression guard: this used to silently leave a transcript_jobs row
	// pending forever (or no row at all + status=pending). Now the
	// handler should produce neither a job nor a stuck pending row.
	assert.Equal(t, 0, transcriptJobsForMaterial(t, h, *matID),
		"sync-success CSV must not enqueue a transcript_jobs row")
}

// 2. Explicit "no stuck pending" guard. Same upload as #1, but asserts
// the negative — text_status must not be pending. Loud regression
// signal for the original bug shape.
func TestSessionUploadMaterial_CSV_SidecarDisabled_NoStuckPending(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	session := createTestSessionForHandlers(t, h.DB, "SCRUM-371 no-stuck-pending guard")

	w, matID := uploadCSVMaterial(t, h, session.ID, []byte("a,b\n1,2\n"), "tiny.csv")
	require.Equal(t, http.StatusCreated, w.Code)
	mat, err := h.DB.GetMaterialByID(context.Background(), *matID)
	require.NoError(t, err)
	require.NotNil(t, mat)
	assert.NotEqual(t, models.MaterialTextStatusPending, mat.TextStatus,
		"sidecar-disabled CSV must never land in `pending` (the production bug shape)")
}

// 3. Oversized CSV still hits the existing SCRUM-334 spreadsheet cap
// (5 MiB default). The CSV sync path is downstream of the cap check so
// this is a regression guard, not new behavior. We use a lowered cap
// to keep the fixture tiny.
func TestSessionUploadMaterial_CSV_OversizedSpreadsheetCap(t *testing.T) {
	// NOT t.Parallel(): t.Setenv on a process-global env var.
	t.Setenv("TALKBACK_SPREADSHEET_MAX_BYTES", "1024")
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	session := createTestSessionForHandlers(t, h.DB, "SCRUM-371 size-cap regression")

	over := bytes.Repeat([]byte("z"), 2048) // > 1 KiB cap
	w, _ := uploadCSVMaterial(t, h, session.ID, over, "huge.csv")
	assert.Equal(t, http.StatusRequestEntityTooLarge, w.Code,
		"the SCRUM-334 spreadsheet cap must still fire before the SCRUM-371 sync path")
}

// 4. Sidecar disabled + a CSV the helper rejects (whitespace-only, which
// CSVToMarkdown returns ErrEmptyCSV for). With no sidecar fallback, the
// handler must surface this as `failed` with a populated error_message
// — never silent `pending`.
func TestSessionUploadMaterial_CSV_MalformedFallsBackToFailed(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	session := createTestSessionForHandlers(t, h.DB, "SCRUM-371 malformed → failed")

	// Whitespace-only — CSVToMarkdown rejects with ErrEmptyCSV. With
	// sidecar disabled (default test handler), no fallback exists.
	w, matID := uploadCSVMaterial(t, h, session.ID, []byte("   \n  \n"), "blank.csv")
	require.Equal(t, http.StatusCreated, w.Code)
	mat, err := h.DB.GetMaterialByID(context.Background(), *matID)
	require.NoError(t, err)
	require.NotNil(t, mat)
	assert.Equal(t, models.MaterialTextStatusFailed, mat.TextStatus,
		"sidecar-disabled malformed CSV must land at failed, not pending")
	require.NotNil(t, mat.ErrorMessage, "error_message must be populated for diagnosis")
	assert.Contains(t, strings.ToLower(*mat.ErrorMessage), "csv",
		"error_message should mention csv extraction")
}

// 5. Sidecar enabled + same malformed CSV: must route to the existing
// async sidecar path. text_status=pending AND a transcript_jobs row
// keyed `material_extract:<id>` must exist. Confirms we did NOT
// short-circuit the sidecar for inputs the sync path can't handle.
func TestSessionUploadMaterial_CSV_MalformedFallsBackToSidecar_WhenEnabled(t *testing.T) {
	// NOT t.Parallel(): t.Setenv on process-global env vars.
	t.Setenv(envFileExtractionForTest, "true") // MARKITDOWN_FILE_EXTRACTION_ENABLED
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	session := createTestSessionForHandlers(t, h.DB, "SCRUM-371 malformed → sidecar")

	// Stand up a non-nil markitdown client with Enabled()==true. The
	// real sidecar isn't reachable (URL is invalid); we only need
	// fileExtractionGate to evaluate true so the upload handler routes
	// to the async path and creates the transcript_jobs row.
	h.Markitdown = &markitdown.Client{
		BaseURL: "http://test-sidecar.invalid",
		Secret:  "test-secret",
		HTTP:    &http.Client{},
	}
	require.True(t, markitdown.FileExtractionEnabled(), "env gate not set")
	require.True(t, h.Markitdown.Enabled(), "markitdown client not enabled")

	// JobProcessor is required for the conditional enqueue + DB write.
	// We don't start it (no workers needed); Enqueue will return "not
	// running" but CreateTranscriptJob still persists the row, which is
	// what this test checks.
	h.JobProcessor = utils.NewJobProcessor(h.DB, 1)

	w, matID := uploadCSVMaterial(t, h, session.ID, []byte("   \n  \n"), "blank.csv")
	require.Equal(t, http.StatusCreated, w.Code)
	mat, err := h.DB.GetMaterialByID(context.Background(), *matID)
	require.NoError(t, err)
	require.NotNil(t, mat)
	assert.Equal(t, models.MaterialTextStatusPending, mat.TextStatus,
		"sync-failed CSV with sidecar enabled must defer to async (pending)")
	assert.Equal(t, 1, transcriptJobsForMaterial(t, h, *matID),
		"sidecar fallback must create a transcript_jobs row for the worker")
}

// 6. Regression guard: .xlsx upload behavior is unchanged by this
// ticket. The .csv-only carve-out in the upload switch must not change
// how .xlsx routes. The existing pure-Go office extractor will reject
// a non-zip payload and the handler falls through to async (pending).
// The contract this test pins: .xlsx never lands in `failed` purely
// because of the SCRUM-371 carve-out.
func TestSessionUploadMaterial_XLSX_StillUsesSidecarOrSyncOffice(t *testing.T) {
	t.Parallel()
	h, cleanup := setupTestHandlersParallel(t)
	defer cleanup()
	session := createTestSessionForHandlers(t, h.DB, "SCRUM-371 xlsx regression")

	// 32 bytes of non-zip garbage. The office extractor will fail to
	// parse it; the handler's existing fall-through sets text_status to
	// pending (deferring to the async worker). The point of the test is
	// to confirm the .csv carve-out didn't accidentally route .xlsx
	// through the failed-fast path.
	payload := bytes.Repeat([]byte{0x01, 0x02, 0x03, 0x04}, 8)
	w, matID := uploadCSVMaterial(t, h, session.ID, payload, "regression.xlsx")
	require.Equal(t, http.StatusCreated, w.Code)
	mat, err := h.DB.GetMaterialByID(context.Background(), *matID)
	require.NoError(t, err)
	require.NotNil(t, mat)
	assert.NotEqual(t, models.MaterialTextStatusFailed, mat.TextStatus,
		"XLSX must not regress to failed because of the SCRUM-371 .csv carve-out")
}

// envFileExtractionForTest is the env var that flips
// markitdown.FileExtractionEnabled() to true. Defining it here keeps
// the test from importing internal markitdown constants.
const envFileExtractionForTest = "MARKITDOWN_FILE_EXTRACTION_ENABLED"
