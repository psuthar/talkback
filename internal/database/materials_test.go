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

	// Create an artifact first
	artifact, err := db.CreateArtifact(ctx, "Material Test Artifact", nil)
	require.NoError(t, err)

	t.Run("creates material with all fields", func(t *testing.T) {
		material := &models.Material{
			ID:            uuid.New(),
			ArtifactID:    artifact.ID,
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
			Kind:        "slides",
			Filename:    "presentation.pdf",
			ContentType: "application/pdf",
			StorageURL:  "data/uploads/presentation.pdf",
			TextStatus:  models.MaterialTextStatusPending,
		}

		err := db.CreateMaterial(ctx, material)
		require.NoError(t, err)
	})
}

func TestGetMaterialsByArtifactID(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)

	ctx := context.Background()

	// Create an artifact
	artifact, err := db.CreateArtifact(ctx, "Materials List Test", nil)
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
			Kind:        "document",
			Filename:    "doc1.txt",
			ContentType: "text/plain",
			StorageURL:  "data/uploads/doc1.txt",
			TextStatus:  models.MaterialTextStatusReady,
		}
		material2 := &models.Material{
			ID:          uuid.New(),
			ArtifactID:  artifact.ID,
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
		// Create another artifact
		otherArtifact, err := db.CreateArtifact(ctx, "Other Artifact", nil)
		require.NoError(t, err)

		// Create material for other artifact
		material := &models.Material{
			ID:          uuid.New(),
			ArtifactID:  otherArtifact.ID,
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
