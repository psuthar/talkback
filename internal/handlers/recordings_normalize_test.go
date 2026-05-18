// SCRUM-466: response-shape normalization for the three platform
// recordings endpoints. The SPA's RecordingsPicker reads every row
// through the same Zoom-shaped accessor (meeting_topic, meeting_uuid,
// instance_uuid, duration_minutes, has_transcript). Meet + Teams used
// to leak their platform-native fields (subject / conference_record_name
// / recording_name / meeting_id / recording_id) which surfaced as
// "(untitled)" rows + Import failures with "X and Y required".
package handlers

import (
	"testing"

	"github.com/psuthar/talkback/internal/googlemeet"
	"github.com/psuthar/talkback/internal/msgraph"
	"github.com/stretchr/testify/assert"
)

func TestNormalizeMeetItem(t *testing.T) {
	t.Parallel()

	t.Run("populated subject becomes the meeting topic", func(t *testing.T) {
		got := normalizeMeetItem(googlemeet.RecordingListItem{
			ConferenceRecordName: "conferenceRecords/A",
			RecordingName:        "conferenceRecords/A/recordings/r1",
			Subject:              "Design Review",
			StartTime:            "2026-05-13T14:22:26Z",
			DriveFileID:          "drive-1",
			ExportURI:            "https://drive.google.com/...",
			TranscriptState:      "ready",
		})
		assert.Equal(t, "Design Review", got.MeetingTopic)
		assert.Equal(t, "2026-05-13T14:22:26Z", got.StartTime)
		assert.Equal(t, "conferenceRecords/A", got.MeetingUUID)
		assert.Equal(t, "conferenceRecords/A/recordings/r1", got.InstanceUUID)
		assert.True(t, got.HasVideo)
		assert.True(t, got.HasTranscript)
		assert.Equal(t, 1, got.RecordingCount)
	})

	t.Run("empty subject falls back to a start-time-derived title", func(t *testing.T) {
		got := normalizeMeetItem(googlemeet.RecordingListItem{
			ConferenceRecordName: "conferenceRecords/B",
			RecordingName:        "conferenceRecords/B/recordings/r1",
			Subject:              "",
			StartTime:            "2026-05-13T14:22:26Z",
			TranscriptState:      "pending",
		})
		assert.NotEqual(t, "(untitled)", got.MeetingTopic)
		assert.NotEmpty(t, got.MeetingTopic, "fallback title must not be empty")
		assert.False(t, got.HasTranscript, "transcript_state=pending must not mark has_transcript")
	})

	t.Run("missing drive download → HasVideo=false", func(t *testing.T) {
		got := normalizeMeetItem(googlemeet.RecordingListItem{
			ConferenceRecordName: "conferenceRecords/C",
			RecordingName:        "conferenceRecords/C/recordings/r1",
			Subject:              "X",
		})
		assert.False(t, got.HasVideo)
	})
}

func TestNormalizeTeamsItem(t *testing.T) {
	t.Parallel()

	t.Run("populated subject becomes the meeting topic", func(t *testing.T) {
		got := normalizeTeamsItem(msgraph.RecordingListItem{
			MeetingID:   "MSPB_meeting-id-1",
			RecordingID: "drive-item-recording-1",
			Subject:     "Customer call",
			StartTime:   "2026-03-21T21:00:00Z",
		})
		assert.Equal(t, "Customer call", got.MeetingTopic)
		assert.Equal(t, "2026-03-21T21:00:00Z", got.StartTime)
		assert.Equal(t, "MSPB_meeting-id-1", got.MeetingUUID)
		assert.Equal(t, "drive-item-recording-1", got.InstanceUUID)
		assert.True(t, got.HasVideo)
		assert.False(t, got.HasTranscript)
		assert.Equal(t, 1, got.RecordingCount)
	})

	t.Run("empty subject falls back to a start-time-derived title", func(t *testing.T) {
		got := normalizeTeamsItem(msgraph.RecordingListItem{
			MeetingID:   "M",
			RecordingID: "R",
			Subject:     "  ",
			StartTime:   "2026-03-21T21:00:00Z",
		})
		assert.NotEqual(t, "(untitled)", got.MeetingTopic)
		assert.NotEmpty(t, got.MeetingTopic)
	})
}
