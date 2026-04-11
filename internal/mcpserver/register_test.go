package mcpserver

import (
	"context"
	"slices"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/psuthar/talkback/internal/database"
)

func TestToolNames(t *testing.T) {
	t.Parallel()
	if got := ToolNames(RegisterConfig{DB: nil}); !slices.Equal(got, []string{ToolHealthCheck}) {
		t.Fatalf("DB nil: got %v", got)
	}
	var stubDB database.DB
	if got := ToolNames(RegisterConfig{DB: &stubDB}); !slices.Equal(got, []string{ToolHealthCheck, ToolGetSessionMetadata, ToolListSessions, ToolGetSessionDecisions, ToolGetDecisions, ToolGetDecisionsByTopic, ToolGetSessionActionItems, ToolGetActionItems, ToolSearchSession, ToolSearchSessionContent, ToolSearchAllSessions, ToolGetSessionRawChunks, ToolGetSessionRetrievalContext, ToolGetSessionSourceChunks, ToolAskSession, ToolAskSessionQuestion}) {
		t.Fatalf("DB set: got %v", got)
	}
}

func TestRegisterListsExpectedTools(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	impl := &mcp.Implementation{Name: "test-talkback-mcp", Version: "test"}

	t.Run("withoutDB", func(t *testing.T) {
		t.Parallel()
		server := mcp.NewServer(impl, nil)
		Register(server, RegisterConfig{Version: "v1", DB: nil})
		assertListToolsMatch(t, ctx, impl, server, ToolNames(RegisterConfig{DB: nil}))
	})

	t.Run("withDBStub", func(t *testing.T) {
		t.Parallel()
		server := mcp.NewServer(impl, nil)
		stubDB := &database.DB{}
		Register(server, RegisterConfig{Version: "v1", DB: stubDB})
		assertListToolsMatch(t, ctx, impl, server, ToolNames(RegisterConfig{DB: stubDB}))
	})
}

func assertListToolsMatch(t *testing.T, ctx context.Context, impl *mcp.Implementation, server *mcp.Server, want []string) {
	t.Helper()
	client := mcp.NewClient(impl, nil)
	st, ct := mcp.NewInMemoryTransports()
	ss, err := server.Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	cs, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()

	res, err := cs.ListTools(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, tool := range res.Tools {
		got = append(got, tool.Name)
	}
	slices.Sort(got)
	wantSorted := slices.Clone(want)
	slices.Sort(wantSorted)
	if !slices.Equal(got, wantSorted) {
		t.Fatalf("tool names: got %v want %v", got, wantSorted)
	}
}
