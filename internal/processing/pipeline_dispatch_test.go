package processing

import (
	"context"
	"database/sql"
	"io/fs"
	"log"
	"os"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	_ "github.com/lib/pq"
	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/database"
	"github.com/psuthar/talkback/internal/migrations"
	"github.com/psuthar/talkback/internal/models"
	"github.com/psuthar/talkback/internal/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMain sets up a shared test database for the processing package tests.
func TestMain(m *testing.M) {
	sharedURL, cleanupDB, err := test.CreateSharedTestDB()
	if err != nil {
		log.Fatalf("Create shared test DB: %v", err)
	}
	if err := runProcessingTestMigrations(sharedURL); err != nil {
		_ = test.DropTestDatabaseURL(sharedURL)
		log.Fatalf("Run test migrations: %v", err)
	}
	originalURL := os.Getenv("DATABASE_URL")
	os.Setenv("DATABASE_URL", sharedURL)
	code := m.Run()
	if cleanupDB != nil {
		cleanupDB()
	}
	if originalURL != "" {
		os.Setenv("DATABASE_URL", originalURL)
	} else {
		os.Unsetenv("DATABASE_URL")
	}
	os.Exit(code)
}

func runProcessingTestMigrations(databaseURL string) error {
	migrationsSubFS, err := fs.Sub(migrations.MigrationsFS, "migrations")
	if err != nil {
		return err
	}
	sourceDriver, err := iofs.New(migrationsSubFS, ".")
	if err != nil {
		return err
	}
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return err
	}
	defer db.Close()
	postgresDriver, err := postgres.WithInstance(db, &postgres.Config{})
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

// setupProcessingTestDB creates an isolated test DB for a single test. Returns DB and cleanup.
func setupProcessingTestDB(t *testing.T) (*database.DB, func()) {
	t.Helper()
	databaseURL, cleanupDB := test.SetupTestDB(t)
	if err := runProcessingTestMigrations(databaseURL); err != nil {
		cleanupDB()
		t.Fatalf("run test migrations: %v", err)
	}
	db, err := database.NewFromURL(databaseURL)
	require.NoError(t, err)
	cleanup := func() {
		db.Close()
		cleanupDB()
	}
	return db, cleanup
}

// seedProcessingJob creates a session and a processing job with the given source and returns them.
func seedProcessingJob(t *testing.T, db *database.DB, source string) (*models.Session, *models.SessionProcessingJob) {
	t.Helper()
	ctx := context.Background()

	sess := &models.Session{
		ID:     uuid.New(),
		Title:  "Dispatch Test Session " + source,
		Status: models.SessionStatusOpen,
	}
	require.NoError(t, db.CreateSession(ctx, sess))

	meetingUUID := "dispatch-test-meeting"
	creatorIdentity := "dispatch@smoke.test"
	job := &models.SessionProcessingJob{
		ID:              uuid.New(),
		SessionID:       sess.ID,
		Source:          source,
		State:           models.ProcessingStateQueued,
		Stage:           models.ProcessingStageFetch,
		MeetingUUID:     &meetingUUID,
		InstanceUUID:    &meetingUUID,
		CreatorIdentity: &creatorIdentity,
	}
	require.NoError(t, db.CreateOrGetSessionProcessingJob(ctx, job))
	return sess, job
}

// TestPipelineDispatch_TeamsWithNoTokenFails verifies that RunJob for a "teams" source
// job, when getTeamsToken is nil, marks the job as failed_permanent with error code
// "teams_not_configured" and does not return an error itself.
// This covers the new case "teams": branch in RunJob.
func TestPipelineDispatch_TeamsWithNoTokenFails(t *testing.T) {
	t.Parallel()
	db, cleanup := setupProcessingTestDB(t)
	defer cleanup()

	_, job := seedProcessingJob(t, db, models.SessionProcessingJobSourceTeams)

	// Call RunJob with nil getTeamsToken to trigger the teams_not_configured path.
	err := RunJob(context.Background(), db, job, nil, nil, nil, nil, "", nil, nil)

	// RunJob must return nil — it handles the failure internally.
	assert.NoError(t, err, "RunJob must return nil for teams dispatch (failure recorded in DB)")

	// Verify DB: job should be failed_permanent with "teams_not_configured" error code.
	ctx := context.Background()
	updated, dbErr := db.GetSessionProcessingJobByID(ctx, job.ID)
	require.NoError(t, dbErr)
	require.NotNil(t, updated)
	assert.Equal(t, models.ProcessingStateFailedPermanent, updated.State,
		"teams job with no token must be failed_permanent")
	require.NotNil(t, updated.LastErrorCode)
	assert.Equal(t, "teams_not_configured", *updated.LastErrorCode)
}

// TestPipelineDispatch_UnsupportedSourceFails verifies that RunJob for an unknown source
// marks the job as failed_permanent with error code "unsupported_source" and returns nil.
// This covers the default: branch in RunJob.
// Note: "unsupported" is not a valid DB CHECK value, so we directly invoke the dispatch
// logic by creating a job row that bypasses the DB constraint.
func TestPipelineDispatch_UnsupportedSourceFails(t *testing.T) {
	t.Parallel()
	db, cleanup := setupProcessingTestDB(t)
	defer cleanup()

	ctx := context.Background()

	// We cannot INSERT source="unsupported" due to the DB CHECK constraint.
	// Instead, create a valid job first, then build a synthetic in-memory job struct
	// that references the same DB row but uses an unsupported source string.
	// The dispatch switch uses job.Source from the struct, not from the DB,
	// so this accurately tests the default: branch without violating the constraint.
	_, realJob := seedProcessingJob(t, db, models.SessionProcessingJobSourceTeams)

	// Override Source in memory to trigger the default: branch.
	syntheticJob := *realJob
	syntheticJob.Source = "unsupported"

	err := RunJob(ctx, db, &syntheticJob, nil, nil, nil, nil, "", nil, nil)

	assert.NoError(t, err, "RunJob must return nil for unsupported source (failure recorded in DB)")

	// Verify the DB row (keyed by job ID, which is the same) is marked failed_permanent.
	updated, dbErr := db.GetSessionProcessingJobByID(ctx, realJob.ID)
	require.NoError(t, dbErr)
	require.NotNil(t, updated)
	assert.Equal(t, models.ProcessingStateFailedPermanent, updated.State,
		"unsupported source must be failed_permanent")
	require.NotNil(t, updated.LastErrorCode)
	assert.Equal(t, "unsupported_source", *updated.LastErrorCode)
}

// TestPipelineDispatch_GoogleMeetWithNoTokenFails covers the new google_meet
// switch arm: nil getGoogleMeetToken → failed_permanent + google_meet_not_configured.
func TestPipelineDispatch_GoogleMeetWithNoTokenFails(t *testing.T) {
	t.Parallel()
	db, cleanup := setupProcessingTestDB(t)
	defer cleanup()

	_, job := seedProcessingJob(t, db, models.SessionProcessingJobSourceGoogleMeet)

	err := RunJob(context.Background(), db, job, nil, nil, nil, nil, "", nil, nil)
	assert.NoError(t, err, "RunJob must return nil for google_meet dispatch (failure recorded in DB)")

	updated, dbErr := db.GetSessionProcessingJobByID(context.Background(), job.ID)
	require.NoError(t, dbErr)
	require.NotNil(t, updated)
	assert.Equal(t, models.ProcessingStateFailedPermanent, updated.State)
	require.NotNil(t, updated.LastErrorCode)
	assert.Equal(t, "google_meet_not_configured", *updated.LastErrorCode)
}

// TestSetJobFailedTransient_EscalatesToPermanentAfterMaxAttempts verifies that
// setJobFailedTransient escalates to failed_permanent when attempt > maxTransientAttempts.
// This prevents infinite retry loops for consistently-failing jobs.
func TestSetJobFailedTransient_EscalatesToPermanentAfterMaxAttempts(t *testing.T) {
	t.Parallel()
	db, cleanup := setupProcessingTestDB(t)
	defer cleanup()

	ctx := context.Background()
	_, job := seedProcessingJob(t, db, models.SessionProcessingJobSourceTeams)

	// Simulate attempt count past the threshold.
	overAttempt := maxTransientAttempts + 1
	code := "teams_fetch_recording"
	msg := "graph api error"
	next := time.Now().Add(BackoffTransient(overAttempt))
	setJobFailedTransient(ctx, db, job.ID, overAttempt, code, msg, next)

	updated, err := db.GetSessionProcessingJobByID(ctx, job.ID)
	require.NoError(t, err)
	require.NotNil(t, updated)
	assert.Equal(t, models.ProcessingStateFailedPermanent, updated.State,
		"transient failure past maxTransientAttempts must escalate to failed_permanent")
	require.NotNil(t, updated.LastErrorCode)
	assert.Equal(t, code, *updated.LastErrorCode)
}

// TestSetJobWaiting_EscalatesToPermanentAfterMaxAttempts verifies that
// setJobWaiting escalates to failed_permanent when attempt > maxWaitingAttempts.
// This stops indefinite waiting for transcripts that will never arrive.
func TestSetJobWaiting_EscalatesToPermanentAfterMaxAttempts(t *testing.T) {
	t.Parallel()
	db, cleanup := setupProcessingTestDB(t)
	defer cleanup()

	ctx := context.Background()
	_, job := seedProcessingJob(t, db, models.SessionProcessingJobSourceTeams)

	overAttempt := maxWaitingAttempts + 1
	code := "teams_transcript_unavailable"
	msg := "No Teams transcript available"
	next := time.Now().Add(BackoffWaiting(overAttempt))
	setJobWaiting(ctx, db, job.ID, overAttempt, code, msg, next)

	updated, err := db.GetSessionProcessingJobByID(ctx, job.ID)
	require.NoError(t, err)
	require.NotNil(t, updated)
	assert.Equal(t, models.ProcessingStateFailedPermanent, updated.State,
		"waiting past maxWaitingAttempts must escalate to failed_permanent")
	require.NotNil(t, updated.LastErrorCode)
	assert.Equal(t, code, *updated.LastErrorCode)
}
