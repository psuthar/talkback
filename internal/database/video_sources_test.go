package database

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/models"
	"github.com/psuthar/talkback/internal/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateVideoSource(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)

	ctx := context.Background()

	// Create an artifact first
	artifact, err := db.CreateArtifact(ctx, "Video Test Artifact", nil)
	require.NoError(t, err)

	t.Run("creates video source with loom provider", func(t *testing.T) {
		videoSource := &models.VideoSource{
			ID:               uuid.New(),
			ArtifactID:       artifact.ID,
			Provider:         "loom",
			VideoURL:         "https://www.loom.com/share/example",
			TranscriptStatus: models.VideoTranscriptStatusMissing,
		}

		err := db.CreateVideoSource(ctx, videoSource)
		require.NoError(t, err)
		assert.False(t, videoSource.CreatedAt.IsZero())
	})

	t.Run("creates video source with zoom provider", func(t *testing.T) {
		videoSource := &models.VideoSource{
			ID:               uuid.New(),
			ArtifactID:       artifact.ID,
			Provider:         "zoom",
			VideoURL:         "https://zoom.us/rec/share/example",
			TranscriptStatus: models.VideoTranscriptStatusMissing,
		}

		err := db.CreateVideoSource(ctx, videoSource)
		require.NoError(t, err)
	})
}

func TestGetVideoSourceByArtifactID(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)

	ctx := context.Background()

	artifact, err := db.CreateArtifact(ctx, "Get Video Test", nil)
	require.NoError(t, err)

	t.Run("returns error for artifact with no video source", func(t *testing.T) {
		videoSource, err := db.GetVideoSourceByArtifactID(ctx, artifact.ID)

		assert.Error(t, err)
		assert.Nil(t, videoSource)
	})

	t.Run("returns video source for artifact", func(t *testing.T) {
		// Create video source
		videoSource := &models.VideoSource{
			ID:               uuid.New(),
			ArtifactID:       artifact.ID,
			Provider:         "loom",
			VideoURL:         "https://www.loom.com/share/test",
			TranscriptStatus: models.VideoTranscriptStatusMissing,
		}
		require.NoError(t, db.CreateVideoSource(ctx, videoSource))

		// Get video source
		retrieved, err := db.GetVideoSourceByArtifactID(ctx, artifact.ID)

		require.NoError(t, err)
		assert.Equal(t, videoSource.ID, retrieved.ID)
		assert.Equal(t, videoSource.Provider, retrieved.Provider)
		assert.Equal(t, videoSource.VideoURL, retrieved.VideoURL)
		assert.Equal(t, models.VideoTranscriptStatusMissing, retrieved.TranscriptStatus)
	})
}

func TestUpdateVideoSourceTranscript(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)

	ctx := context.Background()

	artifact, err := db.CreateArtifact(ctx, "Transcript Test", nil)
	require.NoError(t, err)

	t.Run("updates transcript and sets status to ready", func(t *testing.T) {
		// Create video source
		videoSource := &models.VideoSource{
			ID:               uuid.New(),
			ArtifactID:       artifact.ID,
			Provider:         "loom",
			VideoURL:         "https://www.loom.com/share/test",
			TranscriptStatus: models.VideoTranscriptStatusMissing,
		}
		require.NoError(t, db.CreateVideoSource(ctx, videoSource))

		// Update transcript
		transcriptText := "This is the full transcript text."
		err := db.UpdateVideoSourceTranscript(ctx, videoSource.ID, transcriptText)
		require.NoError(t, err)

		// Verify update
		updated, err := db.GetVideoSourceByID(ctx, videoSource.ID)
		require.NoError(t, err)
		assert.Equal(t, models.VideoTranscriptStatusReady, updated.TranscriptStatus)
		assert.NotNil(t, updated.TranscriptText)
		assert.Equal(t, transcriptText, *updated.TranscriptText)
	})
}
