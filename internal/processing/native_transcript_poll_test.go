package processing

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestNativeTranscriptPollInterval pins the SCRUM-415 env-parse semantics
// for MEET_TRANSCRIPT_POLL_INTERVAL.
func TestNativeTranscriptPollInterval(t *testing.T) {
	cases := []struct {
		name string
		env  string
		want time.Duration
	}{
		{"unset returns 5m default", "", defaultNativeTranscriptPollInterval},
		{"unparseable returns default", "bogus", defaultNativeTranscriptPollInterval},
		{"zero returns default", "0", defaultNativeTranscriptPollInterval},
		{"negative returns default", "-5", defaultNativeTranscriptPollInterval},
		{"explicit positive minutes honored", "10", 10 * time.Minute},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("MEET_TRANSCRIPT_POLL_INTERVAL", tc.env)
			assert.Equal(t, tc.want, nativeTranscriptPollInterval())
		})
	}
}

// TestNativeTranscriptPollMaxAge pins env-parse for MEET_TRANSCRIPT_POLL_MAX_AGE.
func TestNativeTranscriptPollMaxAge(t *testing.T) {
	cases := []struct {
		name string
		env  string
		want time.Duration
	}{
		{"unset returns 4h default", "", defaultNativeTranscriptPollMaxAge},
		{"unparseable returns default", "bogus", defaultNativeTranscriptPollMaxAge},
		{"zero returns default", "0", defaultNativeTranscriptPollMaxAge},
		{"explicit positive minutes honored", "60", 60 * time.Minute},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("MEET_TRANSCRIPT_POLL_MAX_AGE", tc.env)
			assert.Equal(t, tc.want, nativeTranscriptPollMaxAge())
		})
	}
}

// TestNativeTranscriptPollExpired pins the four-case state machine the
// SCRUM-415 ticket calls out:
//   - sync ready: covered by the existing Meet pipeline (no expiry check).
//   - async eventually ready: poll has not expired → return false.
//   - async timeout: poll expired → return true.
//   - native-after-whisper race: handled by the Whisper-already-running
//     branch in the pipeline, not by this helper.
func TestNativeTranscriptPollExpired(t *testing.T) {
	now := time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)

	t.Run("zero recording end → never expired (use job's created_at upstream)", func(t *testing.T) {
		assert.False(t, nativeTranscriptPollExpired(time.Time{}, now))
	})

	t.Run("recording ended 1h ago → not expired (under default 4h max)", func(t *testing.T) {
		end := now.Add(-1 * time.Hour)
		assert.False(t, nativeTranscriptPollExpired(end, now))
	})

	t.Run("recording ended 5h ago → expired (over default 4h max)", func(t *testing.T) {
		end := now.Add(-5 * time.Hour)
		assert.True(t, nativeTranscriptPollExpired(end, now))
	})

	t.Run("custom MEET_TRANSCRIPT_POLL_MAX_AGE shortens the budget", func(t *testing.T) {
		t.Setenv("MEET_TRANSCRIPT_POLL_MAX_AGE", "30") // 30 minutes
		end := now.Add(-45 * time.Minute)
		assert.True(t, nativeTranscriptPollExpired(end, now))
	})
}
