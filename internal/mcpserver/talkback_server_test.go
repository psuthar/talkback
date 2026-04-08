package mcpserver

import (
	"context"
	"encoding/json"
	"slices"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/psuthar/talkback/internal/database"
)

func TestNewTalkbackMCPServer_ListTools(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	auth := Auth{
		acceptedKeys:     []string{"unit-test-key"},
		RequireClientKey: false,
	}
	srv := NewTalkbackMCPServer(TalkbackServerConfig{
		Version: "test-v",
		DB:      nil,
		Auth:    auth,
	})
	impl := &mcp.Implementation{Name: TalkbackMCPName, Version: "test-v"}
	client := mcp.NewClient(impl, nil)
	st, ct := mcp.NewInMemoryTransports()
	ss, err := srv.Connect(ctx, st, nil)
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
	var names []string
	for _, tool := range res.Tools {
		names = append(names, tool.Name)
	}
	slices.Sort(names)
	want := slices.Clone(ToolNames(RegisterConfig{DB: nil}))
	slices.Sort(want)
	if !slices.Equal(names, want) {
		t.Fatalf("ListTools: got %v want %v", names, want)
	}
}

func TestNewTalkbackMCPServer_CallTool_health_check(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	auth := Auth{
		acceptedKeys:     []string{"unit-test-key"},
		RequireClientKey: false,
	}
	srv := NewTalkbackMCPServer(TalkbackServerConfig{
		Version: "1.2.3",
		DB:      nil,
		Auth:    auth,
	})
	impl := &mcp.Implementation{Name: TalkbackMCPName, Version: "1.2.3"}
	client := mcp.NewClient(impl, nil)
	st, ct := mcp.NewInMemoryTransports()
	ss, err := srv.Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	cs, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()

	res, err := cs.CallTool(ctx, &mcp.CallToolParams{
		Name:      ToolHealthCheck,
		Arguments: map[string]any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Content) < 1 {
		t.Fatalf("no content: %+v", res)
	}
	tc, ok := res.Content[0].(*mcp.TextContent)
	if !ok {
		t.Fatalf("want TextContent, got %T", res.Content[0])
	}
	var body map[string]string
	if err := json.Unmarshal([]byte(tc.Text), &body); err != nil {
		t.Fatalf("JSON: %v", err)
	}
	if body["status"] != "ok" || body["service"] != TalkbackMCPName {
		t.Fatalf("body: %+v", body)
	}
	if body["version"] != "1.2.3" {
		t.Fatalf("version: %q", body["version"])
	}
}

func TestNewTalkbackMCPServer_ListTools_withDBRegistersSessionTool(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	auth := Auth{acceptedKeys: []string{"k"}, RequireClientKey: false}
	stub := &database.DB{}
	srv := NewTalkbackMCPServer(TalkbackServerConfig{
		Version: "v",
		DB:      stub,
		Auth:    auth,
	})
	impl := &mcp.Implementation{Name: TalkbackMCPName, Version: "v"}
	client := mcp.NewClient(impl, nil)
	st, ct := mcp.NewInMemoryTransports()
	ss, err := srv.Connect(ctx, st, nil)
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
	want := ToolNames(RegisterConfig{DB: stub})
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}
