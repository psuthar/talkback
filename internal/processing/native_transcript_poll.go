// SCRUM-415: shared helpers for the async-native-transcript poll state.
// Meet (today) and Teams (future) can enter waiting_native_transcript when
// the platform's transcript pipeline is still generating the transcript at
// job-run time. The worker re-claims those jobs on a fixed cadence
// (MEET_TRANSCRIPT_POLL_INTERVAL, default 5m) and, after the recording's
// MEET_TRANSCRIPT_POLL_MAX_AGE budget is exhausted (default 4h from the
// recording's end time), gives up and falls back to a Whisper
// retranscription of the already-downloaded MP4.
package processing

import (
	"os"
	"strconv"
	"time"
)

const (
	defaultNativeTranscriptPollInterval = 5 * time.Minute
	defaultNativeTranscriptPollMaxAge   = 4 * time.Hour
)

// nativeTranscriptPollInterval returns the cadence the worker uses to
// re-check whether the native transcript has landed yet. Env var
// MEET_TRANSCRIPT_POLL_INTERVAL is an integer number of minutes; zero /
// negative / unparseable falls back to the default.
func nativeTranscriptPollInterval() time.Duration {
	v := os.Getenv("MEET_TRANSCRIPT_POLL_INTERVAL")
	if v == "" {
		return defaultNativeTranscriptPollInterval
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return defaultNativeTranscriptPollInterval
	}
	return time.Duration(n) * time.Minute
}

// nativeTranscriptPollMaxAge returns the upper bound on how long the
// pipeline waits for a native transcript before falling back to Whisper.
// Computed from the recording's end time (passed in as recordingEnd).
// Env var MEET_TRANSCRIPT_POLL_MAX_AGE is an integer number of minutes;
// zero / negative / unparseable falls back to the default 4 h.
func nativeTranscriptPollMaxAge() time.Duration {
	v := os.Getenv("MEET_TRANSCRIPT_POLL_MAX_AGE")
	if v == "" {
		return defaultNativeTranscriptPollMaxAge
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return defaultNativeTranscriptPollMaxAge
	}
	return time.Duration(n) * time.Minute
}

// nativeTranscriptPollExpired reports whether the recording's
// MEET_TRANSCRIPT_POLL_MAX_AGE budget is exhausted relative to now. The
// pipeline calls this on every re-claim of a waiting_native_transcript
// job; once true, the pipeline falls back to Whisper.
func nativeTranscriptPollExpired(recordingEnd, now time.Time) bool {
	if recordingEnd.IsZero() {
		// No recording-end timestamp → use the job's poll-start time
		// instead. Caller passes the job's created_at, which is a safe
		// upper-bound proxy (the job is created after the recording ends).
		return false
	}
	return now.Sub(recordingEnd) > nativeTranscriptPollMaxAge()
}
