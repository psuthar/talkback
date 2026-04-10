package mcpserver

import (
	"context"
	"testing"

	"github.com/psuthar/talkback/internal/models"
	"github.com/psuthar/talkback/internal/test"
	"github.com/stretchr/testify/require"
)

func TestUserMayReadSessionMCP_DBParticipantAllows(t *testing.T) {
	ctx := context.Background()
	db := setupMCPTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)

	owner := test.MakeUser(t, db, "owner-mcp71a@example.com", models.GlobalRoleCreator)
	participant := test.MakeUser(t, db, "participant-mcp71a@example.com", models.GlobalRoleParticipant)
	sess := test.MakeSession(t, db, "ACL test session", owner.Email)
	require.NoError(t, db.CreateSessionMembership(ctx, sess.ID, participant.ID, "participant", nil))

	session, err := db.GetSession(ctx, sess.ID)
	require.NoError(t, err)
	require.NotNil(t, session)

	ok, err := userMayReadSessionMCP(ctx, db, session, participant)
	require.NoError(t, err)
	require.True(t, ok, "participant membership should grant read access")
}

func TestUserMayReadSessionMCP_DBNonParticipantDenies(t *testing.T) {
	ctx := context.Background()
	db := setupMCPTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)

	owner := test.MakeUser(t, db, "owner-mcp71b@example.com", models.GlobalRoleCreator)
	stranger := test.MakeUser(t, db, "stranger-mcp71b@example.com", models.GlobalRoleParticipant)
	sess := test.MakeSession(t, db, "ACL deny test", owner.Email)

	session, err := db.GetSession(ctx, sess.ID)
	require.NoError(t, err)
	require.NotNil(t, session)

	ok, err := userMayReadSessionMCP(ctx, db, session, stranger)
	require.NoError(t, err)
	require.False(t, ok, "user without membership or creator email should not read")
}
