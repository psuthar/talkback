// SCRUM-467: per-recording dedupe + "skip MP4 ingest" decision for the
// three platform pipelines. Replaces the single-recording-era check
// (`session.PrimaryVideoArtifactID != nil`) that incorrectly suppressed
// every additional recording's MP4 ingest after the first one landed.
//
// The correct dedupe is per-(meeting_uuid, instance_uuid). The handler-
// layer SCRUM-413 dedupe catches duplicate session_processing_jobs at
// import time; this worker-layer helper catches the case where a video
// from a different source path (direct upload, or a job that finished
// while a retry is pending) already exists on this session.
package processing

import (
	"context"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/database"
	"github.com/psuthar/talkback/internal/models"
)

// shouldSkipMP4Ingest returns true when a video_source already exists on
// this session matching THIS specific recording (provider + meeting_uuid
// + instance_uuid composite). Falls back to false if the meeting/
// instance UUIDs are nil (legacy single-recording sessions, or platforms
// that lack stable IDs — the upload then proceeds idempotently into a
// fresh artifact).
//
// Errors from the lookup are non-fatal: we fail OPEN (proceed with
// ingest) so a transient DB blip never causes silent data loss.
//
// NOTE: as of SCRUM-467, no pipeline writes external_recording_id when
// creating a video_source — that column was added in SCRUM-403 but the
// model+INSERT was not updated. So today this function returns false
// for every call (no match possible). The bug it actually fixes is
// removing the BROKEN session-level skip that was always-true and
// suppressed every additional recording's ingest. Once the
// external_recording_id population is done (tracked separately), this
// function will start hitting the intended fast-path on duplicate
// re-runs. The SCRUM-413 handler-layer dedupe currently catches
// duplicate import attempts before the worker runs at all.
func shouldSkipMP4Ingest(ctx context.Context, db *database.DB, sessionID uuid.UUID, provider string, meetingUUID, instanceUUID *string) bool {
	if meetingUUID == nil && instanceUUID == nil {
		return false
	}
	externalID := ""
	if instanceUUID != nil && *instanceUUID != "" {
		externalID = *instanceUUID
	} else if meetingUUID != nil && *meetingUUID != "" {
		externalID = *meetingUUID
	}
	if externalID == "" {
		return false
	}
	existing, err := db.GetVideoSourceByExternalRecordingID(ctx, sessionID, provider, externalID)
	if err != nil || existing == nil {
		return false
	}
	// Only skip when the matching video_source has a ready file_artifact
	// — same predicate the buggy code intended, but scoped to this
	// recording's row instead of the session's primary.
	if existing.FileArtifactID == nil {
		return false
	}
	fa, err := db.GetFileArtifactByID(ctx, *existing.FileArtifactID)
	if err != nil || fa == nil {
		return false
	}
	return fa.Status == models.FileArtifactStatusReady
}

// setPrimaryIfNotSet calls SetSessionPrimaryVideoArtifact only when:
//   - shouldPromote is true (the job's set_as_primary flag — SCRUM-471), AND
//   - the session does NOT already have a ready primary.
//
// shouldPromote distinguishes "session created WITH this video" (legacy
// CreateSession*FromZoom paths set true) from post-creation imports via the
// SCRUM-411 SessionImport* attach endpoints (set false — recording lands
// as secondary; user picks primary explicitly via SCRUM-426
// RecordingsSection kebab).
func setPrimaryIfNotSet(ctx context.Context, db *database.DB, sessionID, artifactID uuid.UUID, shouldPromote bool) error {
	if !shouldPromote {
		return nil
	}
	sess, err := db.GetSession(ctx, sessionID)
	if err == nil && sess != nil && sess.PrimaryVideoArtifactID != nil {
		fa, _ := db.GetFileArtifactByID(ctx, *sess.PrimaryVideoArtifactID)
		if fa != nil && fa.Status == models.FileArtifactStatusReady {
			return nil // existing primary stays
		}
	}
	return db.SetSessionPrimaryVideoArtifact(ctx, sessionID, &artifactID)
}
