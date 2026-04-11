package mcpserver

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/psuthar/talkback/internal/database"
	"github.com/psuthar/talkback/internal/models"
	"github.com/psuthar/talkback/internal/test"
	"github.com/stretchr/testify/require"
)

func newListSessionsMCPTestClientNoActingUser(t *testing.T, db *database.DB) (context.Context, *mcp.ClientSession, func()) {
	t.Helper()
	ctx := context.Background()
	auth := Auth{
		acceptedKeys:     []string{"unit-list-sessions-no-actor"},
		actingUserID:     uuid.Nil,
		RequireClientKey: false,
	}
	srv := NewTalkbackMCPServer(TalkbackServerConfig{
		Version: "test",
		DB:      db,
		Auth:    auth,
	})
	impl := &mcp.Implementation{Name: TalkbackMCPName, Version: "test"}
	mcpClient := mcp.NewClient(impl, nil)
	st, ct := mcp.NewInMemoryTransports()
	ss, err := srv.Connect(ctx, st, nil)
	require.NoError(t, err)
	cs, err := mcpClient.Connect(ctx, ct, nil)
	require.NoError(t, err)
	cleanup := func() {
		_ = cs.Close()
		_ = ss.Close()
	}
	return ctx, cs, cleanup
}

func TestListSessions_MCP_actingUserNotConfigured(t *testing.T) {
	db := setupMCPTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)

	_ = test.MakeUser(t, db, "ls-noactor@example.com", models.GlobalRoleCreator)
	ctx, cs, done := newListSessionsMCPTestClientNoActingUser(t, db)
	defer done()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      ToolListSessions,
		Arguments: map[string]any{},
	})
	require.NoError(t, err)
	require.True(t, res.IsError)
	var payload map[string]any
	require.NoError(t, json.Unmarshal([]byte(toolResultText(t, res)), &payload))
	require.Equal(t, float64(403), payload["http_status"])
	require.Equal(t, mcpErrCodeActingUserNotConfigured, payload["error_code"])
}

func TestListSessions_MCP_creatorSeesOwnSession(t *testing.T) {
	db := setupMCPTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)

	u := test.MakeUser(t, db, "ls-creator@example.com", models.GlobalRoleCreator)
	sess := test.MakeSession(t, db, "List sessions MCP", u.Email)
	ctx, cs, done := newCrossSessionMCPTestClient(t, db, u.ID)
	defer done()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      ToolListSessions,
		Arguments: map[string]any{},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	var out struct {
		Sessions []struct {
			Session map[string]any `json:"session"`
			MyRole  string         `json:"my_role"`
		} `json:"sessions"`
	}
	decodeStructured(t, res, &out)
	require.Len(t, out.Sessions, 1)
	require.Equal(t, sess.ID.String(), out.Sessions[0].Session["id"])
	require.Equal(t, "creator", out.Sessions[0].MyRole)
}

func TestListSessions_MCP_participantOnlyInvited(t *testing.T) {
	db := setupMCPTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)

	owner := test.MakeUser(t, db, "ls-owner@example.com", models.GlobalRoleCreator)
	other := test.MakeUser(t, db, "ls-other@example.com", models.GlobalRoleCreator)
	participant := test.MakeUser(t, db, "ls-participant@example.com", models.GlobalRoleParticipant)

	sInvited := test.MakeSession(t, db, "Invited session", owner.Email)
	_ = test.MakeSession(t, db, "Not invited", other.Email)
	require.NoError(t, db.CreateSessionMembership(context.Background(), sInvited.ID, participant.ID, "participant", nil))

	ctx, cs, done := newCrossSessionMCPTestClient(t, db, participant.ID)
	defer done()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      ToolListSessions,
		Arguments: map[string]any{},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	var out struct {
		Sessions []struct {
			Session map[string]any `json:"session"`
			MyRole  string         `json:"my_role"`
		} `json:"sessions"`
	}
	decodeStructured(t, res, &out)
	require.Len(t, out.Sessions, 1)
	require.Equal(t, sInvited.ID.String(), out.Sessions[0].Session["id"])
	require.Equal(t, "participant", out.Sessions[0].MyRole)
}

func TestListSessions_MCP_adminSeesMultiple(t *testing.T) {
	db := setupMCPTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)

	admin := test.MakeUser(t, db, "ls-admin@example.com", models.GlobalRoleAdmin)
	u1 := test.MakeUser(t, db, "ls-a1@example.com", models.GlobalRoleCreator)
	u2 := test.MakeUser(t, db, "ls-a2@example.com", models.GlobalRoleCreator)
	_ = test.MakeSession(t, db, "Admin sees A", u1.Email)
	_ = test.MakeSession(t, db, "Admin sees B", u2.Email)

	ctx, cs, done := newCrossSessionMCPTestClient(t, db, admin.ID)
	defer done()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      ToolListSessions,
		Arguments: map[string]any{},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	var out struct {
		Sessions []struct {
			Session map[string]any `json:"session"`
			MyRole  string         `json:"my_role"`
		} `json:"sessions"`
	}
	decodeStructured(t, res, &out)
	require.Len(t, out.Sessions, 2)
	for _, row := range out.Sessions {
		require.Equal(t, "admin", row.MyRole)
	}
}

func TestListSessions_MCP_limit(t *testing.T) {
	db := setupMCPTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)

	u := test.MakeUser(t, db, "ls-limit@example.com", models.GlobalRoleCreator)
	for i := 0; i < 3; i++ {
		_ = test.MakeSession(t, db, "S", u.Email)
	}

	ctx, cs, done := newCrossSessionMCPTestClient(t, db, u.ID)
	defer done()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: ToolListSessions,
		Arguments: map[string]any{
			"limit": 2,
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	var out struct {
		Sessions []struct {
			Session map[string]any `json:"session"`
			MyRole  string         `json:"my_role"`
		} `json:"sessions"`
	}
	decodeStructured(t, res, &out)
	require.Len(t, out.Sessions, 2)
}
