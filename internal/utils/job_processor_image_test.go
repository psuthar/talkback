package utils

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"github.com/psuthar/talkback/internal/database"
	"github.com/psuthar/talkback/internal/markitdown"
	"github.com/psuthar/talkback/internal/migrations"
	"github.com/psuthar/talkback/internal/models"
	"github.com/psuthar/talkback/internal/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// SCRUM-303 — handleImageExtraction success path: stub sidecar returns a
// caption, worker writes it to materials.extracted_text and flips the row
// to text_status=ready. The OnTranscriptCompleted callback fires so the
// existing reindex pathway picks up the new chunks.
func TestHandleImageExtraction_SuccessUpdatesMaterial(t *testing.T) {
	jp, db, cleanup := setupJobProcessorTestDB(t)
	defer cleanup()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"text":"# Slide title\n\nA whiteboard photo with body text.","model":"gpt-4o-mini","tokens_used":120}`)
	}))
	defer srv.Close()
	jp.Markitdown = &markitdown.Client{
		BaseURL:      srv.URL,
		Secret:       "test-secret",
		HTTP:         srv.Client(),
		ImageTimeout: 5 * time.Second,
	}
	require.True(t, jp.Markitdown.Enabled())

	mat, job, filePath, indexCalled := seedImageJob(t, db, "happy-image")
	defer os.Remove(filePath)

	jp.OnTranscriptCompleted = func(_ uuid.UUID) { *indexCalled = true }
	jp.handleImageExtraction(context.Background(), job, mat, filePath, time.Now(), 0)

	got, err := db.GetMaterialByID(context.Background(), mat.ID)
	require.NoError(t, err)
	assert.Equal(t, models.MaterialTextStatusReady, got.TextStatus)
	require.NotNil(t, got.ExtractedText)
	assert.Contains(t, *got.ExtractedText, "Slide title")
	assert.True(t, *indexCalled, "OnTranscriptCompleted must fire so the reindex picks up new chunks")
}

// SCRUM-303 — sidecar unreachable / 5xx-after-retry → text_status=failed
// with error_message populated. The reindex callback must NOT fire when
// extraction failed.
func TestHandleImageExtraction_SidecarUnavailableMarksFailed(t *testing.T) {
	jp, db, cleanup := setupJobProcessorTestDB(t)
	defer cleanup()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"down"}`, http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	jp.Markitdown = &markitdown.Client{
		BaseURL:      srv.URL,
		Secret:       "test-secret",
		HTTP:         srv.Client(),
		Retries:      0,
		ImageTimeout: 2 * time.Second,
	}

	mat, job, filePath, indexCalled := seedImageJob(t, db, "down-image")
	defer os.Remove(filePath)

	jp.OnTranscriptCompleted = func(_ uuid.UUID) { *indexCalled = true }
	jp.handleImageExtraction(context.Background(), job, mat, filePath, time.Now(), 0)

	got, err := db.GetMaterialByID(context.Background(), mat.ID)
	require.NoError(t, err)
	assert.Equal(t, models.MaterialTextStatusFailed, got.TextStatus)
	require.NotNil(t, got.ErrorMessage)
	assert.NotEmpty(t, *got.ErrorMessage)
	// Confirm the typed error from the client surfaces in the message.
	assert.Contains(t, strings.ToLower(*got.ErrorMessage), "sidecar")
	assert.False(t, *indexCalled, "OnTranscriptCompleted must NOT fire on extraction failure")

	// Sanity: errors.Is on the client's wrapped error is the expected sentinel.
	_, err = jp.Markitdown.ExtractImage(context.Background(), []byte("x"), "image/png")
	assert.True(t, errors.Is(err, markitdown.ErrSidecarUnavailable))
}

// SCRUM-303 — when the JobProcessor has no markitdown client configured
// (nil or .Enabled()==false), handleImageExtraction marks the material
// failed with a clear message rather than silently producing empty text.
// (At upload time the gate prevents this from happening; this is a
// defense-in-depth check for stale jobs after a config change.)
func TestHandleImageExtraction_NoClientConfiguredMarksFailed(t *testing.T) {
	jp, db, cleanup := setupJobProcessorTestDB(t)
	defer cleanup()
	jp.Markitdown = nil

	mat, job, filePath, indexCalled := seedImageJob(t, db, "noclient-image")
	defer os.Remove(filePath)

	jp.OnTranscriptCompleted = func(_ uuid.UUID) { *indexCalled = true }
	jp.handleImageExtraction(context.Background(), job, mat, filePath, time.Now(), 0)

	got, err := db.GetMaterialByID(context.Background(), mat.ID)
	require.NoError(t, err)
	assert.Equal(t, models.MaterialTextStatusFailed, got.TextStatus)
	require.NotNil(t, got.ErrorMessage)
	assert.Contains(t, *got.ErrorMessage, "not configured")
	assert.False(t, *indexCalled)
}

// ─── helpers ────────────────────────────────────────────────────────────

// setupJobProcessorTestDB returns a JobProcessor pointed at a fresh,
// isolated test database with migrations applied. The returned cleanup
// closes the pool and drops the DB.
func setupJobProcessorTestDB(t *testing.T) (*JobProcessor, *database.DB, func()) {
	t.Helper()
	databaseURL, cleanupDB := test.SetupTestDB(t)
	if err := runUtilsTestMigrations(databaseURL); err != nil {
		cleanupDB()
		t.Fatalf("run test migrations: %v", err)
	}
	db, err := database.NewFromURL(databaseURL)
	require.NoError(t, err)
	cleanup := func() {
		db.Close()
		cleanupDB()
	}
	jp := NewJobProcessor(db, 1)
	return jp, db, cleanup
}

func runUtilsTestMigrations(databaseURL string) error {
	migrationsSubFS, err := fs.Sub(migrations.MigrationsFS, "migrations")
	if err != nil {
		return err
	}
	sourceDriver, err := iofs.New(migrationsSubFS, ".")
	if err != nil {
		return err
	}
	conn, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return err
	}
	defer conn.Close()
	postgresDriver, err := postgres.WithInstance(conn, &postgres.Config{})
	if err != nil {
		return err
	}
	m, err := migrate.NewWithInstance("iofs", sourceDriver, "postgres", postgresDriver)
	if err != nil {
		return err
	}
	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return err
	}
	return nil
}

// seedImageJob creates a session, artifact, image-kind material, and a
// queued TranscriptJob row. Writes a tiny fake PNG to a temp file. Returns
// (material, job, filePath, indexCalled) — caller flips indexCalled in
// OnTranscriptCompleted to assert callback firing.
func seedImageJob(t *testing.T, db *database.DB, label string) (*models.Material, *models.TranscriptJob, string, *bool) {
	t.Helper()
	ctx := context.Background()
	sess := &models.Session{ID: uuid.New(), Title: label, Status: models.SessionStatusOpen}
	require.NoError(t, db.CreateSession(ctx, sess))
	artifact, err := db.CreateArtifact(ctx, sess.ID, label+"-artifact", nil)
	require.NoError(t, err)

	tmp := t.TempDir()
	filePath := filepath.Join(tmp, label+".png")
	require.NoError(t, os.WriteFile(filePath, []byte("\x89PNG\r\n\x1a\nfake-image-bytes"), 0o644))

	mat := &models.Material{
		ID:          uuid.New(),
		ArtifactID:  artifact.ID,
		SessionID:   sess.ID,
		Kind:        string(models.MaterialKindImage),
		Filename:    label + ".png",
		ContentType: "image/png",
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
		JobKey:     "image_extract:" + mat.ID.String(),
		QueuedAt:   time.Now(),
	}
	require.NoError(t, db.CreateTranscriptJob(ctx, job))

	called := false
	return mat, job, filePath, &called
}
