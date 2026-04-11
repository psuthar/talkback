package database

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/models"
	"github.com/psuthar/talkback/internal/test"
	"github.com/stretchr/testify/require"
)

func TestListAccessibleSessionIDsForUser_nilUser(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)
	ctx := context.Background()

	_, err := db.ListAccessibleSessionIDsForUser(ctx, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "nil user")
}

func TestListAccessibleSessionIDsForUser_creatorEmail(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)
	ctx := context.Background()

	email := "creator-" + uuid.New().String() + "@example.com"
	u := createTestUser(t, db, email, models.GlobalRoleCreator)
	s := createSessionWithCreator(t, db, "Owned session", email)

	ids, err := db.ListAccessibleSessionIDsForUser(ctx, u)
	require.NoError(t, err)
	require.Len(t, ids, 1)
	require.Equal(t, s.ID, ids[0])
}

func TestListAccessibleSessionIDsForUser_sessionMembership(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)
	ctx := context.Background()

	ownerEmail := "owner-" + uuid.New().String() + "@example.com"
	_ = createTestUser(t, db, ownerEmail, models.GlobalRoleCreator)
	s := createSessionWithCreator(t, db, "Shared session", ownerEmail)

	memberEmail := "member-" + uuid.New().String() + "@example.com"
	member := createTestUser(t, db, memberEmail, models.GlobalRoleCreator)
	err := db.CreateSessionMembership(ctx, s.ID, member.ID, "participant", nil)
	require.NoError(t, err)

	ids, err := db.ListAccessibleSessionIDsForUser(ctx, member)
	require.NoError(t, err)
	require.Len(t, ids, 1)
	require.Equal(t, s.ID, ids[0])
}

func TestListAccessibleSessionIDsForUser_excludesUnrelatedSessions(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)
	ctx := context.Background()

	aliceEmail := "alice-" + uuid.New().String() + "@example.com"
	bobEmail := "bob-" + uuid.New().String() + "@example.com"
	_ = createTestUser(t, db, aliceEmail, models.GlobalRoleCreator)
	bob := createTestUser(t, db, bobEmail, models.GlobalRoleCreator)
	sAlice := createSessionWithCreator(t, db, "Alice only", aliceEmail)

	ids, err := db.ListAccessibleSessionIDsForUser(ctx, bob)
	require.NoError(t, err)
	require.Empty(t, ids)

	// Sanity: Alice still sees her session
	alice, err := db.GetUserByEmail(ctx, aliceEmail)
	require.NoError(t, err)
	require.NotNil(t, alice)
	ids, err = db.ListAccessibleSessionIDsForUser(ctx, alice)
	require.NoError(t, err)
	require.Len(t, ids, 1)
	require.Equal(t, sAlice.ID, ids[0])
}

func TestListAccessibleSessionIDsForUser_globalAdminSeesSessions(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)
	ctx := context.Background()

	otherEmail := "other-" + uuid.New().String() + "@example.com"
	_ = createTestUser(t, db, otherEmail, models.GlobalRoleCreator)
	s1 := createSessionWithCreator(t, db, "Admin visible 1", otherEmail)
	s2 := createSessionWithCreator(t, db, "Admin visible 2", otherEmail)

	adminEmail := "admin-" + uuid.New().String() + "@example.com"
	admin := createTestUser(t, db, adminEmail, models.GlobalRoleAdmin)

	ids, err := db.ListAccessibleSessionIDsForUser(ctx, admin)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(ids), 2)
	seen := make(map[uuid.UUID]struct{})
	for _, id := range ids {
		seen[id] = struct{}{}
	}
	_, ok1 := seen[s1.ID]
	_, ok2 := seen[s2.ID]
	require.True(t, ok1, "admin list should include s1")
	require.True(t, ok2, "admin list should include s2")
}
