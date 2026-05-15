// SCRUM-414: per-session recording cap support.
package database

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

// CountActiveRecordingsForSession returns how many video_sources rows the
// session has whose corresponding session_processing_jobs row is in a
// non-failed state. Counts BOTH ingestion in-progress and ready/completed
// recordings — anything the session is committed to retaining. Cleanly-
// failed (failed_permanent) and canceled rows are excluded so a user whose
// first import failed isn't blocked from retrying.
//
// SCRUM-414 cap check uses this value: when it is >= MAX_RECORDINGS_PER_SESSION
// the attach handlers return 429.
func (db *DB) CountActiveRecordingsForSession(ctx context.Context, sessionID uuid.UUID) (int, error) {
	var n int
	// Use NOT EXISTS to exclude recordings whose matching job (joined on
	// session+source+external_recording_id) is in a failed-terminal state.
	// LEFT JOIN would multiply rows when multiple jobs share the same
	// (session, source) tuple, producing an inflated count.
	err := db.Pool.QueryRow(ctx, `
		SELECT count(*)
		FROM video_sources vs
		WHERE vs.session_id = $1
		  AND NOT EXISTS (
		    SELECT 1 FROM session_processing_jobs j
		    WHERE j.session_id = vs.session_id
		      AND j.source = vs.provider
		      AND vs.external_recording_id IS NOT NULL
		      AND (j.meeting_uuid = vs.external_recording_id OR j.instance_uuid = vs.external_recording_id)
		      AND j.state IN ('failed_permanent','canceled')
		  )
	`, sessionID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count active recordings: %w", err)
	}
	return n, nil
}
