package database

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSessionPrimaryContentMigration verifies the SCRUM-269 schema additions:
// the three new columns exist on `sessions`, the CHECK constraint on
// primary_content_kind enforces the closed set, and the FK pointers behave
// correctly under their declared ON DELETE rules.
//
// SCRUM-270 will wire these columns into the Session model + repository
// reads; this test exercises them via raw SQL because no model accessor
// exists yet, and confirms the migration is functionally correct as
// shipped.
func TestSessionPrimaryContentMigration(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)

	ctx := context.Background()
	session := createTestSession(t, db, "primary-content session")

	t.Run("columns exist and default to NULL", func(t *testing.T) {
		var (
			kind         *string
			materialID   *uuid.UUID
			sessionLinkID *uuid.UUID
		)
		err := db.Pool.QueryRow(ctx, `
			SELECT primary_content_kind, primary_material_id, primary_session_link_id
			FROM sessions WHERE id = $1
		`, session.ID).Scan(&kind, &materialID, &sessionLinkID)
		require.NoError(t, err)
		assert.Nil(t, kind, "primary_content_kind defaults to NULL on new sessions")
		assert.Nil(t, materialID, "primary_material_id defaults to NULL")
		assert.Nil(t, sessionLinkID, "primary_session_link_id defaults to NULL")
	})

	t.Run("primary_content_kind CHECK accepts video/document/link", func(t *testing.T) {
		for _, k := range []string{"video", "document", "link"} {
			_, err := db.Pool.Exec(ctx,
				`UPDATE sessions SET primary_content_kind = $1 WHERE id = $2`, k, session.ID)
			require.NoErrorf(t, err, "kind %q must satisfy the CHECK constraint", k)
		}
	})

	t.Run("primary_content_kind CHECK rejects unknown kinds", func(t *testing.T) {
		_, err := db.Pool.Exec(ctx,
			`UPDATE sessions SET primary_content_kind = 'unknown-kind' WHERE id = $1`, session.ID)
		require.Error(t, err, "unknown kind must violate the CHECK constraint")
	})

	t.Run("primary_material_id ON DELETE SET NULL", func(t *testing.T) {
		// Create an artifact + material owned by the session, point primary at it,
		// delete the material, and confirm the pointer goes to NULL (not cascade).
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

		_, err = db.Pool.Exec(ctx,
			`UPDATE sessions SET primary_material_id = $1, primary_content_kind = 'document' WHERE id = $2`,
			materialID, session.ID)
		require.NoError(t, err)

		_, err = db.Pool.Exec(ctx, `DELETE FROM materials WHERE id = $1`, materialID)
		require.NoError(t, err)

		var got *uuid.UUID
		err = db.Pool.QueryRow(ctx,
			`SELECT primary_material_id FROM sessions WHERE id = $1`, session.ID).Scan(&got)
		require.NoError(t, err)
		assert.Nil(t, got, "primary_material_id must SET NULL on referenced material delete")
	})
}
