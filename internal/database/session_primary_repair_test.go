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

// TestSoftDeleteMaterial_RepairsSessionPrimary_SCRUM282 confirms that
// soft-deleting a material that's the session's primary clears
// primary_content_kind + primary_material_id in the same transaction so the
// resolver doesn't surface a tombstoned material as primary.
func TestSoftDeleteMaterial_RepairsSessionPrimary_SCRUM282(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)

	ctx := context.Background()
	session := createTestSession(t, db, "soft-delete primary repair")

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

	_, err = db.Pool.Exec(ctx, `
		UPDATE sessions SET primary_content_kind = 'document', primary_material_id = $1
		WHERE id = $2
	`, materialID, session.ID)
	require.NoError(t, err)

	require.NoError(t, db.SoftDeleteMaterial(ctx, materialID))

	got, err := db.GetSession(ctx, session.ID)
	require.NoError(t, err)
	assert.Nil(t, got.PrimaryContentKind, "kind cleared after soft-delete of primary material")
	assert.Nil(t, got.PrimaryMaterialID, "pointer cleared after soft-delete of primary material")

	// Material row itself is soft-deleted but still exists.
	m, err := db.GetMaterialByID(ctx, materialID)
	require.NoError(t, err)
	require.NotNil(t, m)
	assert.NotNil(t, m.DeletedAt, "material is soft-deleted (tombstone)")
}

// TestSoftDeleteMaterial_NonPrimaryUntouched_SCRUM282 confirms that soft-
// deleting a non-primary material does NOT alter the session's primary
// fields (regression guard against an over-broad UPDATE WHERE clause).
func TestSoftDeleteMaterial_NonPrimaryUntouched_SCRUM282(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)

	ctx := context.Background()
	session := createTestSession(t, db, "soft-delete non-primary")

	artifactID := uuid.New()
	_, err := db.Pool.Exec(ctx, `
		INSERT INTO artifacts (id, session_id, title, status, created_at, updated_at)
		VALUES ($1, $2, 'fixture artifact', 'ready', now(), now())
	`, artifactID, session.ID)
	require.NoError(t, err)

	primaryMaterialID := uuid.New()
	otherMaterialID := uuid.New()
	for _, mid := range []uuid.UUID{primaryMaterialID, otherMaterialID} {
		_, err := db.Pool.Exec(ctx, `
			INSERT INTO materials (id, artifact_id, session_id, kind, filename, content_type,
			                       storage_url, text_status, created_at)
			VALUES ($1, $2, $3, 'document', 'fixture.pdf', 'application/pdf',
			        'r2://bucket/key', 'ready', now())
		`, mid, artifactID, session.ID)
		require.NoError(t, err)
	}

	_, err = db.Pool.Exec(ctx, `
		UPDATE sessions SET primary_content_kind = 'document', primary_material_id = $1
		WHERE id = $2
	`, primaryMaterialID, session.ID)
	require.NoError(t, err)

	// Soft-delete the OTHER (non-primary) material.
	require.NoError(t, db.SoftDeleteMaterial(ctx, otherMaterialID))

	got, err := db.GetSession(ctx, session.ID)
	require.NoError(t, err)
	require.NotNil(t, got.PrimaryContentKind)
	assert.Equal(t, models.SessionPrimaryContentKindDocument, *got.PrimaryContentKind)
	require.NotNil(t, got.PrimaryMaterialID)
	assert.Equal(t, primaryMaterialID, *got.PrimaryMaterialID, "primary unaffected by non-primary delete")
}

// TestDeleteMaterial_RepairsSessionPrimary_SCRUM282 confirms hard delete
// clears both pointer and kind atomically. The FK is ON DELETE SET NULL
// (SCRUM-269 migration), so the pointer would already nil out — this test
// guards the kind clearing that prevents the silent-empty (kind='document'
// + pointer=NULL) state.
func TestDeleteMaterial_RepairsSessionPrimary_SCRUM282(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)

	ctx := context.Background()
	session := createTestSession(t, db, "hard-delete primary repair")

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

	_, err = db.Pool.Exec(ctx, `
		UPDATE sessions SET primary_content_kind = 'document', primary_material_id = $1
		WHERE id = $2
	`, materialID, session.ID)
	require.NoError(t, err)

	require.NoError(t, db.DeleteMaterial(ctx, materialID))

	got, err := db.GetSession(ctx, session.ID)
	require.NoError(t, err)
	assert.Nil(t, got.PrimaryContentKind, "kind cleared after hard-delete of primary material")
	assert.Nil(t, got.PrimaryMaterialID, "pointer cleared after hard-delete of primary material")
}

// TestDeleteSessionLink_RepairsSessionPrimary_SCRUM282 confirms link delete
// clears both kind and pointer atomically.
func TestDeleteSessionLink_RepairsSessionPrimary_SCRUM282(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)

	ctx := context.Background()
	session := createTestSession(t, db, "link primary repair")

	linkTitle := "Spec link"
	link := &models.SessionLink{
		ID:        uuid.New(),
		SessionID: session.ID,
		URL:       "https://example.com/spec",
		Title:     &linkTitle,
		Status:    models.SessionLinkStatusVerified,
	}
	require.NoError(t, db.CreateSessionLink(ctx, link))

	_, err := db.Pool.Exec(ctx, `
		UPDATE sessions SET primary_content_kind = 'link', primary_session_link_id = $1
		WHERE id = $2
	`, link.ID, session.ID)
	require.NoError(t, err)

	require.NoError(t, db.DeleteSessionLink(ctx, link.ID))

	got, err := db.GetSession(ctx, session.ID)
	require.NoError(t, err)
	assert.Nil(t, got.PrimaryContentKind, "kind cleared after link delete")
	assert.Nil(t, got.PrimarySessionLinkID, "pointer cleared after link delete")
}

// TestDeleteSessionLink_NonPrimaryUntouched_SCRUM282 confirms deleting a
// non-primary link does not alter the session's primary fields.
func TestDeleteSessionLink_NonPrimaryUntouched_SCRUM282(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)

	ctx := context.Background()
	session := createTestSession(t, db, "non-primary link delete")

	primaryLinkTitle := "Primary"
	primaryLink := &models.SessionLink{
		ID:        uuid.New(),
		SessionID: session.ID,
		URL:       "https://example.com/primary",
		Title:     &primaryLinkTitle,
		Status:    models.SessionLinkStatusVerified,
	}
	require.NoError(t, db.CreateSessionLink(ctx, primaryLink))

	otherLinkTitle := "Other"
	otherLink := &models.SessionLink{
		ID:        uuid.New(),
		SessionID: session.ID,
		URL:       "https://example.com/other",
		Title:     &otherLinkTitle,
		Status:    models.SessionLinkStatusVerified,
	}
	require.NoError(t, db.CreateSessionLink(ctx, otherLink))

	_, err := db.Pool.Exec(ctx, `
		UPDATE sessions SET primary_content_kind = 'link', primary_session_link_id = $1
		WHERE id = $2
	`, primaryLink.ID, session.ID)
	require.NoError(t, err)

	require.NoError(t, db.DeleteSessionLink(ctx, otherLink.ID))

	got, err := db.GetSession(ctx, session.ID)
	require.NoError(t, err)
	require.NotNil(t, got.PrimaryContentKind)
	assert.Equal(t, models.SessionPrimaryContentKindLink, *got.PrimaryContentKind)
	require.NotNil(t, got.PrimarySessionLinkID)
	assert.Equal(t, primaryLink.ID, *got.PrimarySessionLinkID)
}
