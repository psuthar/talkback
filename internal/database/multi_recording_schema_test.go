package database

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMultiRecordingSchemaMigration verifies SCRUM-403:
//   - the strict UNIQUE(session_id, source) on session_processing_jobs is gone,
//   - the expression-based UNIQUE(session_id, source, COALESCE(meeting_uuid,''),
//     COALESCE(instance_uuid,'')) takes its place,
//   - video_sources gains an external_recording_id column with a partial UNIQUE
//     on (session_id, provider, external_recording_id) WHERE external_recording_id
//     IS NOT NULL.
func TestMultiRecordingSchemaMigration(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)

	ctx := context.Background()
	session := createTestSession(t, db, "multi-rec schema session")

	t.Run("old strict unique index is gone", func(t *testing.T) {
		var found bool
		err := db.Pool.QueryRow(ctx, `
			SELECT EXISTS(
				SELECT 1 FROM pg_indexes
				WHERE schemaname = 'public'
				  AND indexname = 'idx_session_processing_jobs_session_source'
			)
		`).Scan(&found)
		require.NoError(t, err)
		assert.False(t, found, "old strict UNIQUE(session_id, source) must be dropped")
	})

	t.Run("new expression-based unique index exists", func(t *testing.T) {
		var found bool
		err := db.Pool.QueryRow(ctx, `
			SELECT EXISTS(
				SELECT 1 FROM pg_indexes
				WHERE schemaname = 'public'
				  AND indexname = 'idx_session_processing_jobs_session_source_meeting'
			)
		`).Scan(&found)
		require.NoError(t, err)
		assert.True(t, found, "new partial-null-safe UNIQUE must be present")
	})

	t.Run("session_processing_jobs blocks dup with NULL meeting/instance UUIDs", func(t *testing.T) {
		_, err := db.Pool.Exec(ctx, `
			INSERT INTO session_processing_jobs (session_id, source, meeting_uuid, instance_uuid)
			VALUES ($1, 'zoom', NULL, NULL)
		`, session.ID)
		require.NoError(t, err, "first insert with NULL UUIDs must succeed")

		_, err = db.Pool.Exec(ctx, `
			INSERT INTO session_processing_jobs (session_id, source, meeting_uuid, instance_uuid)
			VALUES ($1, 'zoom', NULL, NULL)
		`, session.ID)
		assert.Error(t, err, "duplicate insert with same NULL UUIDs must be rejected")
	})

	t.Run("session_processing_jobs allows multiple rows per (session,source) with distinct meeting_uuid", func(t *testing.T) {
		s := createTestSession(t, db, "multi-rec same-source distinct-uuid session")

		_, err := db.Pool.Exec(ctx, `
			INSERT INTO session_processing_jobs (session_id, source, meeting_uuid, instance_uuid)
			VALUES ($1, 'zoom', 'meeting-A', NULL)
		`, s.ID)
		require.NoError(t, err)

		_, err = db.Pool.Exec(ctx, `
			INSERT INTO session_processing_jobs (session_id, source, meeting_uuid, instance_uuid)
			VALUES ($1, 'zoom', 'meeting-B', NULL)
		`, s.ID)
		require.NoError(t, err, "different meeting_uuid for same (session,source) must be allowed")

		var n int
		err = db.Pool.QueryRow(ctx,
			`SELECT count(*) FROM session_processing_jobs WHERE session_id = $1 AND source = 'zoom'`,
			s.ID).Scan(&n)
		require.NoError(t, err)
		assert.Equal(t, 2, n, "both Zoom jobs for the session must persist")
	})

	t.Run("video_sources.external_recording_id column exists and defaults to NULL", func(t *testing.T) {
		artifactID := uuid.New()
		_, err := db.Pool.Exec(ctx, `
			INSERT INTO artifacts (id, session_id, title, status, created_at, updated_at)
			VALUES ($1, $2, 'multi-rec artifact', 'ready', now(), now())
		`, artifactID, session.ID)
		require.NoError(t, err)

		vsID := uuid.New()
		_, err = db.Pool.Exec(ctx, `
			INSERT INTO video_sources (id, artifact_id, session_id, provider, video_url, source_type)
			VALUES ($1, $2, $3, 'zoom', 'https://example.com/v1.mp4', 'upload')
		`, vsID, artifactID, session.ID)
		require.NoError(t, err)

		var ext *string
		err = db.Pool.QueryRow(ctx,
			`SELECT external_recording_id FROM video_sources WHERE id = $1`, vsID).Scan(&ext)
		require.NoError(t, err)
		assert.Nil(t, ext, "external_recording_id defaults to NULL")
	})

	t.Run("video_sources partial UNIQUE rejects duplicate external_recording_id per (session,provider)", func(t *testing.T) {
		s := createTestSession(t, db, "external-id dedupe session")

		artifactID := uuid.New()
		_, err := db.Pool.Exec(ctx, `
			INSERT INTO artifacts (id, session_id, title, status, created_at, updated_at)
			VALUES ($1, $2, 'a', 'ready', now(), now())
		`, artifactID, s.ID)
		require.NoError(t, err)

		_, err = db.Pool.Exec(ctx, `
			INSERT INTO video_sources (id, artifact_id, session_id, provider, video_url, source_type, external_recording_id)
			VALUES ($1, $2, $3, 'zoom', 'https://example.com/a.mp4', 'upload', 'zoom-meeting-uuid-1')
		`, uuid.New(), artifactID, s.ID)
		require.NoError(t, err, "first import with external_recording_id must succeed")

		_, err = db.Pool.Exec(ctx, `
			INSERT INTO video_sources (id, artifact_id, session_id, provider, video_url, source_type, external_recording_id)
			VALUES ($1, $2, $3, 'zoom', 'https://example.com/a-dup.mp4', 'upload', 'zoom-meeting-uuid-1')
		`, uuid.New(), artifactID, s.ID)
		assert.Error(t, err, "duplicate external_recording_id for same (session,zoom) must be rejected")
	})

	t.Run("video_sources partial UNIQUE permits same external_recording_id across different providers", func(t *testing.T) {
		s := createTestSession(t, db, "external-id provider-split session")

		artifactID := uuid.New()
		_, err := db.Pool.Exec(ctx, `
			INSERT INTO artifacts (id, session_id, title, status, created_at, updated_at)
			VALUES ($1, $2, 'a', 'ready', now(), now())
		`, artifactID, s.ID)
		require.NoError(t, err)

		_, err = db.Pool.Exec(ctx, `
			INSERT INTO video_sources (id, artifact_id, session_id, provider, video_url, source_type, external_recording_id)
			VALUES ($1, $2, $3, 'zoom', 'https://example.com/zoom.mp4', 'upload', 'shared-id')
		`, uuid.New(), artifactID, s.ID)
		require.NoError(t, err)

		_, err = db.Pool.Exec(ctx, `
			INSERT INTO video_sources (id, artifact_id, session_id, provider, video_url, source_type, external_recording_id)
			VALUES ($1, $2, $3, 'google_meet', 'https://example.com/meet.mp4', 'upload', 'shared-id')
		`, uuid.New(), artifactID, s.ID)
		assert.NoError(t, err, "same external_recording_id across different providers must be allowed")
	})

	t.Run("video_sources partial UNIQUE ignores NULL external_recording_id", func(t *testing.T) {
		s := createTestSession(t, db, "external-id null-allowed session")

		artifactID := uuid.New()
		_, err := db.Pool.Exec(ctx, `
			INSERT INTO artifacts (id, session_id, title, status, created_at, updated_at)
			VALUES ($1, $2, 'a', 'ready', now(), now())
		`, artifactID, s.ID)
		require.NoError(t, err)

		_, err = db.Pool.Exec(ctx, `
			INSERT INTO video_sources (id, artifact_id, session_id, provider, video_url, source_type, external_recording_id)
			VALUES ($1, $2, $3, 'other', 'https://example.com/upload1.mp4', 'upload', NULL)
		`, uuid.New(), artifactID, s.ID)
		require.NoError(t, err)

		_, err = db.Pool.Exec(ctx, `
			INSERT INTO video_sources (id, artifact_id, session_id, provider, video_url, source_type, external_recording_id)
			VALUES ($1, $2, $3, 'other', 'https://example.com/upload2.mp4', 'upload', NULL)
		`, uuid.New(), artifactID, s.ID)
		assert.NoError(t, err, "two uploads with NULL external_recording_id for same (session,provider) must be allowed")
	})
}
