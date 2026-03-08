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

	// Create a session and artifact first
	session := createTestSession(t, db, "Test Session")
	artifact, err := db.CreateArtifact(ctx, session.ID, "Video Test Artifact", nil)
	require.NoError(t, err)

	t.Run("creates video source with loom provider (embed mode)", func(t *testing.T) {
		embedURL := "https://www.loom.com/share/example"
		videoSource := &models.VideoSource{
			ID:               uuid.New(),
			ArtifactID:       artifact.ID,
			SessionID:        session.ID,
			Provider:         "loom",
			VideoURL:         embedURL,
			PlaybackMode:     "embed",
			EmbedURL:         &embedURL,
			TranscriptStatus: models.VideoTranscriptStatusMissing,
		}

		err := db.CreateVideoSource(ctx, videoSource)
		require.NoError(t, err)
		assert.False(t, videoSource.CreatedAt.IsZero())
		assert.Equal(t, "embed", videoSource.PlaybackMode)
		assert.NotNil(t, videoSource.EmbedURL)
		assert.Equal(t, embedURL, *videoSource.EmbedURL)
	})

	t.Run("creates video source with direct mode", func(t *testing.T) {
		mediaURL := "https://example.com/video.mp4"
		posterURL := "https://example.com/poster.jpg"
		duration := 1234
		videoSource := &models.VideoSource{
			ID:               uuid.New(),
			ArtifactID:       artifact.ID,
			SessionID:        session.ID,
			Provider:         "other",
			VideoURL:         mediaURL,
			PlaybackMode:     "direct",
			MediaURL:         &mediaURL,
			PosterURL:        &posterURL,
			DurationSeconds:  &duration,
			TranscriptStatus: models.VideoTranscriptStatusMissing,
		}

		err := db.CreateVideoSource(ctx, videoSource)
		require.NoError(t, err)
		assert.Equal(t, "direct", videoSource.PlaybackMode)
		assert.NotNil(t, videoSource.MediaURL)
		assert.Equal(t, mediaURL, *videoSource.MediaURL)
		assert.NotNil(t, videoSource.PosterURL)
		assert.Equal(t, posterURL, *videoSource.PosterURL)
		assert.NotNil(t, videoSource.DurationSeconds)
		assert.Equal(t, duration, *videoSource.DurationSeconds)
	})

	t.Run("creates video source with zoom provider", func(t *testing.T) {
		embedURL := "https://zoom.us/rec/share/example"
		videoSource := &models.VideoSource{
			ID:               uuid.New(),
			ArtifactID:       artifact.ID,
			SessionID:        session.ID,
			Provider:         "zoom",
			VideoURL:         embedURL,
			PlaybackMode:     "embed",
			EmbedURL:         &embedURL,
			TranscriptStatus: models.VideoTranscriptStatusMissing,
		}

		err := db.CreateVideoSource(ctx, videoSource)
		require.NoError(t, err)
	})

	t.Run("returns error for non-existent artifact_id", func(t *testing.T) {
		nonExistentArtifactID := uuid.New()
		embedURL := "https://www.loom.com/share/example"
		videoSource := &models.VideoSource{
			ID:               uuid.New(),
			ArtifactID:       nonExistentArtifactID,
			SessionID:        session.ID,
			Provider:         "loom",
			VideoURL:         embedURL,
			PlaybackMode:     "embed",
			EmbedURL:         &embedURL,
			TranscriptStatus: models.VideoTranscriptStatusMissing,
		}

		err := db.CreateVideoSource(ctx, videoSource)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to create video source")
		// Should be a foreign key constraint violation
		errMsg := err.Error()
		assert.True(t, containsString(errMsg, "foreign key") || containsString(errMsg, "violates foreign key"), 
			"Expected foreign key error, got: %s", errMsg)
	})

	t.Run("returns error for invalid provider enum value", func(t *testing.T) {
		embedURL := "https://www.loom.com/share/example"
		videoSource := &models.VideoSource{
			ID:               uuid.New(),
			ArtifactID:       artifact.ID,
			SessionID:        session.ID,
			Provider:         "invalid_provider", // Invalid enum value
			VideoURL:         embedURL,
			PlaybackMode:     "embed",
			EmbedURL:         &embedURL,
			TranscriptStatus: models.VideoTranscriptStatusMissing,
		}

		err := db.CreateVideoSource(ctx, videoSource)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to create video source")
	})

	t.Run("returns error for invalid transcript_status enum value", func(t *testing.T) {
		embedURL := "https://www.loom.com/share/example"
		videoSource := &models.VideoSource{
			ID:               uuid.New(),
			ArtifactID:       artifact.ID,
			SessionID:        session.ID,
			Provider:         "loom",
			VideoURL:         embedURL,
			PlaybackMode:     "embed",
			EmbedURL:         &embedURL,
			TranscriptStatus: "invalid_status", // Invalid enum value
		}

		err := db.CreateVideoSource(ctx, videoSource)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to create video source")
	})
}

func TestGetVideoSourceByArtifactID(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)

	ctx := context.Background()

	session := createTestSession(t, db, "Test Session")
	artifact, err := db.CreateArtifact(ctx, session.ID, "Get Video Test", nil)
	require.NoError(t, err)

	t.Run("returns error for artifact with no video source", func(t *testing.T) {
		videoSource, err := db.GetVideoSourceByArtifactID(ctx, artifact.ID)

		assert.Error(t, err)
		assert.Nil(t, videoSource)
	})

	t.Run("returns video source for artifact", func(t *testing.T) {
		// Create video source
		embedURL := "https://www.loom.com/share/test"
		videoSource := &models.VideoSource{
			ID:               uuid.New(),
			ArtifactID:       artifact.ID,
			SessionID:        session.ID,
			Provider:         "loom",
			VideoURL:         embedURL,
			PlaybackMode:     "embed",
			EmbedURL:         &embedURL,
			TranscriptStatus: models.VideoTranscriptStatusMissing,
		}
		require.NoError(t, db.CreateVideoSource(ctx, videoSource))

		// Get video source
		retrieved, err := db.GetVideoSourceByArtifactID(ctx, artifact.ID)

		require.NoError(t, err)
		assert.Equal(t, videoSource.ID, retrieved.ID)
		assert.Equal(t, videoSource.Provider, retrieved.Provider)
		assert.Equal(t, videoSource.VideoURL, retrieved.VideoURL)
		assert.Equal(t, videoSource.PlaybackMode, retrieved.PlaybackMode)
		assert.NotNil(t, retrieved.EmbedURL)
		assert.Equal(t, embedURL, *retrieved.EmbedURL)
		assert.Equal(t, models.VideoTranscriptStatusMissing, retrieved.TranscriptStatus)
	})
}

func TestUpdateVideoSourceTranscript(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)

	ctx := context.Background()

	session := createTestSession(t, db, "Test Session")
	artifact, err := db.CreateArtifact(ctx, session.ID, "Transcript Test", nil)
	require.NoError(t, err)

	t.Run("updates transcript and sets status to ready", func(t *testing.T) {
		// Create video source
		embedURL := "https://www.loom.com/share/test"
		videoSource := &models.VideoSource{
			ID:               uuid.New(),
			ArtifactID:       artifact.ID,
			SessionID:        session.ID,
			Provider:         "loom",
			VideoURL:         embedURL,
			PlaybackMode:     "embed",
			EmbedURL:         &embedURL,
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

	t.Run("returns error for non-existent video_source_id", func(t *testing.T) {
		nonExistentVideoID := uuid.New()
		transcriptText := "This transcript should not be saved."

		err := db.UpdateVideoSourceTranscript(ctx, nonExistentVideoID, transcriptText)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to update video source transcript")
	})

	t.Run("SetVideoSourceVideoRole sets primary and demotes previous primary", func(t *testing.T) {
		session := createTestSession(t, db, "Role Test Session")
		art1, _ := db.CreateArtifact(ctx, session.ID, "Art 1", nil)
		art2, _ := db.CreateArtifact(ctx, session.ID, "Art 2", nil)
		vs1 := &models.VideoSource{
			ID: uuid.New(), ArtifactID: art1.ID, SessionID: session.ID, Provider: "other",
			VideoURL: "https://example.com/1", PlaybackMode: "embed", TranscriptStatus: models.VideoTranscriptStatusReady,
		}
		vs2 := &models.VideoSource{
			ID: uuid.New(), ArtifactID: art2.ID, SessionID: session.ID, Provider: "other",
			VideoURL: "https://example.com/2", PlaybackMode: "embed", TranscriptStatus: models.VideoTranscriptStatusReady,
		}
		require.NoError(t, db.CreateVideoSource(ctx, vs1))
		require.NoError(t, db.CreateVideoSource(ctx, vs2))

		require.NoError(t, db.SetVideoSourceVideoRole(ctx, session.ID, vs1.ID, models.VideoRolePrimary))
		got1, _ := db.GetVideoSourceByID(ctx, vs1.ID)
		require.NotNil(t, got1.VideoRole)
		assert.Equal(t, models.VideoRolePrimary, *got1.VideoRole)

		require.NoError(t, db.SetVideoSourceVideoRole(ctx, session.ID, vs2.ID, models.VideoRolePrimary))
		got1Again, _ := db.GetVideoSourceByID(ctx, vs1.ID)
		got2, _ := db.GetVideoSourceByID(ctx, vs2.ID)
		require.NotNil(t, got1Again.VideoRole)
		assert.Equal(t, models.VideoRoleSecondary, *got1Again.VideoRole)
		require.NotNil(t, got2.VideoRole)
		assert.Equal(t, models.VideoRolePrimary, *got2.VideoRole)
	})
}
