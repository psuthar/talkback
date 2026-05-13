package processing

import (
	"context"
	"testing"

	"github.com/psuthar/talkback/internal/googlemeet"
	"github.com/psuthar/talkback/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRunGoogleMeetJob_MissingIDsFailsPermanent verifies the early-exit branch:
// when the job has no conference_record / recording fields populated, runGoogleMeetJob
// fails permanent with code google_meet_missing_ids and does not call out to Google.
//
// End-to-end coverage (HTTP-stubbed conferenceRecords/recordings/transcripts/entries +
// Drive download + transcript parse + index) lives in the SCRUM-324 smoke test where
// the pipeline can be driven through a httptest harness from outside the package.
func TestRunGoogleMeetJob_MissingIDsFailsPermanent(t *testing.T) {
	t.Parallel()
	db, cleanup := setupProcessingTestDB(t)
	defer cleanup()

	// seedProcessingJob sets MeetingUUID + InstanceUUID = "x"; clear them to trigger
	// the missing-ids branch.
	_, job := seedProcessingJob(t, db, models.SessionProcessingJobSourceGoogleMeet)
	job.MeetingUUID = nil
	job.InstanceUUID = nil

	getToken := func(ctx context.Context, creatorIdentity string) (string, error) {
		return "test-access-token", nil
	}
	err := runGoogleMeetJob(context.Background(), db, job, getToken, nil, "", nil, nil)
	require.NoError(t, err)

	updated, dbErr := db.GetSessionProcessingJobByID(context.Background(), job.ID)
	require.NoError(t, dbErr)
	require.NotNil(t, updated)
	assert.Equal(t, models.ProcessingStateFailedPermanent, updated.State)
	require.NotNil(t, updated.LastErrorCode)
	assert.Equal(t, "google_meet_missing_ids", *updated.LastErrorCode)
}

// TestShouldEnqueueMeetWhisperFallback covers the four branches of the
// fallback decision (SCRUM-380):
//  1. transcripts empty + attempt below threshold → keep waiting on Meet
//  2. transcripts empty + attempt at/above threshold → fall back to Whisper
//  3. all transcripts in a terminal failure state → fall back immediately
//  4. any transcript still in a non-terminal state → keep waiting
//
// This is the contract that decides whether ingest converts to Whisper or
// keeps polling Meet, so it gets explicit coverage independent of the DB-
// dependent enqueue helper.
func TestShouldEnqueueMeetWhisperFallback(t *testing.T) {
	t.Parallel()

	t.Run("empty list below threshold waits on Meet", func(t *testing.T) {
		assert.False(t, shouldEnqueueMeetWhisperFallback(nil, 1))
		assert.False(t, shouldEnqueueMeetWhisperFallback([]googlemeet.Transcript{}, meetTranscriptWaitAttempts-1))
	})

	t.Run("empty list at threshold falls back to Whisper", func(t *testing.T) {
		assert.True(t, shouldEnqueueMeetWhisperFallback(nil, meetTranscriptWaitAttempts))
		assert.True(t, shouldEnqueueMeetWhisperFallback(nil, meetTranscriptWaitAttempts+5))
	})

	t.Run("all transcripts terminal falls back regardless of attempt", func(t *testing.T) {
		all := []googlemeet.Transcript{{State: "FAILED"}, {State: "ERROR"}}
		assert.True(t, shouldEnqueueMeetWhisperFallback(all, 1))
		assert.True(t, shouldEnqueueMeetWhisperFallback(all, meetTranscriptWaitAttempts))
	})

	t.Run("any non-terminal transcript keeps waiting", func(t *testing.T) {
		// Meet is actively preparing a transcript (STARTED/ENDED). Even
		// past the wait threshold, do not pre-empt with Whisper — the
		// Meet-native transcript is preferred when one is on the way.
		mixed := []googlemeet.Transcript{{State: "FAILED"}, {State: "STARTED"}}
		assert.False(t, shouldEnqueueMeetWhisperFallback(mixed, 1))
		assert.False(t, shouldEnqueueMeetWhisperFallback(mixed, meetTranscriptWaitAttempts+5))

		stillProcessing := []googlemeet.Transcript{{State: "STARTED"}}
		assert.False(t, shouldEnqueueMeetWhisperFallback(stillProcessing, meetTranscriptWaitAttempts+10))
	})
}

// TestRunGoogleMeetJob_NilCreatorIdentityFailsPermanent covers the auth-precondition
// branch: even with a non-nil token resolver, a job missing creator_identity must
// fail permanent with code google_meet_auth.
func TestRunGoogleMeetJob_NilCreatorIdentityFailsPermanent(t *testing.T) {
	t.Parallel()
	db, cleanup := setupProcessingTestDB(t)
	defer cleanup()

	_, job := seedProcessingJob(t, db, models.SessionProcessingJobSourceGoogleMeet)
	job.CreatorIdentity = nil

	getToken := func(ctx context.Context, creatorIdentity string) (string, error) {
		return "test-access-token", nil
	}
	err := runGoogleMeetJob(context.Background(), db, job, getToken, nil, "", nil, nil)
	require.NoError(t, err)

	updated, dbErr := db.GetSessionProcessingJobByID(context.Background(), job.ID)
	require.NoError(t, dbErr)
	require.NotNil(t, updated)
	assert.Equal(t, models.ProcessingStateFailedPermanent, updated.State)
	require.NotNil(t, updated.LastErrorCode)
	assert.Equal(t, "google_meet_auth", *updated.LastErrorCode)
}
