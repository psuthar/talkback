package processing

import (
	"context"
	"log"
	"time"

	"github.com/psuthar/talkback/internal/database"
	"github.com/psuthar/talkback/internal/models"
)

// RunWorker claims and runs session_processing_jobs. Run in a goroutine; exits when ctx is done.
func RunWorker(ctx context.Context, db *database.DB, getZoomToken ZoomTokenFunc, pollInterval, lockDuration time.Duration) {
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
			runOne(ctx, db, job, getZoomToken)
		}
	}
}

func runOne(ctx context.Context, db *database.DB, job *models.SessionProcessingJob, getZoomToken ZoomTokenFunc) {
	defer func() {
		// Ensure unlock on panic
		_ = db.UnlockSessionProcessingJob(ctx, job.ID)
	}()
	_ = RunJob(ctx, db, job, getZoomToken)
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
