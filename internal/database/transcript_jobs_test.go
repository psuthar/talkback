package database

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/psuthar/talkback/internal/models"
	"github.com/psuthar/talkback/internal/test"
)

func TestCreateTranscriptJob(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)

	ctx := context.Background()
	session := createTestSession(t, db, "Test Session")
	artifact, err := db.CreateArtifact(ctx, session.ID, "Test Artifact", nil)
	require.NoError(t, err)

	t.Run("creates transcript job with all fields", func(t *testing.T) {
		videoSource := &models.VideoSource{
			ID:         uuid.New(),
			ArtifactID: artifact.ID,
			SessionID:  session.ID,
			Provider:   "loom",
			VideoURL:   "https://www.loom.com/share/test",
			PlaybackMode: "embed",
			EmbedURL:     stringPtr("https://www.loom.com/embed/test"),
			TranscriptStatus: models.VideoTranscriptStatusMissing,
		}
		err := db.CreateVideoSource(ctx, videoSource)
		require.NoError(t, err)

		job := &models.TranscriptJob{
			ID:            uuid.New(),
			VideoSourceID: videoSource.ID,
			SessionID:     session.ID,
			Status:        models.TranscriptJobStatusQueued,
			SourceURL:     "https://www.loom.com/share/test",
			JobKey:        "test-job-key-1",
		}

		err = db.CreateTranscriptJob(ctx, job)
		require.NoError(t, err)
		assert.NotZero(t, job.QueuedAt)
	})


	t.Run("creates transcript job with optional fields", func(t *testing.T) {
		videoSource := &models.VideoSource{
			ID:         uuid.New(),
			ArtifactID: artifact.ID,
			SessionID:  session.ID,
			Provider:   "loom",
			VideoURL:   "https://www.loom.com/share/test2",
			PlaybackMode: "embed",
			EmbedURL:     stringPtr("https://www.loom.com/embed/test2"),
			TranscriptStatus: models.VideoTranscriptStatusMissing,
		}
		err := db.CreateVideoSource(ctx, videoSource)
		require.NoError(t, err)

		now := time.Now()
		job := &models.TranscriptJob{
			ID:              uuid.New(),
			VideoSourceID:   videoSource.ID,
			SessionID:       session.ID,
			Status:          models.TranscriptJobStatusDownloading,
			SourceURL:       "https://www.loom.com/share/test2",
			JobKey:          "test-job-key-2",
			ResolvedMediaURL: stringPtr("https://cdn.loom.com/videos/test.mp4"),
			StartedAt:       &now,
			WhisperModel:    stringPtr("whisper-1"),
			DetectedLanguage: stringPtr("en"),
			DurationSeconds: intPtr(300),
		}

		err = db.CreateTranscriptJob(ctx, job)
		require.NoError(t, err)
		assert.NotZero(t, job.QueuedAt)
	})

	t.Run("returns error for duplicate job key", func(t *testing.T) {
		videoSource := &models.VideoSource{
			ID:         uuid.New(),
			ArtifactID: artifact.ID,
			SessionID:  session.ID,
			Provider:   "loom",
			VideoURL:   "https://www.loom.com/share/test3",
			PlaybackMode: "embed",
			EmbedURL:     stringPtr("https://www.loom.com/embed/test3"),
			TranscriptStatus: models.VideoTranscriptStatusMissing,
		}
		err := db.CreateVideoSource(ctx, videoSource)
		require.NoError(t, err)

		job1 := &models.TranscriptJob{
			ID:            uuid.New(),
			VideoSourceID: videoSource.ID,
			SessionID:     session.ID,
			Status:        models.TranscriptJobStatusQueued,
			SourceURL:     "https://www.loom.com/share/test3",
			JobKey:        "duplicate-key",
		}
		err = db.CreateTranscriptJob(ctx, job1)
		require.NoError(t, err)

		job2 := &models.TranscriptJob{
			ID:            uuid.New(),
			VideoSourceID: videoSource.ID,
			SessionID:     session.ID,
			Status:        models.TranscriptJobStatusQueued,
			SourceURL:     "https://www.loom.com/share/test3",
			JobKey:        "duplicate-key", // Same key
		}
		err = db.CreateTranscriptJob(ctx, job2)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "duplicate key")
	})
}

func TestGetTranscriptJob(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)

	ctx := context.Background()
	session := createTestSession(t, db, "Test Session")
	artifact, err := db.CreateArtifact(ctx, session.ID, "Test Artifact", nil)
	require.NoError(t, err)

	videoSource := &models.VideoSource{
		ID:         uuid.New(),
		ArtifactID: artifact.ID,
		SessionID:  session.ID,
		Provider:   "loom",
		VideoURL:   "https://www.loom.com/share/test",
		PlaybackMode: "embed",
		EmbedURL:     stringPtr("https://www.loom.com/embed/test"),
		TranscriptStatus: models.VideoTranscriptStatusMissing,
	}
	err = db.CreateVideoSource(ctx, videoSource)
	require.NoError(t, err)

	t.Run("returns transcript job by id", func(t *testing.T) {
		job := &models.TranscriptJob{
			ID:            uuid.New(),
			VideoSourceID: videoSource.ID,
			SessionID:     session.ID,
			Status:        models.TranscriptJobStatusQueued,
			SourceURL:     "https://www.loom.com/share/test",
			JobKey:        "test-job-key-get",
		}
		err := db.CreateTranscriptJob(ctx, job)
		require.NoError(t, err)

		retrieved, err := db.GetTranscriptJob(ctx, job.ID)
		require.NoError(t, err)
		assert.Equal(t, job.ID, retrieved.ID)
		assert.Equal(t, job.VideoSourceID, retrieved.VideoSourceID)
		assert.Equal(t, job.SessionID, retrieved.SessionID)
		assert.Equal(t, job.Status, retrieved.Status)
		assert.Equal(t, job.SourceURL, retrieved.SourceURL)
		assert.Equal(t, job.JobKey, retrieved.JobKey)
		assert.Equal(t, job.LoomPassword, retrieved.LoomPassword) // Verify password field (nil in this case)
	})

	t.Run("retrieves transcript job with password", func(t *testing.T) {
		videoSourceWithPassword := &models.VideoSource{
			ID:         uuid.New(),
			ArtifactID: artifact.ID,
			SessionID:  session.ID,
			Provider:   "loom",
			VideoURL:   "https://www.loom.com/share/test-password",
			PlaybackMode: "embed",
			EmbedURL:     stringPtr("https://www.loom.com/embed/test-password"),
			TranscriptStatus: models.VideoTranscriptStatusMissing,
		}
		err := db.CreateVideoSource(ctx, videoSourceWithPassword)
		require.NoError(t, err)

		password := "test-retrieve-password"
		jobWithPassword := &models.TranscriptJob{
			ID:            uuid.New(),
			VideoSourceID: videoSourceWithPassword.ID,
			SessionID:     session.ID,
			Status:        models.TranscriptJobStatusQueued,
			SourceURL:     "https://www.loom.com/share/test-password",
			JobKey:        "test-job-key-password-retrieve",
			LoomPassword:  &password,
		}

		err = db.CreateTranscriptJob(ctx, jobWithPassword)
		require.NoError(t, err)

		retrieved, err := db.GetTranscriptJob(ctx, jobWithPassword.ID)
		require.NoError(t, err)
		assert.NotNil(t, retrieved.LoomPassword)
		assert.Equal(t, password, *retrieved.LoomPassword)
	})

	t.Run("returns error for non-existent job", func(t *testing.T) {
		nonExistentID := uuid.New()
		_, err := db.GetTranscriptJob(ctx, nonExistentID)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})
}

func TestGetTranscriptJobByKey(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)

	ctx := context.Background()
	session := createTestSession(t, db, "Test Session")
	artifact, err := db.CreateArtifact(ctx, session.ID, "Test Artifact", nil)
	require.NoError(t, err)

	videoSource := &models.VideoSource{
		ID:         uuid.New(),
		ArtifactID: artifact.ID,
		SessionID:  session.ID,
		Provider:   "loom",
		VideoURL:   "https://www.loom.com/share/test",
		PlaybackMode: "embed",
		EmbedURL:     stringPtr("https://www.loom.com/embed/test"),
		TranscriptStatus: models.VideoTranscriptStatusMissing,
	}
	err = db.CreateVideoSource(ctx, videoSource)
	require.NoError(t, err)

	t.Run("returns transcript job by key", func(t *testing.T) {
		job := &models.TranscriptJob{
			ID:            uuid.New(),
			VideoSourceID: videoSource.ID,
			SessionID:     session.ID,
			Status:        models.TranscriptJobStatusQueued,
			SourceURL:     "https://www.loom.com/share/test",
			JobKey:        "test-job-key-by-key",
		}
		err := db.CreateTranscriptJob(ctx, job)
		require.NoError(t, err)

		retrieved, err := db.GetTranscriptJobByKey(ctx, job.JobKey)
		require.NoError(t, err)
		assert.Equal(t, job.ID, retrieved.ID)
		assert.Equal(t, job.JobKey, retrieved.JobKey)
	})

	t.Run("returns nil for non-existent job key", func(t *testing.T) {
		job, err := db.GetTranscriptJobByKey(ctx, "non-existent-key")
		assert.NoError(t, err)
		assert.Nil(t, job)
	})
}

func TestUpdateTranscriptJobStatus(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)

	ctx := context.Background()
	session := createTestSession(t, db, "Test Session")
	artifact, err := db.CreateArtifact(ctx, session.ID, "Test Artifact", nil)
	require.NoError(t, err)

	videoSource := &models.VideoSource{
		ID:         uuid.New(),
		ArtifactID: artifact.ID,
		SessionID:  session.ID,
		Provider:   "loom",
		VideoURL:   "https://www.loom.com/share/test",
		PlaybackMode: "embed",
		EmbedURL:     stringPtr("https://www.loom.com/embed/test"),
		TranscriptStatus: models.VideoTranscriptStatusMissing,
	}
	err = db.CreateVideoSource(ctx, videoSource)
	require.NoError(t, err)

	job := &models.TranscriptJob{
		ID:            uuid.New(),
		VideoSourceID: videoSource.ID,
		SessionID:     session.ID,
		Status:        models.TranscriptJobStatusQueued,
		SourceURL:     "https://www.loom.com/share/test",
		JobKey:        "test-job-key-update",
	}
	err = db.CreateTranscriptJob(ctx, job)
	require.NoError(t, err)

	t.Run("updates transcript job status", func(t *testing.T) {
		errorMsg := "Test error message"
		err := db.UpdateTranscriptJobStatus(ctx, job.ID, models.TranscriptJobStatusFailed, &errorMsg)
		require.NoError(t, err)

		updated, err := db.GetTranscriptJob(ctx, job.ID)
		require.NoError(t, err)
		assert.Equal(t, models.TranscriptJobStatusFailed, updated.Status)
		assert.NotNil(t, updated.ErrorMessage)
		assert.Equal(t, errorMsg, *updated.ErrorMessage)
	})

	t.Run("updates status without error message", func(t *testing.T) {
		err := db.UpdateTranscriptJobStatus(ctx, job.ID, models.TranscriptJobStatusDownloading, nil)
		require.NoError(t, err)

		updated, err := db.GetTranscriptJob(ctx, job.ID)
		require.NoError(t, err)
		assert.Equal(t, models.TranscriptJobStatusDownloading, updated.Status)
	})
}

func TestUpdateTranscriptJobStarted(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)

	ctx := context.Background()
	session := createTestSession(t, db, "Test Session")
	artifact, err := db.CreateArtifact(ctx, session.ID, "Test Artifact", nil)
	require.NoError(t, err)

	videoSource := &models.VideoSource{
		ID:         uuid.New(),
		ArtifactID: artifact.ID,
		SessionID:  session.ID,
		Provider:   "loom",
		VideoURL:   "https://www.loom.com/share/test",
		PlaybackMode: "embed",
		EmbedURL:     stringPtr("https://www.loom.com/embed/test"),
		TranscriptStatus: models.VideoTranscriptStatusMissing,
	}
	err = db.CreateVideoSource(ctx, videoSource)
	require.NoError(t, err)

	job := &models.TranscriptJob{
		ID:            uuid.New(),
		VideoSourceID: videoSource.ID,
		SessionID:     session.ID,
		Status:        models.TranscriptJobStatusQueued,
		SourceURL:     "https://www.loom.com/share/test",
		JobKey:        "test-job-key-started",
	}
	err = db.CreateTranscriptJob(ctx, job)
	require.NoError(t, err)

	err = db.UpdateTranscriptJobStarted(ctx, job.ID)
	require.NoError(t, err)

	updated, err := db.GetTranscriptJob(ctx, job.ID)
	require.NoError(t, err)
	assert.Equal(t, models.TranscriptJobStatusDownloading, updated.Status)
	assert.NotNil(t, updated.StartedAt)
}

func TestUpdateTranscriptJobProgress(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)

	ctx := context.Background()
	session := createTestSession(t, db, "Test Session")
	artifact, err := db.CreateArtifact(ctx, session.ID, "Test Artifact", nil)
	require.NoError(t, err)

	videoSource := &models.VideoSource{
		ID:         uuid.New(),
		ArtifactID: artifact.ID,
		SessionID:  session.ID,
		Provider:   "loom",
		VideoURL:   "https://www.loom.com/share/test",
		PlaybackMode: "embed",
		EmbedURL:     stringPtr("https://www.loom.com/embed/test"),
		TranscriptStatus: models.VideoTranscriptStatusMissing,
	}
	err = db.CreateVideoSource(ctx, videoSource)
	require.NoError(t, err)

	job := &models.TranscriptJob{
		ID:            uuid.New(),
		VideoSourceID: videoSource.ID,
		SessionID:     session.ID,
		Status:        models.TranscriptJobStatusQueued,
		SourceURL:     "https://www.loom.com/share/test",
		JobKey:        "test-job-key-progress",
	}
	err = db.CreateTranscriptJob(ctx, job)
	require.NoError(t, err)

	resolvedURL := "https://cdn.loom.com/videos/resolved.mp4"
	err = db.UpdateTranscriptJobProgress(ctx, job.ID, models.TranscriptJobStatusTranscribing, &resolvedURL)
	require.NoError(t, err)

	updated, err := db.GetTranscriptJob(ctx, job.ID)
	require.NoError(t, err)
	assert.Equal(t, models.TranscriptJobStatusTranscribing, updated.Status)
	assert.NotNil(t, updated.ResolvedMediaURL)
	assert.Equal(t, resolvedURL, *updated.ResolvedMediaURL)
}

func TestCompleteTranscriptJob(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)

	ctx := context.Background()
	session := createTestSession(t, db, "Test Session")
	artifact, err := db.CreateArtifact(ctx, session.ID, "Test Artifact", nil)
	require.NoError(t, err)

	videoSource := &models.VideoSource{
		ID:         uuid.New(),
		ArtifactID: artifact.ID,
		SessionID:  session.ID,
		Provider:   "loom",
		VideoURL:   "https://www.loom.com/share/test",
		PlaybackMode: "embed",
		EmbedURL:     stringPtr("https://www.loom.com/embed/test"),
		TranscriptStatus: models.VideoTranscriptStatusMissing,
	}
	err = db.CreateVideoSource(ctx, videoSource)
	require.NoError(t, err)

	job := &models.TranscriptJob{
		ID:            uuid.New(),
		VideoSourceID: videoSource.ID,
		SessionID:     session.ID,
		Status:        models.TranscriptJobStatusQueued,
		SourceURL:     "https://www.loom.com/share/test",
		JobKey:        "test-job-key-complete",
	}
	err = db.CreateTranscriptJob(ctx, job)
	require.NoError(t, err)

	model := "whisper-1"
	language := "en"
	duration := 300

	err = db.CompleteTranscriptJob(ctx, job.ID, &model, &language, &duration)
	require.NoError(t, err)

	updated, err := db.GetTranscriptJob(ctx, job.ID)
	require.NoError(t, err)
	assert.Equal(t, models.TranscriptJobStatusCompleted, updated.Status)
	assert.NotNil(t, updated.CompletedAt)
	assert.NotNil(t, updated.WhisperModel)
	assert.Equal(t, model, *updated.WhisperModel)
	assert.NotNil(t, updated.DetectedLanguage)
	assert.Equal(t, language, *updated.DetectedLanguage)
	assert.NotNil(t, updated.DurationSeconds)
	assert.Equal(t, duration, *updated.DurationSeconds)
}

func TestFailTranscriptJob(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)

	ctx := context.Background()
	session := createTestSession(t, db, "Test Session")
	artifact, err := db.CreateArtifact(ctx, session.ID, "Test Artifact", nil)
	require.NoError(t, err)

	videoSource := &models.VideoSource{
		ID:         uuid.New(),
		ArtifactID: artifact.ID,
		SessionID:  session.ID,
		Provider:   "loom",
		VideoURL:   "https://www.loom.com/share/test",
		PlaybackMode: "embed",
		EmbedURL:     stringPtr("https://www.loom.com/embed/test"),
		TranscriptStatus: models.VideoTranscriptStatusMissing,
	}
	err = db.CreateVideoSource(ctx, videoSource)
	require.NoError(t, err)

	job := &models.TranscriptJob{
		ID:            uuid.New(),
		VideoSourceID: videoSource.ID,
		SessionID:     session.ID,
		Status:        models.TranscriptJobStatusQueued,
		SourceURL:     "https://www.loom.com/share/test",
		JobKey:        "test-job-key-fail",
	}
	err = db.CreateTranscriptJob(ctx, job)
	require.NoError(t, err)

	errorMsg := "Transcription failed: network error"
	err = db.FailTranscriptJob(ctx, job.ID, errorMsg)
	require.NoError(t, err)

	updated, err := db.GetTranscriptJob(ctx, job.ID)
	require.NoError(t, err)
	assert.Equal(t, models.TranscriptJobStatusFailed, updated.Status)
	assert.NotNil(t, updated.CompletedAt)
	assert.NotNil(t, updated.ErrorMessage)
	assert.Equal(t, errorMsg, *updated.ErrorMessage)
}

func TestGetTranscriptJobsByVideoSource(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)

	ctx := context.Background()
	session := createTestSession(t, db, "Test Session")
	artifact, err := db.CreateArtifact(ctx, session.ID, "Test Artifact", nil)
	require.NoError(t, err)

	videoSource1 := &models.VideoSource{
		ID:         uuid.New(),
		ArtifactID: artifact.ID,
		SessionID:  session.ID,
		Provider:   "loom",
		VideoURL:   "https://www.loom.com/share/test1",
		PlaybackMode: "embed",
		EmbedURL:     stringPtr("https://www.loom.com/embed/test1"),
		TranscriptStatus: models.VideoTranscriptStatusMissing,
	}
	err = db.CreateVideoSource(ctx, videoSource1)
	require.NoError(t, err)

	videoSource2 := &models.VideoSource{
		ID:         uuid.New(),
		ArtifactID: artifact.ID,
		SessionID:  session.ID,
		Provider:   "loom",
		VideoURL:   "https://www.loom.com/share/test2",
		PlaybackMode: "embed",
		EmbedURL:     stringPtr("https://www.loom.com/embed/test2"),
		TranscriptStatus: models.VideoTranscriptStatusMissing,
	}
	err = db.CreateVideoSource(ctx, videoSource2)
	require.NoError(t, err)

	// Create jobs for videoSource1
	job1 := &models.TranscriptJob{
		ID:            uuid.New(),
		VideoSourceID: videoSource1.ID,
		SessionID:     session.ID,
		Status:        models.TranscriptJobStatusQueued,
		SourceURL:     "https://www.loom.com/share/test1",
		JobKey:        "test-job-key-1",
	}
	err = db.CreateTranscriptJob(ctx, job1)
	require.NoError(t, err)

	job2 := &models.TranscriptJob{
		ID:            uuid.New(),
		VideoSourceID: videoSource1.ID,
		SessionID:     session.ID,
		Status:        models.TranscriptJobStatusCompleted,
		SourceURL:     "https://www.loom.com/share/test1",
		JobKey:        "test-job-key-2",
	}
	err = db.CreateTranscriptJob(ctx, job2)
	require.NoError(t, err)

	// Create job for videoSource2
	job3 := &models.TranscriptJob{
		ID:            uuid.New(),
		VideoSourceID: videoSource2.ID,
		SessionID:     session.ID,
		Status:        models.TranscriptJobStatusQueued,
		SourceURL:     "https://www.loom.com/share/test2",
		JobKey:        "test-job-key-3",
	}
	err = db.CreateTranscriptJob(ctx, job3)
	require.NoError(t, err)

	t.Run("returns all jobs for video source", func(t *testing.T) {
		jobs, err := db.GetTranscriptJobsByVideoSource(ctx, videoSource1.ID)
		require.NoError(t, err)
		assert.Len(t, jobs, 2)
		// Both jobs should be present (order may vary if created at same time)
		jobIDs := make(map[uuid.UUID]bool)
		for _, job := range jobs {
			jobIDs[job.ID] = true
		}
		assert.True(t, jobIDs[job1.ID], "job1 should be in results")
		assert.True(t, jobIDs[job2.ID], "job2 should be in results")
		// Verify they are for the correct video source
		for _, job := range jobs {
			assert.Equal(t, videoSource1.ID, job.VideoSourceID)
		}
	})

	t.Run("returns empty list for video source with no jobs", func(t *testing.T) {
		emptyVideoSource := &models.VideoSource{
			ID:         uuid.New(),
			ArtifactID: artifact.ID,
			SessionID:  session.ID,
			Provider:   "loom",
			VideoURL:   "https://www.loom.com/share/empty",
			PlaybackMode: "embed",
			EmbedURL:     stringPtr("https://www.loom.com/embed/empty"),
			TranscriptStatus: models.VideoTranscriptStatusMissing,
		}
		err := db.CreateVideoSource(ctx, emptyVideoSource)
		require.NoError(t, err)

		jobs, err := db.GetTranscriptJobsByVideoSource(ctx, emptyVideoSource.ID)
		require.NoError(t, err)
		assert.Empty(t, jobs)
	})
}

func TestUpdateVideoSourceTranscriptionJob(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)

	ctx := context.Background()
	session := createTestSession(t, db, "Test Session")
	artifact, err := db.CreateArtifact(ctx, session.ID, "Test Artifact", nil)
	require.NoError(t, err)

	videoSource := &models.VideoSource{
		ID:         uuid.New(),
		ArtifactID: artifact.ID,
		SessionID:  session.ID,
		Provider:   "loom",
		VideoURL:   "https://www.loom.com/share/test",
		PlaybackMode: "embed",
		EmbedURL:     stringPtr("https://www.loom.com/embed/test"),
		TranscriptStatus: models.VideoTranscriptStatusMissing,
	}
	err = db.CreateVideoSource(ctx, videoSource)
	require.NoError(t, err)

	job := &models.TranscriptJob{
		ID:            uuid.New(),
		VideoSourceID: videoSource.ID,
		SessionID:     session.ID,
		Status:        models.TranscriptJobStatusQueued,
		SourceURL:     "https://www.loom.com/share/test",
		JobKey:        "test-job-key-update-video",
	}
	err = db.CreateTranscriptJob(ctx, job)
	require.NoError(t, err)

	err = db.UpdateVideoSourceTranscriptionJob(ctx, videoSource.ID, &job.ID)
	require.NoError(t, err)

	updated, err := db.GetVideoSourceByID(ctx, videoSource.ID)
	require.NoError(t, err)
	assert.NotNil(t, updated.TranscriptionJobID)
	assert.Equal(t, job.ID, *updated.TranscriptionJobID)
}

func TestUpdateVideoSourceTranscriptionSource(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)

	ctx := context.Background()
	session := createTestSession(t, db, "Test Session")
	artifact, err := db.CreateArtifact(ctx, session.ID, "Test Artifact", nil)
	require.NoError(t, err)

	videoSource := &models.VideoSource{
		ID:         uuid.New(),
		ArtifactID: artifact.ID,
		SessionID:  session.ID,
		Provider:   "loom",
		VideoURL:   "https://www.loom.com/share/test",
		PlaybackMode: "embed",
		EmbedURL:     stringPtr("https://www.loom.com/embed/test"),
		TranscriptStatus: models.VideoTranscriptStatusMissing,
	}
	err = db.CreateVideoSource(ctx, videoSource)
	require.NoError(t, err)

	err = db.UpdateVideoSourceTranscriptionSource(ctx, videoSource.ID, "whisper")
	require.NoError(t, err)

	updated, err := db.GetVideoSourceByID(ctx, videoSource.ID)
	require.NoError(t, err)
		assert.NotNil(t, updated.TranscriptionSource)
		assert.Equal(t, "whisper", *updated.TranscriptionSource)
}


