package mcpserver

import (
	"context"
	"testing"

	"github.com/psuthar/talkback/internal/models"
	"github.com/psuthar/talkback/internal/test"
	"github.com/stretchr/testify/require"
)

func TestBuildGetSessionDecisionsOutput_fromDB(t *testing.T) {
	ctx := context.Background()
	db := setupMCPTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)

	owner := test.MakeUser(t, db, "owner-scrum55@example.com", models.GlobalRoleCreator)
	participant := test.MakeUser(t, db, "part-scrum55@example.com", models.GlobalRoleParticipant)
	sess := test.MakeSession(t, db, "Decision session", owner.Email)
	require.NoError(t, db.UpdateSessionContext(ctx, sess.ID, strPtr("our premise"), strPtr("choose A"), strPtr("picked A")))
	require.NoError(t, db.CreateSessionMembership(ctx, sess.ID, participant.ID, "participant", nil))

	rationale := "team alignment"
	_, err := db.CreateOrUpdateStance(ctx, sess.ID, participant.ID, models.StanceAgree, &rationale)
	require.NoError(t, err)

	session, err := db.GetSession(ctx, sess.ID)
	require.NoError(t, err)
	stances, err := db.GetStancesBySessionID(ctx, sess.ID)
	require.NoError(t, err)

	out := buildGetSessionDecisionsOutput(session, stances)
	require.Equal(t, "1", out.SchemaVersion)
	require.Equal(t, "our premise", *out.Premise)
	require.Equal(t, "choose A", *out.PrimaryDecision)
	require.Equal(t, "picked A", *out.DecisionOutcome)
	require.Len(t, out.DecisionStances, 1)
	require.Equal(t, models.StanceAgree, out.DecisionStances[0].Stance)
	require.Equal(t, participant.Email, out.DecisionStances[0].UserEmail)
}
