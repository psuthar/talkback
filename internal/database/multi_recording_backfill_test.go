package database

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMultiRecordingBackfill exercises the SCRUM-410 migration's data
// transformations:
//   - every video_sources row with NULL external_recording_id is populated
//     with a non-null value via the provider-aware precedence,
//   - sessions with a stale primary_video_artifact_id pointer get the
//     corresponding video_sources.video_role flipped to 'primary',
//   - single-recording sessions with neither pointer set get their lone row
//     marked primary.
//
// The migration ran once at TestMain time; the test simulates a fresh
// production-shaped backfill by inserting rows in the post-migration state
// (NULL external_recording_id / video_role) and then re-applies the
// migration's UPDATE statements idempotently.
func TestMultiRecordingBackfill(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)

	ctx := context.Background()

	// Helper: insert a video_sources row directly via SQL so we can simulate
	// legacy NULL external_recording_id + NULL video_role.
	insertLegacyVideoSource := func(t *testing.T, sessionID, artifactID uuid.UUID, provider, key string, fileArtifactID *uuid.UUID) uuid.UUID {
		t.Helper()
		vsID := uuid.New()
		_, err := db.Pool.Exec(ctx, `
			INSERT INTO video_sources (id, artifact_id, session_id, provider, video_url, source_type, file_artifact_id)
			VALUES ($1, $2, $3, $4, $5, 'upload', $6)
		`, vsID, artifactID, sessionID, provider, fmt.Sprintf("https://example.com/%s.mp4", key), fileArtifactID)
		require.NoError(t, err)
		return vsID
	}

	insertArtifact := func(t *testing.T, sessionID uuid.UUID) uuid.UUID {
		t.Helper()
		id := uuid.New()
		_, err := db.Pool.Exec(ctx, `
			INSERT INTO artifacts (id, session_id, title, status, created_at, updated_at)
			VALUES ($1, $2, 'a', 'ready', now(), now())
		`, id, sessionID)
		require.NoError(t, err)
		return id
	}

	insertFileArtifact := func(t *testing.T, sessionID uuid.UUID, key string) uuid.UUID {
		t.Helper()
		id := uuid.New()
		_, err := db.Pool.Exec(ctx, `
			INSERT INTO file_artifacts (id, session_id, kind, content_type,
			                            storage_provider, storage_bucket, storage_key,
			                            status, created_at, updated_at)
			VALUES ($1, $2, 'video', 'video/mp4', 'r2', 'bucket', $3, 'ready', now(), now())
		`, id, sessionID, key)
		require.NoError(t, err)
		return id
	}

	insertJob := func(t *testing.T, sessionID uuid.UUID, source, meetingUUID, instanceUUID string) {
		t.Helper()
		var meeting, instance interface{}
		if meetingUUID != "" {
			meeting = meetingUUID
		}
		if instanceUUID != "" {
			instance = instanceUUID
		}
		_, err := db.Pool.Exec(ctx, `
			INSERT INTO session_processing_jobs (session_id, source, meeting_uuid, instance_uuid)
			VALUES ($1, $2, $3, $4)
		`, sessionID, source, meeting, instance)
		require.NoError(t, err)
	}

	// runBackfill is the exact SQL block from migration 000052 — re-applying it
	// here proves the backfill is idempotent and exercises the migration's
	// behaviour against richer fixtures than the migration runner alone
	// produces.
	runBackfill := func(t *testing.T) {
		t.Helper()
		_, err := db.Pool.Exec(ctx, `
			UPDATE video_sources vs
			SET external_recording_id = COALESCE(
				(SELECT NULLIF(j.meeting_uuid, '') FROM session_processing_jobs j
				 WHERE j.session_id = vs.session_id AND j.source = vs.provider
				   AND j.meeting_uuid IS NOT NULL
				 ORDER BY j.created_at ASC LIMIT 1),
				(SELECT NULLIF(j.instance_uuid, '') FROM session_processing_jobs j
				 WHERE j.session_id = vs.session_id AND j.source = vs.provider
				   AND vs.provider = 'teams' AND j.instance_uuid IS NOT NULL
				 ORDER BY j.created_at ASC LIMIT 1),
				CASE WHEN vs.provider = 'other' AND vs.file_artifact_id IS NOT NULL
				     THEN vs.file_artifact_id::text END,
				vs.session_id::text || ':' || vs.id::text
			)
			WHERE external_recording_id IS NULL;`)
		require.NoError(t, err)

		_, err = db.Pool.Exec(ctx, `
			UPDATE video_sources vs
			SET video_role = 'primary'
			FROM sessions s
			WHERE s.id = vs.session_id
			  AND s.primary_video_artifact_id IS NOT NULL
			  AND vs.file_artifact_id IS NOT NULL
			  AND vs.file_artifact_id = s.primary_video_artifact_id
			  AND NOT EXISTS (
				SELECT 1 FROM video_sources existing_primary
				WHERE existing_primary.session_id = vs.session_id
				  AND existing_primary.video_role = 'primary');`)
		require.NoError(t, err)

		_, err = db.Pool.Exec(ctx, `
			WITH single_recording_sessions AS (
				SELECT vs.session_id, (array_agg(vs.id))[1] AS only_video_id
				FROM video_sources vs
				LEFT JOIN sessions s ON s.id = vs.session_id
				GROUP BY vs.session_id
				HAVING COUNT(*) = 1
				   AND NOT EXISTS (
					   SELECT 1 FROM video_sources existing_primary
					   WHERE existing_primary.session_id = vs.session_id
						 AND existing_primary.video_role = 'primary')
				   AND BOOL_AND(s.primary_video_artifact_id IS NULL)
			)
			UPDATE video_sources vs
			SET video_role = 'primary'
			FROM single_recording_sessions srs
			WHERE vs.id = srs.only_video_id;`)
		require.NoError(t, err)
	}

	// --- Fixture: four sessions covering every backfill branch.
	sZoom := createTestSession(t, db, "zoom-recording session")
	sMeet := createTestSession(t, db, "meet-recording session")
	sTeams := createTestSession(t, db, "teams-recording session")
	sUpload := createTestSession(t, db, "upload-recording session")
	sNoJob := createTestSession(t, db, "fallback-only session")

	aZoom := insertArtifact(t, sZoom.ID)
	insertJob(t, sZoom.ID, "zoom", "zoom-meeting-uuid-001", "")
	vsZoom := insertLegacyVideoSource(t, sZoom.ID, aZoom, "zoom", "zoom", nil)

	aMeet := insertArtifact(t, sMeet.ID)
	insertJob(t, sMeet.ID, "google_meet", "meet-recording/abc", "")
	vsMeet := insertLegacyVideoSource(t, sMeet.ID, aMeet, "google_meet", "meet", nil)

	aTeams := insertArtifact(t, sTeams.ID)
	insertJob(t, sTeams.ID, "teams", "", "teams-recording-001")
	vsTeams := insertLegacyVideoSource(t, sTeams.ID, aTeams, "teams", "teams", nil)

	aUpload := insertArtifact(t, sUpload.ID)
	faUpload := insertFileArtifact(t, sUpload.ID, "sessions/"+sUpload.ID.String()+"/uploads/up.mp4")
	vsUpload := insertLegacyVideoSource(t, sUpload.ID, aUpload, "other", "upload", &faUpload)

	aFallback := insertArtifact(t, sNoJob.ID)
	vsFallback := insertLegacyVideoSource(t, sNoJob.ID, aFallback, "google_meet", "fallback", nil)

	// Primary pointer reconciliation fixture: sessions.primary_video_artifact_id
	// set on the Upload session pointing to its file_artifact, no video_role
	// flipped yet.
	_, err := db.Pool.Exec(ctx, `UPDATE sessions SET primary_video_artifact_id = $1 WHERE id = $2`, faUpload, sUpload.ID)
	require.NoError(t, err)

	// --- Act: run the backfill twice to prove idempotency.
	runBackfill(t)
	runBackfill(t)

	t.Run("zoom row gets meeting_uuid from session_processing_jobs", func(t *testing.T) {
		var got *string
		require.NoError(t, db.Pool.QueryRow(ctx, `SELECT external_recording_id FROM video_sources WHERE id = $1`, vsZoom).Scan(&got))
		require.NotNil(t, got)
		assert.Equal(t, "zoom-meeting-uuid-001", *got)
	})

	t.Run("meet row gets meeting_uuid", func(t *testing.T) {
		var got *string
		require.NoError(t, db.Pool.QueryRow(ctx, `SELECT external_recording_id FROM video_sources WHERE id = $1`, vsMeet).Scan(&got))
		require.NotNil(t, got)
		assert.Equal(t, "meet-recording/abc", *got)
	})

	t.Run("teams row falls back to instance_uuid", func(t *testing.T) {
		var got *string
		require.NoError(t, db.Pool.QueryRow(ctx, `SELECT external_recording_id FROM video_sources WHERE id = $1`, vsTeams).Scan(&got))
		require.NotNil(t, got)
		assert.Equal(t, "teams-recording-001", *got)
	})

	t.Run("uploaded MP4 row uses file_artifact_id", func(t *testing.T) {
		var got *string
		require.NoError(t, db.Pool.QueryRow(ctx, `SELECT external_recording_id FROM video_sources WHERE id = $1`, vsUpload).Scan(&got))
		require.NotNil(t, got)
		assert.Equal(t, faUpload.String(), *got)
	})

	t.Run("fallback row uses synthetic <session_id>:<vs_id>", func(t *testing.T) {
		var got *string
		require.NoError(t, db.Pool.QueryRow(ctx, `SELECT external_recording_id FROM video_sources WHERE id = $1`, vsFallback).Scan(&got))
		require.NotNil(t, got)
		assert.Equal(t, sNoJob.ID.String()+":"+vsFallback.String(), *got)
	})

	t.Run("no video_sources row has NULL external_recording_id after backfill", func(t *testing.T) {
		var n int
		require.NoError(t, db.Pool.QueryRow(ctx, `SELECT count(*) FROM video_sources WHERE external_recording_id IS NULL`).Scan(&n))
		assert.Equal(t, 0, n)
	})

	t.Run("upload session's video_role is now primary (reconciled from primary_video_artifact_id)", func(t *testing.T) {
		var role *string
		require.NoError(t, db.Pool.QueryRow(ctx, `SELECT video_role FROM video_sources WHERE id = $1`, vsUpload).Scan(&role))
		require.NotNil(t, role)
		assert.Equal(t, "primary", *role)
	})

	t.Run("single-recording session with no primary pointer gets video_role=primary", func(t *testing.T) {
		// sNoJob has exactly one video_sources row + no primary_video_artifact_id.
		var role *string
		require.NoError(t, db.Pool.QueryRow(ctx, `SELECT video_role FROM video_sources WHERE id = $1`, vsFallback).Scan(&role))
		require.NotNil(t, role)
		assert.Equal(t, "primary", *role)
	})

	t.Run("idempotency: running the backfill again leaves rows unchanged", func(t *testing.T) {
		var beforeExt, afterExt *string
		require.NoError(t, db.Pool.QueryRow(ctx, `SELECT external_recording_id FROM video_sources WHERE id = $1`, vsZoom).Scan(&beforeExt))
		runBackfill(t)
		require.NoError(t, db.Pool.QueryRow(ctx, `SELECT external_recording_id FROM video_sources WHERE id = $1`, vsZoom).Scan(&afterExt))
		require.NotNil(t, beforeExt)
		require.NotNil(t, afterExt)
		assert.Equal(t, *beforeExt, *afterExt)
	})

	t.Run("partial-unique-index: backfilled external_recording_id values don't collide per (session, provider)", func(t *testing.T) {
		// Insert a second video_sources for the same session+provider with NULL
		// external_recording_id, run the backfill — the synthetic includes the
		// row's unique id so it must NOT collide.
		aSecond := insertArtifact(t, sNoJob.ID)
		vsSecond := insertLegacyVideoSource(t, sNoJob.ID, aSecond, "google_meet", "fallback2", nil)
		runBackfill(t)
		var got *string
		require.NoError(t, db.Pool.QueryRow(ctx, `SELECT external_recording_id FROM video_sources WHERE id = $1`, vsSecond).Scan(&got))
		require.NotNil(t, got)
		assert.Equal(t, sNoJob.ID.String()+":"+vsSecond.String(), *got)
	})
}
