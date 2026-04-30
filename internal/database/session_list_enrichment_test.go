package database

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/models"
	"github.com/psuthar/talkback/internal/test"
	"github.com/stretchr/testify/require"
)

// SCRUM-221: enrichment tests for ListSessionsWithRolesForUser, exercising the
// member_count + created_by_display_name fields populated after the three-branch
// switch.

func TestListSessionsWithRolesForUser_MemberCount_Zero(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)
	ctx := context.Background()

	email := "creator-" + uuid.New().String() + "@example.com"
	creator := createTestUser(t, db, email, models.GlobalRoleCreator)
	_ = createSessionWithCreator(t, db, "Empty session", email)

	out, err := db.ListSessionsWithRolesForUser(ctx, creator)
	require.NoError(t, err)
	require.Len(t, out, 1)
	require.Equal(t, 0, out[0].MemberCount, "expected zero memberships")
}

func TestListSessionsWithRolesForUser_MemberCount_Multi(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)
	ctx := context.Background()

	email := "creator-" + uuid.New().String() + "@example.com"
	creator := createTestUser(t, db, email, models.GlobalRoleCreator)
	s := createSessionWithCreator(t, db, "Populated session", email)

	for i := 0; i < 3; i++ {
		memberEmail := "m" + uuid.New().String() + "@example.com"
		m := createTestUser(t, db, memberEmail, models.GlobalRoleCreator)
		require.NoError(t, db.CreateSessionMembership(ctx, s.ID, m.ID, "participant", nil))
	}

	out, err := db.ListSessionsWithRolesForUser(ctx, creator)
	require.NoError(t, err)
	require.Len(t, out, 1)
	require.Equal(t, 3, out[0].MemberCount, "expected three memberships")
}

func TestListSessionsWithRolesForUser_CreatorDisplayName_Resolved(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)
	ctx := context.Background()

	email := "creator-" + uuid.New().String() + "@example.com"
	creator := createTestUser(t, db, email, models.GlobalRoleCreator)
	require.NoError(t, db.SetUserDisplayName(ctx, creator.ID, "Alice Wonder"))
	_ = createSessionWithCreator(t, db, "Has display name", email)

	out, err := db.ListSessionsWithRolesForUser(ctx, creator)
	require.NoError(t, err)
	require.Len(t, out, 1)
	require.NotNil(t, out[0].CreatedByDisplayName, "expected display name to be populated")
	require.Equal(t, "Alice Wonder", *out[0].CreatedByDisplayName)
}

func TestListSessionsWithRolesForUser_CreatorDisplayName_Missing(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)
	ctx := context.Background()

	// Session with a created_by email that has no matching users row.
	orphanEmail := "orphan-" + uuid.New().String() + "@example.com"
	_ = createSessionWithCreator(t, db, "Orphan creator", orphanEmail)

	// View as a global admin so the session shows up regardless of creator user existence.
	adminEmail := "admin-" + uuid.New().String() + "@example.com"
	admin := createTestUser(t, db, adminEmail, models.GlobalRoleAdmin)

	out, err := db.ListSessionsWithRolesForUser(ctx, admin)
	require.NoError(t, err)
	var found bool
	for _, row := range out {
		if row.Session != nil && row.Session.CreatedBy != nil && *row.Session.CreatedBy == orphanEmail {
			require.Nil(t, row.CreatedByDisplayName, "expected nil display name for orphan creator email")
			found = true
		}
	}
	require.True(t, found, "expected orphan-created session in admin list")
}

func TestListSessionsWithRolesForUser_ViewerIsCreator_Enrichment(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)
	ctx := context.Background()

	email := "creator-" + uuid.New().String() + "@example.com"
	creator := createTestUser(t, db, email, models.GlobalRoleCreator)
	require.NoError(t, db.SetUserDisplayName(ctx, creator.ID, "Bob Boss"))
	s := createSessionWithCreator(t, db, "Owned by viewer", email)

	// Two memberships to also exercise the count.
	for i := 0; i < 2; i++ {
		memberEmail := "member-" + uuid.New().String() + "@example.com"
		m := createTestUser(t, db, memberEmail, models.GlobalRoleCreator)
		require.NoError(t, db.CreateSessionMembership(ctx, s.ID, m.ID, "participant", nil))
	}

	out, err := db.ListSessionsWithRolesForUser(ctx, creator)
	require.NoError(t, err)
	require.Len(t, out, 1)
	require.Equal(t, "creator", out[0].MyRole)
	require.Equal(t, 2, out[0].MemberCount)
	require.NotNil(t, out[0].CreatedByDisplayName)
	require.Equal(t, "Bob Boss", *out[0].CreatedByDisplayName)
}

func TestCountMembersBySessionIDs_EmptyInput(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)
	ctx := context.Background()

	counts, err := db.CountMembersBySessionIDs(ctx, nil)
	require.NoError(t, err)
	require.Empty(t, counts)
}

func TestCountMembersBySessionIDs_NonexistentIDs(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)
	ctx := context.Background()

	counts, err := db.CountMembersBySessionIDs(ctx, []uuid.UUID{uuid.New(), uuid.New()})
	require.NoError(t, err)
	require.Empty(t, counts)
}

func TestGetDisplayNamesByEmails_MixedCaseAndMissing(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)
	ctx := context.Background()

	email := "Mixed-" + uuid.New().String() + "@Example.com"
	u := createTestUser(t, db, email, models.GlobalRoleCreator)
	require.NoError(t, db.SetUserDisplayName(ctx, u.ID, "Mixed Case User"))

	nonexistent := "nobody-" + uuid.New().String() + "@example.com"

	names, err := db.GetDisplayNamesByEmails(ctx, []string{strings.ToUpper(email), nonexistent})
	require.NoError(t, err)
	require.Equal(t, "Mixed Case User", names[strings.ToLower(email)], "case-insensitive match should resolve")
	_, present := names[strings.ToLower(nonexistent)]
	require.False(t, present, "nonexistent email should be absent from result map")
}

func TestGetDisplayNamesByEmails_EmptyInput(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)
	ctx := context.Background()

	names, err := db.GetDisplayNamesByEmails(ctx, nil)
	require.NoError(t, err)
	require.Empty(t, names)
}
