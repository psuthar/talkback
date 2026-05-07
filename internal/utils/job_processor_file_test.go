package utils

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/database"
	"github.com/psuthar/talkback/internal/markitdown"
	"github.com/psuthar/talkback/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// SCRUM-332 — handleFileExtraction success path: stub sidecar returns a
// markdown table, worker writes it to materials.extracted_text and flips
// the row to text_status=ready.
func TestHandleFileExtraction_CSV_SuccessUpdatesMaterial(t *testing.T) {
	jp, db, cleanup := setupJobProcessorTestDB(t)
	defer cleanup()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"text":"| name | role |\n| --- | --- |\n| Alex | Eng |","extension":".csv","bytes":21}`)
	}))
	defer srv.Close()
	jp.Markitdown = &markitdown.Client{
		BaseURL:     srv.URL,
		Secret:      "test-secret",
		HTTP:        srv.Client(),
		FileTimeout: 5 * time.Second,
	}
	require.True(t, jp.Markitdown.Enabled())

	mat, job, filePath, indexCalled := seedSpreadsheetJob(t, db, "happy-csv", "customers.csv", "text/csv", []byte("name,role\nAlex,Eng\n"))
	defer os.Remove(filePath)

	jp.OnTranscriptCompleted = func(_ uuid.UUID) { *indexCalled = true }
	jp.handleFileExtraction(context.Background(), job, mat, filePath, time.Now(), 0)

	got, err := db.GetMaterialByID(context.Background(), mat.ID)
	require.NoError(t, err)
	assert.Equal(t, models.MaterialTextStatusReady, got.TextStatus)
	require.NotNil(t, got.ExtractedText)
	assert.Contains(t, *got.ExtractedText, "| name | role |")
	assert.True(t, *indexCalled, "OnTranscriptCompleted must fire so the reindex picks up new chunks")
}

// XLSX with multi-sheet markdown — confirms ## SheetName headings flow
// through the end-to-end worker path unchanged.
func TestHandleFileExtraction_XLSX_MultiSheetSuccess(t *testing.T) {
	jp, db, cleanup := setupJobProcessorTestDB(t)
	defer cleanup()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"text":"## Sheet1\n\n| a | b |\n| --- | --- |\n| 1 | 2 |\n\n## Sheet2\n\n| x | y |\n| --- | --- |\n| 3 | 4 |","extension":".xlsx","bytes":2048}`)
	}))
	defer srv.Close()
	jp.Markitdown = &markitdown.Client{
		BaseURL:     srv.URL,
		Secret:      "test-secret",
		HTTP:        srv.Client(),
		FileTimeout: 5 * time.Second,
	}

	mat, job, filePath, indexCalled := seedSpreadsheetJob(t, db, "happy-xlsx", "report.xlsx", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", []byte("PK\x03\x04stub-xlsx"))
	defer os.Remove(filePath)

	jp.OnTranscriptCompleted = func(_ uuid.UUID) { *indexCalled = true }
	jp.handleFileExtraction(context.Background(), job, mat, filePath, time.Now(), 0)

	got, err := db.GetMaterialByID(context.Background(), mat.ID)
	require.NoError(t, err)
	assert.Equal(t, models.MaterialTextStatusReady, got.TextStatus)
	require.NotNil(t, got.ExtractedText)
	assert.Contains(t, *got.ExtractedText, "## Sheet1")
	assert.Contains(t, *got.ExtractedText, "## Sheet2")
	assert.True(t, *indexCalled)
}

// XLS legacy binary — verifies the .xls dispatch and that the worker
// passes the right extension hint to the sidecar.
func TestHandleFileExtraction_XLS_LegacyFormat(t *testing.T) {
	jp, db, cleanup := setupJobProcessorTestDB(t)
	defer cleanup()

	var seenExtension string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Confirm the file_extension form field carries .xls.
		require.NoError(t, r.ParseMultipartForm(2<<20))
		seenExtension = r.FormValue("file_extension")
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"text":"## Sheet1\n\n| col |\n| --- |\n| 1 |","extension":".xls","bytes":512}`)
	}))
	defer srv.Close()
	jp.Markitdown = &markitdown.Client{
		BaseURL:     srv.URL,
		Secret:      "test-secret",
		HTTP:        srv.Client(),
		FileTimeout: 5 * time.Second,
	}

	mat, job, filePath, _ := seedSpreadsheetJob(t, db, "happy-xls", "legacy.xls", "application/vnd.ms-excel", []byte("\xd0\xcf\x11\xe0fake-xls"))
	defer os.Remove(filePath)

	jp.handleFileExtraction(context.Background(), job, mat, filePath, time.Now(), 0)

	assert.Equal(t, ".xls", seenExtension, "worker must forward .xls extension to the sidecar")

	got, err := db.GetMaterialByID(context.Background(), mat.ID)
	require.NoError(t, err)
	assert.Equal(t, models.MaterialTextStatusReady, got.TextStatus)
}

// Sidecar 5xx after retry → text_status=failed with a non-empty error
// message. The reindex callback must NOT fire.
func TestHandleFileExtraction_SidecarUnavailableMarksFailed(t *testing.T) {
	jp, db, cleanup := setupJobProcessorTestDB(t)
	defer cleanup()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"down"}`, http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	jp.Markitdown = &markitdown.Client{
		BaseURL:     srv.URL,
		Secret:      "test-secret",
		HTTP:        srv.Client(),
		Retries:     0,
		FileTimeout: 2 * time.Second,
	}

	mat, job, filePath, indexCalled := seedSpreadsheetJob(t, db, "down-csv", "customers.csv", "text/csv", []byte("a,b\n1,2"))
	defer os.Remove(filePath)

	jp.OnTranscriptCompleted = func(_ uuid.UUID) { *indexCalled = true }
	jp.handleFileExtraction(context.Background(), job, mat, filePath, time.Now(), 0)

	got, err := db.GetMaterialByID(context.Background(), mat.ID)
	require.NoError(t, err)
	assert.Equal(t, models.MaterialTextStatusFailed, got.TextStatus)
	require.NotNil(t, got.ErrorMessage)
	assert.Contains(t, strings.ToLower(*got.ErrorMessage), "sidecar")
	assert.False(t, *indexCalled, "OnTranscriptCompleted must NOT fire on extraction failure")

	// Sanity: typed error sentinel surfaces from the client.
	_, err = jp.Markitdown.ExtractFile(context.Background(), []byte("x"), "text/csv", "x.csv", ".csv")
	assert.True(t, errors.Is(err, markitdown.ErrSidecarUnavailable))
}

// Stale job after a config change — markitdown client gone (nil) →
// text_status=failed with a clear "not configured" message rather than
// silently producing empty text.
func TestHandleFileExtraction_NoClientConfiguredMarksFailed(t *testing.T) {
	jp, db, cleanup := setupJobProcessorTestDB(t)
	defer cleanup()
	jp.Markitdown = nil

	mat, job, filePath, indexCalled := seedSpreadsheetJob(t, db, "noclient-csv", "customers.csv", "text/csv", []byte("x"))
	defer os.Remove(filePath)

	jp.OnTranscriptCompleted = func(_ uuid.UUID) { *indexCalled = true }
	jp.handleFileExtraction(context.Background(), job, mat, filePath, time.Now(), 0)

	got, err := db.GetMaterialByID(context.Background(), mat.ID)
	require.NoError(t, err)
	assert.Equal(t, models.MaterialTextStatusFailed, got.TextStatus)
	require.NotNil(t, got.ErrorMessage)
	assert.Contains(t, *got.ErrorMessage, "not configured")
	assert.False(t, *indexCalled)
}

// Material whose filename has no recognised spreadsheet extension AND no
// matching content type — the worker rejects rather than guessing.
func TestHandleFileExtraction_UnknownExtensionMarksFailed(t *testing.T) {
	jp, db, cleanup := setupJobProcessorTestDB(t)
	defer cleanup()
	jp.Markitdown = &markitdown.Client{BaseURL: "https://x", Secret: "s", FileTimeout: time.Second}

	// Empty content type AND an extension we'd never accept.
	mat, job, filePath, _ := seedSpreadsheetJob(t, db, "noext", "blob", "application/octet-stream", []byte("raw"))
	defer os.Remove(filePath)

	jp.handleFileExtraction(context.Background(), job, mat, filePath, time.Now(), 0)

	got, err := db.GetMaterialByID(context.Background(), mat.ID)
	require.NoError(t, err)
	assert.Equal(t, models.MaterialTextStatusFailed, got.TextStatus)
	require.NotNil(t, got.ErrorMessage)
	assert.Contains(t, strings.ToLower(*got.ErrorMessage), "extension not recognised")
}

// Empty markdown response → failed (the sidecar said success but there's
// nothing usable; mark the material so the user sees an explicit problem
// rather than a "ready" row with empty content).
func TestHandleFileExtraction_EmptyMarkdownMarksFailed(t *testing.T) {
	jp, db, cleanup := setupJobProcessorTestDB(t)
	defer cleanup()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"text":"   ","extension":".csv","bytes":0}`)
	}))
	defer srv.Close()
	jp.Markitdown = &markitdown.Client{
		BaseURL:     srv.URL,
		Secret:      "test-secret",
		HTTP:        srv.Client(),
		FileTimeout: 5 * time.Second,
	}

	mat, job, filePath, _ := seedSpreadsheetJob(t, db, "empty-csv", "customers.csv", "text/csv", []byte(""))
	defer os.Remove(filePath)

	jp.handleFileExtraction(context.Background(), job, mat, filePath, time.Now(), 0)

	got, err := db.GetMaterialByID(context.Background(), mat.ID)
	require.NoError(t, err)
	assert.Equal(t, models.MaterialTextStatusFailed, got.TextStatus)
	require.NotNil(t, got.ErrorMessage)
	assert.Contains(t, *got.ErrorMessage, "empty")
}

// ─── pure-unit helpers ────────────────────────────────────────────────

func TestIsSpreadsheetMaterial(t *testing.T) {
	cases := []struct {
		name        string
		filename    string
		contentType string
		want        bool
	}{
		{"csv by extension", "customers.csv", "text/plain", true},
		{"xlsx by extension", "report.XLSX", "application/octet-stream", true},
		{"xls by extension", "legacy.xls", "application/octet-stream", true},
		{"csv by content type", "blob", "text/csv", true},
		{"xlsx by content type", "blob", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", true},
		{"xls by content type", "blob", "application/vnd.ms-excel", true},
		{"pdf is not", "doc.pdf", "application/pdf", false},
		{"png is not", "shot.png", "image/png", false},
		{"docx is not", "memo.docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mat := &models.Material{Filename: tc.filename, ContentType: tc.contentType}
			if got := isSpreadsheetMaterial(mat); got != tc.want {
				t.Fatalf("filename=%q ct=%q: want %v, got %v", tc.filename, tc.contentType, tc.want, got)
			}
		})
	}
}

func TestSpreadsheetExtension(t *testing.T) {
	cases := []struct {
		name        string
		filename    string
		contentType string
		want        string
	}{
		{"csv lowercase", "customers.csv", "text/plain", ".csv"},
		{"csv uppercase", "REPORT.CSV", "", ".csv"},
		{"xlsx", "report.xlsx", "", ".xlsx"},
		{"xls", "legacy.xls", "", ".xls"},
		{"no extension, csv content type", "blob", "text/csv", ".csv"},
		{"no extension, xlsx content type", "blob", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", ".xlsx"},
		{"no match", "blob", "application/octet-stream", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mat := &models.Material{Filename: tc.filename, ContentType: tc.contentType}
			if got := spreadsheetExtension(mat); got != tc.want {
				t.Fatalf("filename=%q ct=%q: want %q, got %q", tc.filename, tc.contentType, tc.want, got)
			}
		})
	}
}

// ─── seed helper ──────────────────────────────────────────────────────

// seedSpreadsheetJob mirrors seedImageJob but creates a kind=document
// material with the supplied filename / content type / bytes, and a
// queued TranscriptJob row.
func seedSpreadsheetJob(t *testing.T, db *database.DB, label, filename, contentType string, bytes []byte) (*models.Material, *models.TranscriptJob, string, *bool) {
	t.Helper()
	ctx := context.Background()
	sess := &models.Session{ID: uuid.New(), Title: label, Status: models.SessionStatusOpen}
	require.NoError(t, db.CreateSession(ctx, sess))
	artifact, err := db.CreateArtifact(ctx, sess.ID, label+"-artifact", nil)
	require.NoError(t, err)

	tmp := t.TempDir()
	filePath := filepath.Join(tmp, filename)
	require.NoError(t, os.WriteFile(filePath, bytes, 0o644))

	mat := &models.Material{
		ID:          uuid.New(),
		ArtifactID:  artifact.ID,
		SessionID:   sess.ID,
		Kind:        string(models.MaterialKindDocument),
		Filename:    filename,
		ContentType: contentType,
		StorageURL:  filePath,
		TextStatus:  models.MaterialTextStatusPending,
	}
	require.NoError(t, db.CreateMaterial(ctx, mat))

	job := &models.TranscriptJob{
		ID:         uuid.New(),
		MaterialID: &mat.ID,
		SessionID:  sess.ID,
		Status:     models.TranscriptJobStatusQueued,
		SourceURL:  filePath,
		JobKey:     "material_extract:" + mat.ID.String(),
		QueuedAt:   time.Now(),
	}
	require.NoError(t, db.CreateTranscriptJob(ctx, job))

	called := false
	return mat, job, filePath, &called
}
