// SCRUM-413: idempotent re-import dedupe for the three SessionImport*
// attach handlers (Zoom, Google Meet, Teams). Shared helper so all three
// handlers respond consistently when a user attaches the same recording
// twice (network retry, double-click, re-visit picker).
package handlers

import (
	"context"
	"net/http"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/database"
	"github.com/psuthar/talkback/internal/models"
)

// terminalReadyStates are the per-job states that mean "this recording is
// fully ingested and indexed; no work to do on re-attach". Treated as
// already_imported with status 200.
var terminalReadyStates = map[string]struct{}{
	models.ProcessingStateReady: {},
}

// terminalFailedStates are per-job states that mean "this recording's
// previous import gave up; a re-attach should re-queue the existing row
// rather than create a new one". Treated as retried with status 202.
var terminalFailedStates = map[string]struct{}{
	models.ProcessingStateFailedPermanent: {},
	models.ProcessingStateCanceled:        {},
}

// dedupeAttachResult is what dedupeExistingAttach returns to the caller.
type dedupeAttachResult struct {
	// Existing is non-nil when a job for the same (session, source,
	// meeting_uuid, instance_uuid) tuple already exists. The caller MUST
	// stop processing and return Response/Status to the client when this is
	// the case.
	Existing *models.SessionProcessingJob
	// Response is the body the caller should write when Existing != nil.
	Response SessionImportResponse
	// Status is the HTTP status the caller should write when Existing != nil:
	// 200 for AlreadyImported, 202 for Retried.
	Status int
}

// dedupeExistingAttach is the SCRUM-413 entry point: returns a non-nil
// Existing if the caller should skip Create-or-Get and return the
// dedupe response. Returns Existing == nil when the caller should proceed
// to create a new job.
//
// A failed-terminal existing row is re-queued in-place (via
// CreateOrGetSessionProcessingJob, which the SCRUM-408 upsert guard
// handles correctly) and reported as Retried=true.
func dedupeExistingAttach(
	ctx context.Context,
	db *database.DB,
	sessionID uuid.UUID,
	source string,
	meetingUUID, instanceUUID *string,
	creatorIdentity *string,
) (dedupeAttachResult, error) {
	existing, err := db.GetSessionProcessingJobByRecordingKey(ctx, sessionID, source, meetingUUID, instanceUUID)
	if err != nil {
		return dedupeAttachResult{}, err
	}
	if existing == nil {
		return dedupeAttachResult{}, nil
	}

	// Failed-terminal: re-queue the row in-place and report retried=true.
	if _, ok := terminalFailedStates[existing.State]; ok {
		retryJob := &models.SessionProcessingJob{
			ID:              existing.ID,
			SessionID:       sessionID,
			Source:          source,
			State:           models.ProcessingStateQueued,
			Stage:           models.ProcessingStageFetch,
			MeetingUUID:     meetingUUID,
			InstanceUUID:    instanceUUID,
			CreatorIdentity: creatorIdentity,
			// SCRUM-471: dedupeExistingAttach is only called from the
			// post-creation SessionImport* handlers — never promote.
			SetAsPrimary: false,
		}
		if err := db.CreateOrGetSessionProcessingJob(ctx, retryJob); err != nil {
			return dedupeAttachResult{}, err
		}
		_ = db.UpdateSessionProcessingMirror(ctx, sessionID, retryJob.State)
		return dedupeAttachResult{
			Existing: existing,
			Response: SessionImportResponse{
				JobID:   retryJob.ID.String(),
				State:   retryJob.State,
				Retried: true,
			},
			Status: http.StatusAccepted,
		}, nil
	}

	// Any other state (ready, queued, mid-stage, failed_transient, waiting)
	// → already_imported, no DB mutation. The caller's frontend can decide
	// whether to surface "already attached" vs "still processing" by
	// reading state.
	_, ready := terminalReadyStates[existing.State]
	resp := SessionImportResponse{
		JobID:           existing.ID.String(),
		State:           existing.State,
		AlreadyImported: true,
	}
	_ = ready // currently unused; reserved for future telemetry/log
	return dedupeAttachResult{
		Existing: existing,
		Response: resp,
		Status:   http.StatusOK,
	}, nil
}
