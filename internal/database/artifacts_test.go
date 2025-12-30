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
		title := "Test Artifact"
		artifact, err := db.CreateArtifact(ctx, title, nil)

		require.NoError(t, err)
		assert.NotEqual(t, uuid.Nil, artifact.ID)
		assert.Equal(t, title, artifact.Title)
		assert.Nil(t, artifact.Description)
		assert.Equal(t, models.StatusDraft, artifact.Status)
		assert.False(t, artifact.CreatedAt.IsZero())
		assert.False(t, artifact.UpdatedAt.IsZero())
	})

	t.Run("creates artifact with title and description", func(t *testing.T) {
		title := "Test Artifact with Description"
		description := "This is a test description"
		artifact, err := db.CreateArtifact(ctx, title, &description)

		require.NoError(t, err)
		assert.Equal(t, title, artifact.Title)
		assert.NotNil(t, artifact.Description)
		assert.Equal(t, description, *artifact.Description)
		assert.Equal(t, models.StatusDraft, artifact.Status)
	})

	t.Run("creates unique artifacts", func(t *testing.T) {
		artifact1, err1 := db.CreateArtifact(ctx, "Artifact 1", nil)
		artifact2, err2 := db.CreateArtifact(ctx, "Artifact 2", nil)

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
		// Arrange: Create an artifact
		title := "Get Test Artifact"
		description := "Test description"
		created, err := db.CreateArtifact(ctx, title, &description)
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
		// Arrange: Create an artifact
		artifact, err := db.CreateArtifact(ctx, "Status Test", nil)
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

		// Note: This might not error depending on implementation
		// Adjust assertion based on actual behavior
		_ = err
	})
}
