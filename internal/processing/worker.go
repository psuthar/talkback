package processing

import (
	"context"
	"log"
	"time"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/database"
	"github.com/psuthar/talkback/internal/models"
	"github.com/psuthar/talkback/internal/storage"
	"github.com/psuthar/talkback/internal/utils"
)

// OnJobReadyFunc is called when a processing job reaches ready (e.g. Zoom import complete). Optional; used to broadcast over WebSocket.
type OnJobReadyFunc func(sessionID uuid.UUID)

// RunWorker claims and runs session_processing_jobs. Run in a goroutine; exits when ctx is done.
// store can be nil; when set (e.g. R2), Zoom import will upload MP4 to storage and set primary_video_artifact_id.
// storagePrefix is the R2 key prefix (e.g. "talkback") used when building artifact keys; only used when store != nil.
// jobProcessor is optional; when set, Zoom import can enqueue Whisper fallback when native transcript is not available (local storage only).
// onJobReady is optional; when set, it is called when a job reaches ready so the API can broadcast to WebSocket clients.
func RunWorker(ctx context.Context, db *database.DB, getZoomToken ZoomTokenFunc, store storage.Interface, storagePrefix string, pollInterval, lockDuration time.Duration, jobProcessor *utils.JobProcessor, onJobReady OnJobReadyFunc) {
	if pollInterval <= 0 {
		pollInterval = 15 * time.Second
	}
	if lockDuration <= 0 {
		lockDuration = 15 * time.Minute
	}
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			job, err := db.ClaimNextSessionProcessingJob(ctx, "worker", lockDuration, time.Now())
			if err != nil {
				log.Printf("processing worker: claim error: %v", err)
				continue
			}
			if job == nil {
				continue
			}
			runOne(ctx, db, job, getZoomToken, store, storagePrefix, jobProcessor, onJobReady)
		}
	}
}

func runOne(ctx context.Context, db *database.DB, job *models.SessionProcessingJob, getZoomToken ZoomTokenFunc, store storage.Interface, storagePrefix string, jobProcessor *utils.JobProcessor, onJobReady OnJobReadyFunc) {
	defer func() {
		// Ensure unlock on panic
		_ = db.UnlockSessionProcessingJob(ctx, job.ID)
	}()
	_ = RunJob(ctx, db, job, getZoomToken, store, storagePrefix, jobProcessor, onJobReady)
}

// RunReconciler resets next_retry_at for stuck or waiting jobs so the worker can pick them up. Run in a goroutine.
func RunReconciler(ctx context.Context, db *database.DB, interval, stuckThreshold time.Duration) {
	if interval <= 0 {
		interval = 20 * time.Minute
	}
	if stuckThreshold <= 0 {
		stuckThreshold = 20 * time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			threshold := time.Now().Add(-stuckThreshold)
			jobs, err := db.ListSessionProcessingJobsForReconciler(ctx, threshold)
			if err != nil {
				log.Printf("processing reconciler: list error: %v", err)
				continue
			}
			for _, j := range jobs {
				if err := db.ResetSessionProcessingJobRetry(ctx, j.ID); err != nil {
					log.Printf("processing reconciler: reset job %s: %v", j.ID, err)
					continue
				}
				_ = db.UpdateSessionProcessingMirror(ctx, j.SessionID, j.State)
				log.Printf("processing reconciler: reset next_retry_at for job %s (session %s)", j.ID, j.SessionID)
			}
		}
	}
}
