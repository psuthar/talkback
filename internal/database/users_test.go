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

// TestUserCanAccessSession_SCRUM228_MatrixCoverage pins the SCRUM-228 contract:
// access flows entirely through session_memberships now that creator
// memberships are universal (SCRUM-226) and orphan creators are anonymised
// (SCRUM-229). The legacy email-match fallback that previously short-circuited
// access by string-comparing session.CreatedBy to user.Email is gone.
func TestUserCanAccessSession_SCRUM228_MatrixCoverage(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)
	ctx := context.Background()

	creatorEmail := "scrum228-creator-" + uuid.New().String() + "@example.com"
	creator := createTestUser(t, db, creatorEmail, models.GlobalRoleCreator)
	sess := createSessionWithCreator(t, db, "SCRUM-228 access matrix", creatorEmail)

	// Original creator has a creator membership thanks to SCRUM-226's
	// transactional CreateSession; UserCanAccessSession must return true.
	t.Run("original_creator_with_membership_returns_true", func(t *testing.T) {
		ok, err := db.UserCanAccessSession(ctx, sess.ID, creator)
		require.NoError(t, err)
		require.True(t, ok, "creator-by-membership must access their own session")
	})

	// Plain participant member.
	plainEmail := "scrum228-plain-" + uuid.New().String() + "@example.com"
	plain := createTestUser(t, db, plainEmail, models.GlobalRoleParticipant)
	require.NoError(t, db.CreateSessionMembership(ctx, sess.ID, plain.ID, "participant", &creator.ID))
	t.Run("participant_member_returns_true", func(t *testing.T) {
		ok, err := db.UserCanAccessSession(ctx, sess.ID, plain)
		require.NoError(t, err)
		require.True(t, ok, "participant member must access the session")
	})

	// Decision-maker member.
	dmEmail := "scrum228-dm-" + uuid.New().String() + "@example.com"
	dm := createTestUser(t, db, dmEmail, models.GlobalRoleParticipant)
	require.NoError(t, db.CreateSessionMembership(ctx, sess.ID, dm.ID, "decision_maker", &creator.ID))
	t.Run("decision_maker_member_returns_true", func(t *testing.T) {
		ok, err := db.UserCanAccessSession(ctx, sess.ID, dm)
		require.NoError(t, err)
		require.True(t, ok, "decision_maker member must access the session")
	})

	// Outsider with no membership.
	outsiderEmail := "scrum228-outsider-" + uuid.New().String() + "@example.com"
	outsider := createTestUser(t, db, outsiderEmail, models.GlobalRoleCreator)
	t.Run("non_member_returns_false", func(t *testing.T) {
		ok, err := db.UserCanAccessSession(ctx, sess.ID, outsider)
		require.NoError(t, err)
		require.False(t, ok, "non-member must not access the session")
	})

	// Global admin who is not a member of this session: admin gets no free
	// pass at the read-gate layer (handler-level admin shortcuts apply
	// elsewhere). This pins behaviour parity with pre-SCRUM-228 — the email
	// fallback never granted admin access either.
	adminEmail := "scrum228-admin-" + uuid.New().String() + "@example.com"
	admin := createTestUser(t, db, adminEmail, models.GlobalRoleAdmin)
	t.Run("global_admin_non_member_returns_false", func(t *testing.T) {
		ok, err := db.UserCanAccessSession(ctx, sess.ID, admin)
		require.NoError(t, err)
		require.False(t, ok, "global admin without a membership must rely on handler-level admin checks, not this read gate")
	})

	// Nil user is rejected.
	t.Run("nil_user_returns_false", func(t *testing.T) {
		ok, err := db.UserCanAccessSession(ctx, sess.ID, nil)
		require.NoError(t, err)
		require.False(t, ok, "nil user must not access any session")
	})

	// Pretend an attacker spoofs an old-style email-only access path: even if
	// session.CreatedBy still carries the original email after a backfill drift,
	// access is decided purely on session_memberships. We forcibly NULL out
	// created_by here (mirroring SCRUM-229's anonymise-on-delete behaviour) and
	// confirm the existing creator membership still grants access.
	t.Run("anonymised_created_by_does_not_change_member_access", func(t *testing.T) {
		_, err := db.Pool.Exec(ctx, `UPDATE sessions SET created_by = NULL WHERE id = $1`, sess.ID)
		require.NoError(t, err)
		ok, err := db.UserCanAccessSession(ctx, sess.ID, creator)
		require.NoError(t, err)
		require.True(t, ok, "creator with a membership row keeps access even when created_by is NULL")
	})
}

// TestGetUsersByIDs_BatchLookup pins the SCRUM-506 contract: GetUsersByIDs
// returns a map of id -> User for the given IDs in a single batched query,
// eliminating the N+1 GetUserByID-in-a-loop pattern that caused the
// /api/invitations/ p95 regression (341–444 ms observed in obs#158, obs#219).
func TestGetUsersByIDs_BatchLookup(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)
	ctx := context.Background()

	// Create three users.
	users := make([]*models.User, 3)
	for i := range users {
		u := &models.User{
			ID:          uuid.New(),
			Email:       "scrum506-batch-" + uuid.New().String() + "@example.com",
			DisplayName: "Batch User",
			Status:      models.UserStatusActive,
			GlobalRole:  models.GlobalRoleParticipant,
		}
		require.NoError(t, db.CreateUser(ctx, u))
		users[i] = u
	}

	// Include one missing UUID alongside the real ones.
	missing := uuid.New()
	ids := []uuid.UUID{users[0].ID, missing, users[1].ID, users[2].ID}

	result, err := db.GetUsersByIDs(ctx, ids)
	require.NoError(t, err)
	assert.Len(t, result, 3, "missing UUID is absent from result; existing users all present")
	for _, u := range users {
		got, ok := result[u.ID]
		require.True(t, ok, "user %s missing from batch result", u.ID)
		assert.Equal(t, u.Email, got.Email)
		assert.Equal(t, u.DisplayName, got.DisplayName)
	}
	_, ok := result[missing]
	assert.False(t, ok, "missing UUID must not appear in result")
}

// TestGetUsersByIDs_EmptyInput pins the SCRUM-506 contract: GetUsersByIDs
// returns an empty map and issues no query on empty input. Hot-path callers
// (ListInvitations on a session with zero invitations + zero memberships)
// rely on this to avoid a wasted round trip.
func TestGetUsersByIDs_EmptyInput(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)
	ctx := context.Background()

	result, err := db.GetUsersByIDs(ctx, nil)
	require.NoError(t, err)
	assert.Empty(t, result)

	result, err = db.GetUsersByIDs(ctx, []uuid.UUID{})
	require.NoError(t, err)
	assert.Empty(t, result)
}
