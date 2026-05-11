package database

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/models"
	"github.com/psuthar/talkback/internal/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// SCRUM-379: migration 000049 drops Loom support. This test verifies the
// post-migration schema state: 'loom' provider is rejected, 'loom_api'
// transcription_source is rejected, and the transcript_jobs.loom_password
// column no longer exists.
func TestMigration_RemoveLoom_ConstraintsAndColumn(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)

	ctx := context.Background()
	session := createTestSession(t, db, "Migration Test Session")
	artifact, err := db.CreateArtifact(ctx, session.ID, "Migration Test Artifact", nil)
	require.NoError(t, err)

	t.Run("provider='loom' is rejected by the CHECK constraint", func(t *testing.T) {
		vs := &models.VideoSource{
			ID:               uuid.New(),
			ArtifactID:       artifact.ID,
			SessionID:        session.ID,
			Provider:         "loom",
			VideoURL:         "https://example.com/share/test",
			PlaybackMode:     "embed",
			EmbedURL:         stringPtr("https://example.com/embed/test"),
			TranscriptStatus: models.VideoTranscriptStatusMissing,
		}
		err := db.CreateVideoSource(ctx, vs)
		require.Error(t, err)
		assert.True(t, strings.Contains(strings.ToLower(err.Error()), "video_sources_provider_check") ||
			strings.Contains(strings.ToLower(err.Error()), "check constraint"),
			"expected provider check constraint violation, got: %v", err)
	})

	t.Run("transcription_source='loom_api' is rejected by the CHECK constraint", func(t *testing.T) {
		vs := &models.VideoSource{
			ID:               uuid.New(),
			ArtifactID:       artifact.ID,
			SessionID:        session.ID,
			Provider:         "other",
			VideoURL:         "https://example.com/video.mp4",
			PlaybackMode:     "embed",
			EmbedURL:         stringPtr("https://example.com/video.mp4"),
			TranscriptStatus: models.VideoTranscriptStatusMissing,
		}
		require.NoError(t, db.CreateVideoSource(ctx, vs))

		err := db.UpdateVideoSourceTranscriptionSource(ctx, vs.ID, "loom_api")
		require.Error(t, err)
		assert.True(t, strings.Contains(strings.ToLower(err.Error()), "transcription_source") ||
			strings.Contains(strings.ToLower(err.Error()), "check constraint"),
			"expected transcription_source check constraint violation, got: %v", err)
	})

	t.Run("transcript_jobs.loom_password column has been dropped", func(t *testing.T) {
		var exists bool
		err := db.Pool.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM information_schema.columns
				WHERE table_name = 'transcript_jobs' AND column_name = 'loom_password'
			)
		`).Scan(&exists)
		require.NoError(t, err)
		assert.False(t, exists, "transcript_jobs.loom_password should no longer exist after migration 000049")
	})

	t.Run("allowed providers still pass the CHECK constraint", func(t *testing.T) {
		for _, provider := range []string{"zoom", "teams", "google_meet", "other"} {
			vs := &models.VideoSource{
				ID:               uuid.New(),
				ArtifactID:       artifact.ID,
				SessionID:        session.ID,
				Provider:         provider,
				VideoURL:         "https://example.com/video.mp4",
				PlaybackMode:     "embed",
				EmbedURL:         stringPtr("https://example.com/video.mp4"),
				TranscriptStatus: models.VideoTranscriptStatusMissing,
			}
			require.NoError(t, db.CreateVideoSource(ctx, vs), "provider %q should be allowed", provider)
		}
	})
}
