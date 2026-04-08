package mcpserver

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

func TestExtractClientAPIKey_meta(t *testing.T) {
	meta := mcp.Meta{
		"talkback": map[string]any{
			"apiKey": "secret-one",
		},
	}
	require.Equal(t, "secret-one", ExtractClientAPIKey(meta, nil))

	meta2 := mcp.Meta{
		"talkback": map[string]any{
			"api_key": "secret-two",
		},
	}
	require.Equal(t, "secret-two", ExtractClientAPIKey(meta2, nil))

	meta3 := mcp.Meta{"talkbackApiKey": "secret-three"}
	require.Equal(t, "secret-three", ExtractClientAPIKey(meta3, nil))

	meta4 := mcp.Meta{"authorization": "Bearer token-four"}
	require.Equal(t, "token-four", ExtractClientAPIKey(meta4, nil))
}

func TestExtractClientAPIKey_headers(t *testing.T) {
	h := http.Header{}
	h.Set("Authorization", "Bearer hdr-secret")
	extra := &mcp.RequestExtra{Header: h}
	require.Equal(t, "hdr-secret", ExtractClientAPIKey(nil, extra))

	h2 := http.Header{}
	h2.Set("X-API-Key", "x-key-val")
	extra2 := &mcp.RequestExtra{Header: h2}
	require.Equal(t, "x-key-val", ExtractClientAPIKey(nil, extra2))
}

func TestAuth_ValidKey(t *testing.T) {
	a := Auth{acceptedKeys: []string{"alpha", "beta"}}
	require.True(t, a.ValidKey("alpha"))
	require.True(t, a.ValidKey("beta"))
	require.False(t, a.ValidKey("gamma"))
	require.False(t, a.ValidKey(""))
	require.False(t, a.ValidKey("Alpha"))
}

func TestLoadAuthFromEnv(t *testing.T) {
	t.Setenv("TALKBACK_MCP_API_KEY", "")
	_, err := LoadAuthFromEnv()
	require.Error(t, err)

	t.Setenv("TALKBACK_MCP_API_KEY", "k1,k2")
	t.Setenv("TALKBACK_MCP_ACTING_USER_ID", "")
	t.Setenv("TALKBACK_MCP_REQUIRE_CLIENT_KEY", "")
	got, err := LoadAuthFromEnv()
	require.NoError(t, err)
	require.Len(t, got.acceptedKeys, 2)
	require.Equal(t, "k1", got.acceptedKeys[0])
	require.Equal(t, uuid.Nil, got.actingUserID)
	require.True(t, got.RequireClientKey)

	t.Setenv("TALKBACK_MCP_REQUIRE_CLIENT_KEY", "false")
	gotRelaxed, err := LoadAuthFromEnv()
	require.NoError(t, err)
	require.False(t, gotRelaxed.RequireClientKey)

	id := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	t.Setenv("TALKBACK_MCP_REQUIRE_CLIENT_KEY", "")
	t.Setenv("TALKBACK_MCP_ACTING_USER_ID", id.String())
	got2, err := LoadAuthFromEnv()
	require.NoError(t, err)
	require.Equal(t, id, got2.actingUserID)
}

func TestRequireToolAuthMiddleware_actingUserContext(t *testing.T) {
	uid := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	a := Auth{acceptedKeys: []string{"good"}, actingUserID: uid, RequireClientKey: true}
	mw := a.RequireToolAuthMiddleware()

	var sawCtx context.Context
	next := func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		sawCtx = ctx
		return nil, nil
	}
	handler := mw(next)

	params := &mcp.CallToolParamsRaw{
		Name: "health_check",
		Meta: mcp.Meta{"talkback": map[string]any{"apiKey": "good"}},
	}
	req := &mcp.ServerRequest[*mcp.CallToolParamsRaw]{
		Session: nil,
		Params:  params,
	}
	_, err := handler(context.Background(), "tools/call", req)
	require.NoError(t, err)
	got, ok := ActingUserID(sawCtx)
	require.True(t, ok)
	require.Equal(t, uid, got)
}

func TestRequireToolAuthMiddleware_relaxedAllowsWithoutClientKey(t *testing.T) {
	a := Auth{acceptedKeys: []string{"good"}, RequireClientKey: false}
	mw := a.RequireToolAuthMiddleware()
	called := false
	next := func(context.Context, string, mcp.Request) (mcp.Result, error) {
		called = true
		return nil, nil
	}
	handler := mw(next)
	params := &mcp.CallToolParamsRaw{
		Name: "health_check",
		Meta: nil,
	}
	req := &mcp.ServerRequest[*mcp.CallToolParamsRaw]{Params: params}
	_, err := handler(context.Background(), "tools/call", req)
	require.NoError(t, err)
	require.True(t, called)
}

func TestRequireToolAuthMiddleware_rejectsBadKey(t *testing.T) {
	a := Auth{acceptedKeys: []string{"good"}, RequireClientKey: true}
	mw := a.RequireToolAuthMiddleware()
	next := func(context.Context, string, mcp.Request) (mcp.Result, error) {
		t.Fatal("next should not run")
		return nil, nil
	}
	handler := mw(next)
	params := &mcp.CallToolParamsRaw{
		Name: "health_check",
		Meta: mcp.Meta{"talkback": map[string]any{"apiKey": "bad"}},
	}
	req := &mcp.ServerRequest[*mcp.CallToolParamsRaw]{Params: params}
	_, err := handler(context.Background(), "tools/call", req)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unauthorized")
}

func TestRequireToolAuthMiddleware_skipsNonToolMethods(t *testing.T) {
	a := Auth{acceptedKeys: []string{"good"}, RequireClientKey: true}
	mw := a.RequireToolAuthMiddleware()
	called := false
	next := func(context.Context, string, mcp.Request) (mcp.Result, error) {
		called = true
		return nil, nil
	}
	handler := mw(next)
	_, err := handler(context.Background(), "tools/list", nil)
	require.NoError(t, err)
	require.True(t, called)
}
