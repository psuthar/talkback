package database

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/models"
	"github.com/psuthar/talkback/internal/test"
	"github.com/stretchr/testify/require"
)

func TestListSessionsMatchingDecisionTopic_emptyAccessibleIDs(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)
	ctx := context.Background()

	rows, err := db.ListSessionsMatchingDecisionTopic(ctx, nil, "anything", 10)
	require.NoError(t, err)
	require.Nil(t, rows)

	rows, err = db.ListSessionsMatchingDecisionTopic(ctx, []uuid.UUID{}, "anything", 10)
	require.NoError(t, err)
	require.Nil(t, rows)
}

func TestListSessionsMatchingDecisionTopic_respectsAccessibleFilter(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)
	ctx := context.Background()

	email := "u-" + uuid.New().String() + "@example.com"
	_ = createTestUser(t, db, email, models.GlobalRoleCreator)
	in := createSessionWithCreator(t, db, "In scope", email)
	out := createSessionWithCreator(t, db, "Out of scope", email)
	premise := "quarterly planning pivot"
	require.NoError(t, db.UpdateSessionContext(ctx, in.ID, &premise, nil, nil))
	require.NoError(t, db.UpdateSessionContext(ctx, out.ID, &premise, nil, nil))

	rows, err := db.ListSessionsMatchingDecisionTopic(ctx, []uuid.UUID{in.ID}, "pivot", 40)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, in.ID, rows[0].SessionID)

	rows, err = db.ListSessionsMatchingDecisionTopic(ctx, []uuid.UUID{out.ID}, "pivot", 40)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, out.ID, rows[0].SessionID)
}

func TestListSessionsMatchingDecisionTopic_matchesPrimaryDecisionAndOutcome(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)
	ctx := context.Background()

	email := "u-" + uuid.New().String() + "@example.com"
	_ = createTestUser(t, db, email, models.GlobalRoleCreator)
	s1 := createSessionWithCreator(t, db, "S1", email)
	pd := "We will migrate the database"
	require.NoError(t, db.UpdateSessionContext(ctx, s1.ID, nil, &pd, nil))

	s2 := createSessionWithCreator(t, db, "S2", email)
	out := "Outcome: migration completed"
	require.NoError(t, db.UpdateSessionContext(ctx, s2.ID, nil, nil, &out))

	r1, err := db.ListSessionsMatchingDecisionTopic(ctx, []uuid.UUID{s1.ID}, "migrate", 40)
	require.NoError(t, err)
	require.Len(t, r1, 1)

	r2, err := db.ListSessionsMatchingDecisionTopic(ctx, []uuid.UUID{s2.ID}, "migration", 40)
	require.NoError(t, err)
	require.Len(t, r2, 1)
}

func TestListSessionsMatchingDecisionTopic_caseInsensitive(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)
	ctx := context.Background()

	email := "u-" + uuid.New().String() + "@example.com"
	_ = createTestUser(t, db, email, models.GlobalRoleCreator)
	s := createSessionWithCreator(t, db, "Case", email)
	premise := "Lowercase Topic Match"
	require.NoError(t, db.UpdateSessionContext(ctx, s.ID, &premise, nil, nil))

	rows, err := db.ListSessionsMatchingDecisionTopic(ctx, []uuid.UUID{s.ID}, "topic", 40)
	require.NoError(t, err)
	require.Len(t, rows, 1)
}

func TestListSessionsMatchingDecisionTopic_limit(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)
	ctx := context.Background()

	email := "u-" + uuid.New().String() + "@example.com"
	_ = createTestUser(t, db, email, models.GlobalRoleCreator)
	s1 := createSessionWithCreator(t, db, "Older", email)
	s2 := createSessionWithCreator(t, db, "Newer", email)
	topic := "sharedkeyword"
	require.NoError(t, db.UpdateSessionContext(ctx, s1.ID, &topic, nil, nil))
	require.NoError(t, db.UpdateSessionContext(ctx, s2.ID, &topic, nil, nil))
	// Bump s2.updated_at so it sorts first
	require.NoError(t, db.UpdateSessionTitle(ctx, s2.ID, "Newer renamed"))

	rows, err := db.ListSessionsMatchingDecisionTopic(ctx, []uuid.UUID{s1.ID, s2.ID}, "sharedkeyword", 1)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, s2.ID, rows[0].SessionID)
}

func TestListSessionsMatchingDecisionTopic_stanceCount(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)
	ctx := context.Background()

	email := "u-" + uuid.New().String() + "@example.com"
	u := createTestUser(t, db, email, models.GlobalRoleCreator)
	s := createSessionWithCreator(t, db, "Stances", email)
	premise := "stance topic here"
	require.NoError(t, db.UpdateSessionContext(ctx, s.ID, &premise, nil, nil))

	u2 := createTestUser(t, db, "voter-"+uuid.New().String()+"@example.com", models.GlobalRoleCreator)
	_, err := db.CreateOrUpdateStance(ctx, s.ID, u.ID, "agree", nil)
	require.NoError(t, err)
	_, err = db.CreateOrUpdateStance(ctx, s.ID, u2.ID, "disagree", nil)
	require.NoError(t, err)

	rows, err := db.ListSessionsMatchingDecisionTopic(ctx, []uuid.UUID{s.ID}, "stance topic", 40)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, 2, rows[0].StanceCount)
}
