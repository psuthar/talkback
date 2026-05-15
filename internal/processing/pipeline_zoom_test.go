package processing

import (
	"testing"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/models"
	"github.com/stretchr/testify/assert"
)

// TestMeetingUUIDForJob locks in the SCRUM-409 file-move: meetingUUIDForJob
// resolves a Zoom job's meeting/instance identifier with the same precedence
// (instance_uuid first, meeting_uuid as fallback, empty otherwise) it had in
// pipeline.go before the move.
func TestMeetingUUIDForJob(t *testing.T) {
	t.Parallel()

	instance := "abc-instance"
	meeting := "xyz-meeting"
	emptyStr := ""

	cases := []struct {
		name string
		job  *models.SessionProcessingJob
		want string
	}{
		{"both unset returns empty", &models.SessionProcessingJob{}, ""},
		{"instance_uuid only", &models.SessionProcessingJob{InstanceUUID: &instance}, instance},
		{"meeting_uuid only", &models.SessionProcessingJob{MeetingUUID: &meeting}, meeting},
		{"instance wins when both set", &models.SessionProcessingJob{InstanceUUID: &instance, MeetingUUID: &meeting}, instance},
		{"empty-string instance falls back to meeting", &models.SessionProcessingJob{InstanceUUID: &emptyStr, MeetingUUID: &meeting}, meeting},
		{"both empty pointers return empty", &models.SessionProcessingJob{InstanceUUID: &emptyStr, MeetingUUID: &emptyStr}, emptyStr},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			tc.job.ID = uuid.New()
			tc.job.SessionID = uuid.New()
			assert.Equal(t, tc.want, meetingUUIDForJob(tc.job))
		})
	}
}

// TestZoomArtifactMetadata locks in the SCRUM-409 file-move:
// zoomArtifactMetadata emits {} only when at least one of (meeting_uuid,
// recording_file_id) is non-empty, with stable JSON keys.
func TestZoomArtifactMetadata(t *testing.T) {
	t.Parallel()
	assert.Nil(t, zoomArtifactMetadata("", nil), "empty inputs produce no metadata")
	assert.NotEmpty(t, zoomArtifactMetadata("uuid-1", nil), "meeting_uuid alone produces metadata")
}
