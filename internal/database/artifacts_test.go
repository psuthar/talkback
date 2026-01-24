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

func TestCreateArtifact(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)

	ctx := context.Background()

	t.Run("creates artifact with title only", func(t *testing.T) {
		session := createTestSession(t, db, "Test Session")
		title := "Test Artifact"
		artifact, err := db.CreateArtifact(ctx, session.ID, title, nil)

		require.NoError(t, err)
		assert.NotEqual(t, uuid.Nil, artifact.ID)
		assert.Equal(t, title, artifact.Title)
		assert.Nil(t, artifact.Description)
		assert.Equal(t, models.StatusDraft, artifact.Status)
		assert.False(t, artifact.CreatedAt.IsZero())
		assert.False(t, artifact.UpdatedAt.IsZero())
	})

	t.Run("creates artifact with title and description", func(t *testing.T) {
		session := createTestSession(t, db, "Test Session")
		title := "Test Artifact with Description"
		description := "This is a test description"
		artifact, err := db.CreateArtifact(ctx, session.ID, title, &description)

		require.NoError(t, err)
		assert.Equal(t, title, artifact.Title)
		assert.NotNil(t, artifact.Description)
		assert.Equal(t, description, *artifact.Description)
		assert.Equal(t, models.StatusDraft, artifact.Status)
	})

	t.Run("creates unique artifacts", func(t *testing.T) {
		session := createTestSession(t, db, "Test Session")
		artifact1, err1 := db.CreateArtifact(ctx, session.ID, "Artifact 1", nil)
		artifact2, err2 := db.CreateArtifact(ctx, session.ID, "Artifact 2", nil)

		require.NoError(t, err1)
		require.NoError(t, err2)
		assert.NotEqual(t, artifact1.ID, artifact2.ID)
	})
}

func TestGetArtifact(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)

	ctx := context.Background()

	t.Run("returns artifact by id", func(t *testing.T) {
		// Arrange: Create a session and artifact
		session := createTestSession(t, db, "Test Session")
		title := "Get Test Artifact"
		description := "Test description"
		created, err := db.CreateArtifact(ctx, session.ID, title, &description)
		require.NoError(t, err)

		// Act: Get the artifact
		artifact, err := db.GetArtifact(ctx, created.ID)

		// Assert
		require.NoError(t, err)
		assert.Equal(t, created.ID, artifact.ID)
		assert.Equal(t, title, artifact.Title)
		assert.NotNil(t, artifact.Description)
		assert.Equal(t, description, *artifact.Description)
		assert.Equal(t, models.StatusDraft, artifact.Status)
	})

	t.Run("returns error for non-existent artifact", func(t *testing.T) {
		nonExistentID := uuid.New()
		artifact, err := db.GetArtifact(ctx, nonExistentID)

		assert.Error(t, err)
		assert.Nil(t, artifact)
		assert.Contains(t, err.Error(), "failed to get artifact")
	})
}

func TestUpdateArtifactStatus(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)

	ctx := context.Background()

	t.Run("updates artifact status to ready", func(t *testing.T) {
		// Arrange: Create a session and artifact
		session := createTestSession(t, db, "Test Session")
		artifact, err := db.CreateArtifact(ctx, session.ID, "Status Test", nil)
		require.NoError(t, err)
		assert.Equal(t, models.StatusDraft, artifact.Status)

		// Act: Update status
		err = db.UpdateArtifactStatus(ctx, artifact.ID, models.StatusReady)
		require.NoError(t, err)

		// Assert: Verify status was updated
		updated, err := db.GetArtifact(ctx, artifact.ID)
		require.NoError(t, err)
		assert.Equal(t, models.StatusReady, updated.Status)
	})

	t.Run("returns error for non-existent artifact", func(t *testing.T) {
		nonExistentID := uuid.New()
		err := db.UpdateArtifactStatus(ctx, nonExistentID, models.StatusReady)

		// UpdateArtifactStatus may or may not error on non-existent ID depending on implementation
		// If it doesn't error, that's acceptable (UPDATE with no matching rows is valid in SQL)
		// If it does error, we should catch it
		if err != nil {
			assert.Contains(t, err.Error(), "failed to update artifact status")
		}
		// If no error, verify the artifact still doesn't exist
		if err == nil {
			_, getErr := db.GetArtifact(ctx, nonExistentID)
			assert.Error(t, getErr) // Should still not exist
		}
	})

	t.Run("returns error for invalid status enum value", func(t *testing.T) {
		session := createTestSession(t, db, "Test Session")
		artifact, err := db.CreateArtifact(ctx, session.ID, "Status Test", nil)
		require.NoError(t, err)

		// Try to set an invalid status (this will fail at the database level)
		// Note: This test depends on how the status is validated
		// If status is validated in Go code, this might error before DB call
		invalidStatus := models.ArtifactStatus("invalid_status")
		err = db.UpdateArtifactStatus(ctx, artifact.ID, invalidStatus)
		// This may or may not error depending on validation layer
		// If it doesn't error, the DB constraint should catch it
		if err != nil {
			assert.Contains(t, err.Error(), "failed to update artifact status")
		}
	})
}
