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

// newCrossSessionMCPTestClient wires a TalkBack MCP server with DB + acting user (IDE auth mode).
func newCrossSessionMCPTestClient(t *testing.T, db *database.DB, actingUserID uuid.UUID) (ctx context.Context, client *mcp.ClientSession, cleanup func()) {
	t.Helper()
	ctx = context.Background()
	auth := Auth{
		acceptedKeys:     []string{"unit-cross-session-key"},
		actingUserID:     actingUserID,
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
	cleanup = func() {
		_ = cs.Close()
		_ = ss.Close()
	}
	return ctx, cs, cleanup
}

func toolResultText(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	require.NotEmpty(t, res.Content, "tool result content")
	tc, ok := res.Content[0].(*mcp.TextContent)
	require.True(t, ok, "want TextContent, got %T", res.Content[0])
	return tc.Text
}

func TestGetDecisionsByTopic_MCP_emptyTopicError(t *testing.T) {
	db := setupMCPTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)

	u := test.MakeUser(t, db, "gdbt-empty@example.com", models.GlobalRoleCreator)
	ctx, cs, done := newCrossSessionMCPTestClient(t, db, u.ID)
	defer done()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: ToolGetDecisionsByTopic,
		Arguments: map[string]any{
			"topic": "   ",
		},
	})
	require.NoError(t, err)
	require.True(t, res.IsError, "empty topic should surface as tool error")
	var payload map[string]any
	require.NoError(t, json.Unmarshal([]byte(toolResultText(t, res)), &payload))
	require.Equal(t, float64(400), payload["http_status"])
}

func TestGetDecisionsByTopic_MCP_noAccessibleSessions(t *testing.T) {
	db := setupMCPTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)

	u := test.MakeUser(t, db, "gdbt-none@example.com", models.GlobalRoleCreator)
	ctx, cs, done := newCrossSessionMCPTestClient(t, db, u.ID)
	defer done()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: ToolGetDecisionsByTopic,
		Arguments: map[string]any{
			"topic": "anything",
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	var out getDecisionsByTopicOutput
	decodeStructured(t, res, &out)
	require.Equal(t, decisionsByTopicSchemaVersion, out.SchemaVersion)
	require.Equal(t, "anything", out.Topic)
	require.Empty(t, out.Results)
	require.Contains(t, out.Note, "No accessible sessions")
}

func TestGetDecisionsByTopic_MCP_matchesPremise(t *testing.T) {
	db := setupMCPTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)

	u := test.MakeUser(t, db, "gdbt-match@example.com", models.GlobalRoleCreator)
	sess := test.MakeSession(t, db, "Decision visibility", u.Email)
	premise := "We must align on the quarterly roadmap pivot"
	require.NoError(t, db.UpdateSessionContext(context.Background(), sess.ID, &premise, nil, nil))

	ctx, cs, done := newCrossSessionMCPTestClient(t, db, u.ID)
	defer done()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: ToolGetDecisionsByTopic,
		Arguments: map[string]any{
			"topic": "roadmap",
			"limit": 10,
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	var out getDecisionsByTopicOutput
	decodeStructured(t, res, &out)
	require.Len(t, out.Results, 1)
	require.Equal(t, sess.ID.String(), out.Results[0].SessionID)
	require.NotNil(t, out.Results[0].Premise)
	require.Contains(t, *out.Results[0].Premise, "roadmap")
}

func TestSearchAllSessions_MCP_openAINotConfigured(t *testing.T) {
	db := setupMCPTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)

	u := test.MakeUser(t, db, "sas-openai@example.com", models.GlobalRoleCreator)
	test.MakeSession(t, db, "Has session", u.Email)

	t.Setenv("OPENAI_API_KEY", "")
	ctx, cs, done := newCrossSessionMCPTestClient(t, db, u.ID)
	defer done()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: ToolSearchAllSessions,
		Arguments: map[string]any{
			"query": "hello",
		},
	})
	require.NoError(t, err)
	require.True(t, res.IsError)
	var payload map[string]any
	require.NoError(t, json.Unmarshal([]byte(toolResultText(t, res)), &payload))
	require.Equal(t, float64(503), payload["http_status"])
	require.Equal(t, "openai_not_configured", payload["error_code"])
}

func TestSearchAllSessions_MCP_noAccessibleSessions(t *testing.T) {
	db := setupMCPTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)

	u := test.MakeUser(t, db, "sas-none@example.com", models.GlobalRoleCreator)
	t.Setenv("OPENAI_API_KEY", "sk-test-placeholder-for-handler-branch")
	ctx, cs, done := newCrossSessionMCPTestClient(t, db, u.ID)
	defer done()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: ToolSearchAllSessions,
		Arguments: map[string]any{
			"query": "hello",
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	var out searchAllSessionsOutput
	decodeStructured(t, res, &out)
	require.Equal(t, crossSessionSearchSchemaVersion, out.SchemaVersion)
	require.Empty(t, out.Results)
	require.Contains(t, out.Note, "No accessible sessions")
}

func TestSearchAllSessions_MCP_noIndexedChunks(t *testing.T) {
	db := setupMCPTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)

	u := test.MakeUser(t, db, "sas-nochunks@example.com", models.GlobalRoleCreator)
	test.MakeSession(t, db, "Indexed later", u.Email)

	t.Setenv("OPENAI_API_KEY", "sk-test-placeholder-for-handler-branch")
	ctx, cs, done := newCrossSessionMCPTestClient(t, db, u.ID)
	defer done()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: ToolSearchAllSessions,
		Arguments: map[string]any{
			"query": "anything",
		},
	})
	require.NoError(t, err)
	require.False(t, res.IsError)

	var out searchAllSessionsOutput
	decodeStructured(t, res, &out)
	require.Empty(t, out.Results)
	require.Contains(t, out.Note, "No indexed chunks")
}

func TestSearchAllSessions_MCP_emptyQueryError(t *testing.T) {
	db := setupMCPTestDB(t)
	defer db.Close()
	defer test.TruncateTables(t, db.Pool)

	u := test.MakeUser(t, db, "sas-emptyq@example.com", models.GlobalRoleCreator)
	t.Setenv("OPENAI_API_KEY", "sk-test-placeholder-for-handler-branch")
	ctx, cs, done := newCrossSessionMCPTestClient(t, db, u.ID)
	defer done()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name: ToolSearchAllSessions,
		Arguments: map[string]any{
			"query": "  ",
		},
	})
	require.NoError(t, err)
	require.True(t, res.IsError)
	var payload map[string]any
	require.NoError(t, json.Unmarshal([]byte(toolResultText(t, res)), &payload))
	require.Equal(t, float64(400), payload["http_status"])
}

func decodeStructured(t *testing.T, res *mcp.CallToolResult, dest any) {
	t.Helper()
	if res.StructuredContent != nil {
		b, err := json.Marshal(res.StructuredContent)
		require.NoError(t, err)
		require.NoError(t, json.Unmarshal(b, dest))
		return
	}
	require.NoError(t, json.Unmarshal([]byte(toolResultText(t, res)), dest))
}
