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

// TestDeleteUser_AnonymisesSessionsCreatedBy pins the SCRUM-229 contract:
// deleting a user must NULL out sessions.created_by for sessions that user
// owned, in the same transaction. This eliminates the orphan-creator state
// (sessions whose created_by points to a deleted users row) that previously
// kept the email-match fallback in UserCanAccessSession load-bearing.
func TestDeleteUser_AnonymisesSessionsCreatedBy(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)
	ctx := context.Background()

	creatorEmail := "scrum229-creator-" + uuid.New().String() + "@example.com"
	creator := createTestUser(t, db, creatorEmail, models.GlobalRoleCreator)
	sess := createSessionWithCreator(t, db, "SCRUM-229 owned session", creatorEmail)

	// Sanity: created_by is set to the creator's email pre-delete.
	pre, err := db.GetSession(ctx, sess.ID)
	require.NoError(t, err)
	require.NotNil(t, pre.CreatedBy)
	require.Equal(t, creatorEmail, *pre.CreatedBy)

	require.NoError(t, db.DeleteUser(ctx, creator.ID))

	// Session row must still exist; created_by must be NULL after delete.
	post, err := db.GetSession(ctx, sess.ID)
	require.NoError(t, err)
	assert.Nil(t, post.CreatedBy, "sessions.created_by must be NULL after the creator's user row is deleted")

	// User row must be gone.
	u, err := db.GetUserByID(ctx, creator.ID)
	require.NoError(t, err)
	assert.Nil(t, u, "users row must be removed by DeleteUser")
}

// TestDeleteUser_OrphanCreatorQueryReturnsZeroAfterDelete asserts the
// invariant SCRUM-228 wants to drop the email-match fallback against:
// after a user-delete, the verification query that previously found
// orphans returns no rows for that user's sessions.
func TestDeleteUser_OrphanCreatorQueryReturnsZeroAfterDelete(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)
	ctx := context.Background()

	creatorEmail := "scrum229-orphan-check-" + uuid.New().String() + "@example.com"
	creator := createTestUser(t, db, creatorEmail, models.GlobalRoleCreator)
	_ = createSessionWithCreator(t, db, "SCRUM-229 orphan-check session", creatorEmail)

	require.NoError(t, db.DeleteUser(ctx, creator.ID))

	// Verification query mirrored from SCRUM-228's pre-merge check. After
	// SCRUM-229's anonymisation it must return zero rows for the test fixture.
	var orphanCount int
	require.NoError(t, db.Pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM sessions s
		LEFT JOIN users u ON u.email = s.created_by
		WHERE s.created_by IS NOT NULL AND u.id IS NULL
	`).Scan(&orphanCount))
	assert.Equal(t, 0, orphanCount, "no orphan-creator sessions should remain after DeleteUser")
}

// TestDeleteUser_OnlyAffectsTargetCreator asserts that anonymisation is
// scoped to the deleted user's sessions only — other users' sessions keep
// their created_by intact.
func TestDeleteUser_OnlyAffectsTargetCreator(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)
	ctx := context.Background()

	emailA := "scrum229-a-" + uuid.New().String() + "@example.com"
	emailB := "scrum229-b-" + uuid.New().String() + "@example.com"
	userA := createTestUser(t, db, emailA, models.GlobalRoleCreator)
	_ = createTestUser(t, db, emailB, models.GlobalRoleCreator)
	_ = createSessionWithCreator(t, db, "Owned by A", emailA)
	sessB := createSessionWithCreator(t, db, "Owned by B", emailB)

	require.NoError(t, db.DeleteUser(ctx, userA.ID))

	// userA's session has been anonymised; userB's session is untouched.
	postB, err := db.GetSession(ctx, sessB.ID)
	require.NoError(t, err)
	require.NotNil(t, postB.CreatedBy)
	assert.Equal(t, emailB, *postB.CreatedBy, "DeleteUser must not affect other users' sessions")
}

// TestDeleteUser_NoUserIsIdempotent asserts deleting a non-existent user
// is a no-op — important because admin teardowns can race or retry.
func TestDeleteUser_NoUserIsIdempotent(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)
	ctx := context.Background()

	// Brand-new uuid that has no users row.
	require.NoError(t, db.DeleteUser(ctx, uuid.New()), "deleting a non-existent user must be idempotent")
}
