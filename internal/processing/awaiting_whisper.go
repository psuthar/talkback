// SCRUM-469: helper that transitions every awaiting_whisper job on a
// session to ready. Extracted from cmd/api/main.go's
// OnTranscriptCompleted callback so it can be unit-tested. The previous
// single-source lookup (by sessions.source_provider) missed
// multi-recording sessions where the awaiting job's source differed
// from the session's original.
package processing

import (
	"context"
	"log"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/database"
	"github.com/psuthar/talkback/internal/models"
)

// FlipAwaitingWhisperJobsToReady scans every session_processing_jobs
// row for the session; for each in state awaiting_whisper it transitions
// to ready, releases the worker lock, and returns the total flipped.
// Caller is responsible for the per-session mirror update + WebSocket
// broadcast (only if return value > 0).
//
// Errors from individual updates are logged but do not abort the loop —
// a partial flip is better than none.
func FlipAwaitingWhisperJobsToReady(ctx context.Context, db *database.DB, sessionID uuid.UUID) int {
	jobs, err := db.ListSessionProcessingJobsBySessionID(ctx, sessionID)
	if err != nil {
		log.Printf("FlipAwaitingWhisperJobsToReady: list jobs session=%s: %v", sessionID, err)
		return 0
	}
	flipped := 0
	for _, j := range jobs {
		if j == nil || j.State != models.ProcessingStateAwaitingWhisper {
			continue
		}
		if err := db.UpdateSessionProcessingJobState(ctx, j.ID, models.ProcessingStateReady, models.ProcessingStageReady, j.AttemptCount, nil, nil, nil); err != nil {
			log.Printf("FlipAwaitingWhisperJobsToReady: update state job=%s: %v", j.ID, err)
			continue
		}
		_ = db.UnlockSessionProcessingJob(ctx, j.ID)
		flipped++
		log.Printf("FlipAwaitingWhisperJobsToReady: marked ready session=%s job=%s source=%s", sessionID, j.ID, j.Source)
	}
	return flipped
}
