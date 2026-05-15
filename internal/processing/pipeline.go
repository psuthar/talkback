package processing

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/database"
	"github.com/psuthar/talkback/internal/models"
	"github.com/psuthar/talkback/internal/storage"
	"github.com/psuthar/talkback/internal/utils"
)

// zoomMaxVideoDurationSeconds is implemented in pipeline_zoom.go (SCRUM-409).

func maxUploadBytesVideo() int64 {
	v := os.Getenv("MAX_UPLOAD_BYTES_VIDEO")
	if v == "" {
		return 1024 * 1024 * 1024
	}
	n, _ := strconv.ParseInt(v, 10, 64)
	if n <= 0 {
		return 1024 * 1024 * 1024
	}
	return n
}

// ZoomTokenFunc returns a valid Zoom access token for the given creator identity (for background workers).
type ZoomTokenFunc func(ctx context.Context, creatorIdentity string) (string, error)

// TeamsTokenFunc returns a valid Microsoft Graph access token for the given creator identity.
type TeamsTokenFunc func(ctx context.Context, creatorIdentity string) (string, error)

// GoogleMeetTokenFunc returns a valid Google Meet access token for the given creator identity.
type GoogleMeetTokenFunc func(ctx context.Context, creatorIdentity string) (string, error)

// runGoogleMeetJob is implemented in pipeline_google_meet.go.

// RunJob runs the ingestion pipeline for one session_processing_job (fetch → download → parse → chunk → embed → ready).
// It dispatches to the appropriate provider pipeline based on job.Source.
// Idempotent: skips stages whose outputs already exist. Updates job state and session mirror.
// onJobReady is optional; when set, it is called when the job reaches ready so the API can broadcast to WebSocket clients.
func RunJob(ctx context.Context, db *database.DB, job *models.SessionProcessingJob, getZoomToken ZoomTokenFunc, getTeamsToken TeamsTokenFunc, getGoogleMeetToken GoogleMeetTokenFunc, store storage.Interface, storagePrefix string, jobProcessor *utils.JobProcessor, onJobReady OnJobReadyFunc) error {
	switch job.Source {
	case models.SessionProcessingJobSourceZoom:
		return runZoomJob(ctx, db, job, getZoomToken, store, storagePrefix, jobProcessor, onJobReady)
	case models.SessionProcessingJobSourceTeams:
		if getTeamsToken == nil {
			attempt := job.AttemptCount + 1
			setJobFailedPermanent(ctx, db, job.ID, attempt, "teams_not_configured", "Teams token resolver not configured")
			_ = db.UpdateSessionProcessingMirror(ctx, job.SessionID, models.ProcessingStateFailedPermanent)
			return nil
		}
		return runTeamsJob(ctx, db, job, getTeamsToken, store, storagePrefix, jobProcessor, onJobReady)
	case models.SessionProcessingJobSourceGoogleMeet:
		if getGoogleMeetToken == nil {
			attempt := job.AttemptCount + 1
			setJobFailedPermanent(ctx, db, job.ID, attempt, "google_meet_not_configured", "Google Meet token resolver not configured")
			_ = db.UpdateSessionProcessingMirror(ctx, job.SessionID, models.ProcessingStateFailedPermanent)
			return nil
		}
		return runGoogleMeetJob(ctx, db, job, getGoogleMeetToken, store, storagePrefix, jobProcessor, onJobReady)
	default:
		// Any job source not explicitly handled here is a misconfiguration. Fail
		// permanently so the worker never reclaims it and the job does not spin.
		attempt := job.AttemptCount + 1
		msg := fmt.Sprintf("unsupported job source: %s", job.Source)
		setJobFailedPermanent(ctx, db, job.ID, attempt, "unsupported_source", msg)
		_ = db.UpdateSessionProcessingMirror(ctx, job.SessionID, models.ProcessingStateFailedPermanent)
		return nil
	}
}


// mergeEtagIntoMetadata merges etag into existing metadata_json; returns new JSON bytes.
func mergeEtagIntoMetadata(existing []byte, etag string) []byte {
	m := make(map[string]string)
	if len(existing) > 0 {
		_ = json.Unmarshal(existing, &m)
	}
	if etag != "" {
		m["etag"] = etag
	}
	if len(m) == 0 {
		return nil
	}
	b, _ := json.Marshal(m)
	return b
}

func updateJobState(ctx context.Context, db *database.DB, jobID uuid.UUID, state, stage string, attempt int, nextRetryAt *time.Time, code, msg *string) {
	_ = db.UpdateSessionProcessingJobState(ctx, jobID, state, stage, attempt, nextRetryAt, code, msg)
}

// maxTransientAttempts is the number of failed_transient retries before escalating to failed_permanent.
const maxTransientAttempts = 5

// maxWaitingAttempts is the number of waiting retries before escalating to failed_permanent.
const maxWaitingAttempts = 5

func setJobFailedTransient(ctx context.Context, db *database.DB, jobID uuid.UUID, attempt int, code, msg string, nextRetryAt time.Time) {
	if attempt > maxTransientAttempts {
		escalatedMsg := fmt.Sprintf("permanent failure after %d retries: %s", maxTransientAttempts, msg)
		updateJobState(ctx, db, jobID, models.ProcessingStateFailedPermanent, "fetch", attempt, nil, &code, &escalatedMsg)
	} else {
		updateJobState(ctx, db, jobID, models.ProcessingStateFailedTransient, "fetch", attempt, &nextRetryAt, &code, &msg)
	}
	_ = db.UnlockSessionProcessingJob(ctx, jobID)
}

func setJobFailedPermanent(ctx context.Context, db *database.DB, jobID uuid.UUID, attempt int, code, msg string) {
	updateJobState(ctx, db, jobID, models.ProcessingStateFailedPermanent, "fetch", attempt, nil, &code, &msg)
	_ = db.UnlockSessionProcessingJob(ctx, jobID)
}

func setJobWaiting(ctx context.Context, db *database.DB, jobID uuid.UUID, attempt int, code, msg string, nextRetryAt time.Time) {
	if attempt > maxWaitingAttempts {
		escalatedMsg := fmt.Sprintf("permanent failure after %d retries: %s", maxWaitingAttempts, msg)
		updateJobState(ctx, db, jobID, models.ProcessingStateFailedPermanent, "download", attempt, nil, &code, &escalatedMsg)
	} else {
		updateJobState(ctx, db, jobID, models.ProcessingStateWaiting, "download", attempt, &nextRetryAt, &code, &msg)
	}
	_ = db.UnlockSessionProcessingJob(ctx, jobID)
}

