// SCRUM-467: tests for the per-recording dedupe + first-primary helpers.
package processing

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/database"
	"github.com/psuthar/talkback/internal/models"
	"github.com/psuthar/talkback/internal/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestShouldSkipMP4Ingest_PerRecordingPredicate documents that the fix
// is per-recording, not session-level. With NO video_source at all, the
// answer is false. With a video_source whose external_recording_id
// matches, AND a ready file_artifact, the answer is true.
func TestShouldSkipMP4Ingest_PerRecordingPredicate(t *testing.T) {
	// No t.Parallel(): the shared test DB has its own concurrency
	// constraints, and the assertions here scan all rows for the
	// session, which means a concurrent test seeding into the same
	// session would corrupt the predicate.
	db, cleanup := setupTestDBForProcessing(t)
	defer cleanup()
	ctx := context.Background()

	session := createTestSession(t, db, "skip-mp4-test")

	t.Run("no video_source → no skip (proceed with ingest)", func(t *testing.T) {
		meetingUUID := "MSPB_xyz"
		recordingID := "rec-1"
		got := shouldSkipMP4Ingest(ctx, db, session.ID, "teams", &meetingUUID, &recordingID)
		assert.False(t, got, "must not skip when there's nothing to skip against — this is the SCRUM-467 fix")
	})

	t.Run("nil meeting/instance UUIDs → no skip", func(t *testing.T) {
		got := shouldSkipMP4Ingest(ctx, db, session.ID, "teams", nil, nil)
		assert.False(t, got, "no key → no dedupe match → ingest must run")
	})

	t.Run("empty-string meeting/instance UUIDs → no skip", func(t *testing.T) {
		empty := ""
		got := shouldSkipMP4Ingest(ctx, db, session.ID, "teams", &empty, &empty)
		assert.False(t, got, "empty string is treated the same as nil")
	})

	t.Run("video_source with matching external_recording_id + ready file_artifact → skip", func(t *testing.T) {
		externalID := "rec-already-ingested"
		artifactID := uuid.New()
		// Seed a ready file_artifact (storage_key must be unique).
		_, err := db.Pool.Exec(ctx, `
			INSERT INTO file_artifacts (id, session_id, kind, content_type, storage_provider, storage_bucket, storage_key, status, created_at, updated_at)
			VALUES ($1, $2, 'video', 'video/mp4', 'r2', 'b', $3, 'ready', now(), now())
		`, artifactID, session.ID, "k-"+artifactID.String())
		require.NoError(t, err)
		// Insert artifacts row too (legacy table; required by FK).
		legacyArtifactID := uuid.New()
		_, err = db.Pool.Exec(ctx, `
			INSERT INTO artifacts (id, session_id, title, status, created_at, updated_at)
			VALUES ($1, $2, 't', 'ready', now(), now())
		`, legacyArtifactID, session.ID)
		require.NoError(t, err)
		// Seed a video_source with external_recording_id set.
		vsID := uuid.New()
		_, err = db.Pool.Exec(ctx, `
			INSERT INTO video_sources (id, artifact_id, session_id, provider, video_url, source_type, file_artifact_id, external_recording_id)
			VALUES ($1, $2, $3, 'teams', $4, 'upload', $5, $6)
		`, vsID, legacyArtifactID, session.ID, "https://example.com/v.mp4", artifactID, externalID)
		require.NoError(t, err)

		got := shouldSkipMP4Ingest(ctx, db, session.ID, "teams", &externalID, &externalID)
		assert.True(t, got, "match on external_recording_id + ready file_artifact must short-circuit ingest")
	})

	t.Run("match on different provider → no skip", func(t *testing.T) {
		externalID := "rec-already-ingested"
		got := shouldSkipMP4Ingest(ctx, db, session.ID, "zoom", &externalID, &externalID)
		assert.False(t, got, "provider mismatch must NOT short-circuit — same external_id across platforms is a coincidence, not a duplicate")
	})
}

// TestSetPrimaryIfNotSet_PreservesExistingPrimary pins the SCRUM-467
// fix to the second half of the bug: the worker should NOT silently
// overwrite the session's primary when a 2nd recording lands. First
// ingest gets primary; the user picks subsequent primaries explicitly.
func TestSetPrimaryIfNotSet_PreservesExistingPrimary(t *testing.T) {
	// No t.Parallel(): the shared test DB has its own concurrency
	// constraints, and the assertions here scan all rows for the
	// session, which means a concurrent test seeding into the same
	// session would corrupt the predicate.
	db, cleanup := setupTestDBForProcessing(t)
	defer cleanup()
	ctx := context.Background()

	session := createTestSession(t, db, "primary-preserve-test")

	insertReadyArtifact := func() uuid.UUID {
		id := uuid.New()
		_, err := db.Pool.Exec(ctx, `
			INSERT INTO file_artifacts (id, session_id, kind, content_type, storage_provider, storage_bucket, storage_key, status, created_at, updated_at)
			VALUES ($1, $2, 'video', 'video/mp4', 'r2', 'b', $3, 'ready', now(), now())
		`, id, session.ID, "k-"+id.String())
		require.NoError(t, err)
		return id
	}
	a1 := insertReadyArtifact()
	a2 := insertReadyArtifact()

	// First call (shouldPromote=true): no primary yet → a1 becomes primary.
	require.NoError(t, setPrimaryIfNotSet(ctx, db, session.ID, a1, true))
	sess, err := db.GetSession(ctx, session.ID)
	require.NoError(t, err)
	require.NotNil(t, sess.PrimaryVideoArtifactID)
	assert.Equal(t, a1, *sess.PrimaryVideoArtifactID)

	// Second call: primary exists + ready → unchanged.
	require.NoError(t, setPrimaryIfNotSet(ctx, db, session.ID, a2, true))
	sess, err = db.GetSession(ctx, session.ID)
	require.NoError(t, err)
	require.NotNil(t, sess.PrimaryVideoArtifactID)
	assert.Equal(t, a1, *sess.PrimaryVideoArtifactID, "existing primary must not be overwritten by SCRUM-467")
}

// TestSetPrimaryIfNotSet_ShouldPromoteFalseSkipsPromotion pins the
// SCRUM-471 opt-in behavior: when the job's set_as_primary flag is false,
// the helper must NOT promote even when the session has no primary yet.
func TestSetPrimaryIfNotSet_ShouldPromoteFalseSkipsPromotion(t *testing.T) {
	db, cleanup := setupTestDBForProcessing(t)
	defer cleanup()
	ctx := context.Background()

	session := createTestSession(t, db, "scrum471-no-promote")

	artifactID := uuid.New()
	_, err := db.Pool.Exec(ctx, `
		INSERT INTO file_artifacts (id, session_id, kind, content_type, storage_provider, storage_bucket, storage_key, status, created_at, updated_at)
		VALUES ($1, $2, 'video', 'video/mp4', 'r2', 'b', $3, 'ready', now(), now())
	`, artifactID, session.ID, "k-"+artifactID.String())
	require.NoError(t, err)

	// shouldPromote=false → no primary should be set even with empty session.
	require.NoError(t, setPrimaryIfNotSet(ctx, db, session.ID, artifactID, false))
	sess, err := db.GetSession(ctx, session.ID)
	require.NoError(t, err)
	assert.Nil(t, sess.PrimaryVideoArtifactID, "shouldPromote=false must NOT set primary (SCRUM-471)")
}

// --- test helpers ---

func setupTestDBForProcessing(t *testing.T) (*database.DB, func()) {
	t.Helper()
	db, err := database.New()
	require.NoError(t, err, "DATABASE_URL must be set for processing tests")
	cleanup := func() {
		test.TruncateTables(t, db.Pool)
		db.Close()
	}
	return db, cleanup
}

func createTestSession(t *testing.T, db *database.DB, title string) *models.Session {
	t.Helper()
	sess := &models.Session{
		ID:     uuid.New(),
		Title:  title + "-" + uuid.NewString(),
		Status: models.SessionStatusOpen,
	}
	require.NoError(t, db.CreateSession(context.Background(), sess))
	return sess
}
