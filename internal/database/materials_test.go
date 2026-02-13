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

func TestCreateMaterial(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)

	ctx := context.Background()

	// Create a session and artifact first
	session := createTestSession(t, db, "Test Session")
	artifact, err := db.CreateArtifact(ctx, session.ID, "Material Test Artifact", nil)
	require.NoError(t, err)

	t.Run("creates material with all fields", func(t *testing.T) {
		material := &models.Material{
			ID:            uuid.New(),
			ArtifactID:    artifact.ID,
			SessionID:     session.ID,
			Kind:          "document",
			Filename:      "test.txt",
			ContentType:   "text/plain",
			StorageURL:    "data/uploads/test.txt",
			TextStatus:    models.MaterialTextStatusReady,
			ExtractedText: stringPtr("Extracted text content"),
		}

		err := db.CreateMaterial(ctx, material)
		require.NoError(t, err)
	})

	t.Run("creates material with pending text status", func(t *testing.T) {
		material := &models.Material{
			ID:          uuid.New(),
			ArtifactID:  artifact.ID,
			SessionID:   session.ID,
			Kind:        "slides",
			Filename:    "presentation.pdf",
			ContentType: "application/pdf",
			StorageURL:  "data/uploads/presentation.pdf",
			TextStatus:  models.MaterialTextStatusPending,
		}

		err := db.CreateMaterial(ctx, material)
		require.NoError(t, err)
	})

	t.Run("returns error for non-existent artifact_id", func(t *testing.T) {
		nonExistentArtifactID := uuid.New()
		material := &models.Material{
			ID:          uuid.New(),
			ArtifactID:  nonExistentArtifactID,
			SessionID:   session.ID,
			Kind:        "document",
			Filename:    "test.txt",
			ContentType: "text/plain",
			StorageURL:  "data/uploads/test.txt",
			TextStatus:  models.MaterialTextStatusReady,
		}

		err := db.CreateMaterial(ctx, material)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to create material")
		// Should be a foreign key constraint violation
		errMsg := err.Error()
		assert.True(t, containsString(errMsg, "foreign key") || containsString(errMsg, "violates foreign key"), 
			"Expected foreign key error, got: %s", errMsg)
	})

	t.Run("returns error for invalid text_status enum value", func(t *testing.T) {
		material := &models.Material{
			ID:          uuid.New(),
			ArtifactID:  artifact.ID,
			SessionID:   session.ID,
			Kind:        "document",
			Filename:    "test.txt",
			ContentType: "text/plain",
			StorageURL:  "data/uploads/test.txt",
			TextStatus:  "invalid_status", // Invalid enum value
		}

		err := db.CreateMaterial(ctx, material)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to create material")
	})

	t.Run("allows any kind value (no enum constraint)", func(t *testing.T) {
		// Note: The database schema doesn't enforce a CHECK constraint on kind,
		// so any string value is allowed. This test verifies that behavior.
		material := &models.Material{
			ID:          uuid.New(),
			ArtifactID:  artifact.ID,
			SessionID:   session.ID,
			Kind:        "custom_kind", // Any value is allowed
			Filename:    "test.txt",
			ContentType: "text/plain",
			StorageURL:  "data/uploads/test.txt",
			TextStatus:  models.MaterialTextStatusReady,
		}

		err := db.CreateMaterial(ctx, material)
		// Should succeed since there's no enum constraint on kind
		assert.NoError(t, err)
	})

	t.Run("creates material with title and error_message", func(t *testing.T) {
		title := "My Document"
		errMsg := "extraction failed"
		material := &models.Material{
			ID:            uuid.New(),
			ArtifactID:    artifact.ID,
			SessionID:     session.ID,
			Kind:          "document",
			Filename:      "doc.pdf",
			ContentType:   "application/pdf",
			StorageURL:    "data/uploads/doc.pdf",
			TextStatus:    models.MaterialTextStatusFailed,
			Title:         &title,
			ErrorMessage:  &errMsg,
		}
		err := db.CreateMaterial(ctx, material)
		require.NoError(t, err)
		got, err := db.GetMaterialByID(ctx, material.ID)
		require.NoError(t, err)
		require.NotNil(t, got)
		assert.Equal(t, title, *got.Title)
		assert.Equal(t, errMsg, *got.ErrorMessage)
	})
}

func TestDeleteMaterial(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)
	ctx := context.Background()

	session := createTestSession(t, db, "Test Session")
	artifact, err := db.CreateArtifact(ctx, session.ID, "Test Artifact", nil)
	require.NoError(t, err)

	material := &models.Material{
		ID:          uuid.New(),
		ArtifactID:  artifact.ID,
		SessionID:   session.ID,
		Kind:        "document",
		Filename:    "to-delete.txt",
		ContentType: "text/plain",
		StorageURL:  "data/uploads/to-delete.txt",
		TextStatus:  models.MaterialTextStatusReady,
	}
	require.NoError(t, db.CreateMaterial(ctx, material))

	err = db.DeleteMaterial(ctx, material.ID)
	require.NoError(t, err)

	got, err := db.GetMaterialByID(ctx, material.ID)
	assert.Error(t, err)
	assert.Nil(t, got)
}

func TestUpdateMaterialTextStatusWithError(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)
	ctx := context.Background()

	session := createTestSession(t, db, "Test Session")
	artifact, err := db.CreateArtifact(ctx, session.ID, "Test Artifact", nil)
	require.NoError(t, err)

	material := &models.Material{
		ID:          uuid.New(),
		ArtifactID:  artifact.ID,
		SessionID:   session.ID,
		Kind:        "document",
		Filename:    "doc.txt",
		ContentType: "text/plain",
		StorageURL:  "data/uploads/doc.txt",
		TextStatus:  models.MaterialTextStatusPending,
	}
	require.NoError(t, db.CreateMaterial(ctx, material))

	errMsg := "PDF parse failed"
	err = db.UpdateMaterialTextStatusWithError(ctx, material.ID, models.MaterialTextStatusFailed, nil, &errMsg)
	require.NoError(t, err)

	got, err := db.GetMaterialByID(ctx, material.ID)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, models.MaterialTextStatusFailed, got.TextStatus)
	assert.Equal(t, errMsg, *got.ErrorMessage)
}

func TestGetMaterialsByArtifactID(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)

	ctx := context.Background()

	// Create a session and artifact
	session := createTestSession(t, db, "Test Session")
	artifact, err := db.CreateArtifact(ctx, session.ID, "Materials List Test", nil)
	require.NoError(t, err)

	t.Run("returns empty list for artifact with no materials", func(t *testing.T) {
		materials, err := db.GetMaterialsByArtifactID(ctx, artifact.ID)

		require.NoError(t, err)
		assert.Empty(t, materials)
	})

	t.Run("returns all materials for artifact", func(t *testing.T) {
		// Create multiple materials
		material1 := &models.Material{
			ID:          uuid.New(),
			ArtifactID:  artifact.ID,
			SessionID:   session.ID,
			Kind:        "document",
			Filename:    "doc1.txt",
			ContentType: "text/plain",
			StorageURL:  "data/uploads/doc1.txt",
			TextStatus:  models.MaterialTextStatusReady,
		}
		material2 := &models.Material{
			ID:          uuid.New(),
			ArtifactID:  artifact.ID,
			SessionID:   session.ID,
			Kind:        "slides",
			Filename:    "slides.pdf",
			ContentType: "application/pdf",
			StorageURL:  "data/uploads/slides.pdf",
			TextStatus:  models.MaterialTextStatusPending,
		}

		require.NoError(t, db.CreateMaterial(ctx, material1))
		require.NoError(t, db.CreateMaterial(ctx, material2))

		// Get materials
		materials, err := db.GetMaterialsByArtifactID(ctx, artifact.ID)

		require.NoError(t, err)
		assert.Len(t, materials, 2)
		assert.Equal(t, material1.Filename, materials[0].Filename)
		assert.Equal(t, material2.Filename, materials[1].Filename)
	})

	t.Run("does not return materials for other artifacts", func(t *testing.T) {
		// Create another session and artifact
		otherSession := createTestSession(t, db, "Other Session")
		otherArtifact, err := db.CreateArtifact(ctx, otherSession.ID, "Other Artifact", nil)
		require.NoError(t, err)

		// Create material for other artifact
		material := &models.Material{
			ID:          uuid.New(),
			ArtifactID:  otherArtifact.ID,
			SessionID:   otherSession.ID,
			Kind:        "document",
			Filename:    "other.txt",
			ContentType: "text/plain",
			StorageURL:  "data/uploads/other.txt",
			TextStatus:  models.MaterialTextStatusReady,
		}
		require.NoError(t, db.CreateMaterial(ctx, material))

		// Get materials for original artifact
		materials, err := db.GetMaterialsByArtifactID(ctx, artifact.ID)

		require.NoError(t, err)
		// Should still only have 2 materials from previous test
		assert.Len(t, materials, 2)
	})
}

// Helper function
func stringPtr(s string) *string {
	return &s
}

func containsString(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || 
		(len(s) > 0 && len(substr) > 0 && 
		(s[:len(substr)] == substr || containsString(s[1:], substr))))
}
