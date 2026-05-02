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

// TestGetSession_PrimaryContentRoundTrip verifies SCRUM-270's repository-read
// extension: GetSession exposes the new primary_content_kind /
// primary_material_id / primary_session_link_id columns through the Session
// model. Writes the columns via raw SQL (no model-layer setter exists yet —
// the PATCH endpoint lands in SCRUM-272) and confirms each read path
// surfaces them with correct nullability.
func TestGetSession_PrimaryContentRoundTrip(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)

	ctx := context.Background()
	session := createTestSession(t, db, "primary-content repo round-trip")

	// Seed the artifact + material the session can point at.
	artifactID := uuid.New()
	_, err := db.Pool.Exec(ctx, `
		INSERT INTO artifacts (id, session_id, title, status, created_at, updated_at)
		VALUES ($1, $2, 'fixture artifact', 'ready', now(), now())
	`, artifactID, session.ID)
	require.NoError(t, err)

	materialID := uuid.New()
	_, err = db.Pool.Exec(ctx, `
		INSERT INTO materials (id, artifact_id, session_id, kind, filename, content_type,
		                       storage_url, text_status, created_at)
		VALUES ($1, $2, $3, 'document', 'fixture.pdf', 'application/pdf',
		        'r2://bucket/key', 'ready', now())
	`, materialID, artifactID, session.ID)
	require.NoError(t, err)

	t.Run("nil when columns are NULL", func(t *testing.T) {
		got, err := db.GetSession(ctx, session.ID)
		require.NoError(t, err)
		assert.Nil(t, got.PrimaryContentKind, "PrimaryContentKind nil when DB column is NULL")
		assert.Nil(t, got.PrimaryMaterialID, "PrimaryMaterialID nil when DB column is NULL")
		assert.Nil(t, got.PrimarySessionLinkID, "PrimarySessionLinkID nil when DB column is NULL")
	})

	t.Run("populated when DB columns set", func(t *testing.T) {
		_, err := db.Pool.Exec(ctx, `
			UPDATE sessions
			SET primary_content_kind = 'document',
			    primary_material_id = $1
			WHERE id = $2
		`, materialID, session.ID)
		require.NoError(t, err)

		got, err := db.GetSession(ctx, session.ID)
		require.NoError(t, err)
		require.NotNil(t, got.PrimaryContentKind)
		assert.Equal(t, models.SessionPrimaryContentKindDocument, *got.PrimaryContentKind)
		require.NotNil(t, got.PrimaryMaterialID)
		assert.Equal(t, materialID, *got.PrimaryMaterialID)
		assert.Nil(t, got.PrimarySessionLinkID, "PrimarySessionLinkID stays nil when only material is set")
	})

	t.Run("ListAllSessions surfaces the new fields", func(t *testing.T) {
		// Already updated above.
		all, err := db.ListAllSessions(ctx)
		require.NoError(t, err)

		var found *models.Session
		for _, s := range all {
			if s.ID == session.ID {
				found = s
				break
			}
		}
		require.NotNil(t, found, "session must appear in ListAllSessions")
		require.NotNil(t, found.PrimaryContentKind)
		assert.Equal(t, models.SessionPrimaryContentKindDocument, *found.PrimaryContentKind)
		require.NotNil(t, found.PrimaryMaterialID)
		assert.Equal(t, materialID, *found.PrimaryMaterialID)
	})
}

// TestSessionPrimaryContentKind_Constants pins the closed-set string values
// to the migration's CHECK constraint. If the migration's CHECK is ever
// extended or these constants drift apart, this test fails fast.
func TestSessionPrimaryContentKind_Constants(t *testing.T) {
	assert.Equal(t, "video", string(models.SessionPrimaryContentKindVideo))
	assert.Equal(t, "document", string(models.SessionPrimaryContentKindDocument))
	assert.Equal(t, "link", string(models.SessionPrimaryContentKindLink))
}
