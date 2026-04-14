package mcpserver

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

func TestRequireToolAuthMiddleware_skipsHandshakeMethods(t *testing.T) {
	t.Parallel()
	a := Auth{acceptedKeys: []string{"good"}, RequireClientKey: true}
	mw := a.RequireToolAuthMiddleware()

	methods := []string{
		"initialize",
		"tools/list",
		"ping",
		"notifications/initialized",
	}
	for _, method := range methods {
		method := method
		t.Run(method, func(t *testing.T) {
			t.Parallel()
			called := false
			next := func(context.Context, string, mcp.Request) (mcp.Result, error) {
				called = true
				return nil, nil
			}
			handler := mw(next)
			_, err := handler(context.Background(), method, nil)
			require.NoError(t, err)
			require.True(t, called, "middleware should forward %q without auth", method)
		})
	}
}

func TestRequireToolAuthMiddleware_acceptsSecondRotationKey(t *testing.T) {
	t.Parallel()
	a := Auth{acceptedKeys: []string{"alpha", "beta"}, RequireClientKey: true}
	mw := a.RequireToolAuthMiddleware()
	called := false
	next := func(context.Context, string, mcp.Request) (mcp.Result, error) {
		called = true
		return nil, nil
	}
	handler := mw(next)
	params := &mcp.CallToolParamsRaw{
		Name: "health_check",
		Meta: mcp.Meta{"talkback": map[string]any{"apiKey": "beta"}},
	}
	req := &mcp.ServerRequest[*mcp.CallToolParamsRaw]{Params: params}
	_, err := handler(context.Background(), "tools/call", req)
	require.NoError(t, err)
	require.True(t, called)
}

func TestRequireToolAuthMiddleware_perKeyActingUser(t *testing.T) {
	t.Parallel()
	uidA := uuid.MustParse("33333333-3333-3333-3333-333333333333")
	uidB := uuid.MustParse("44444444-4444-4444-4444-444444444444")
	a := Auth{
		acceptedKeys:     []string{"alpha", "beta"},
		actingUserID:     uidB,
		keyToUser:        map[string]uuid.UUID{"alpha": uidA},
		RequireClientKey: true,
	}
	mw := a.RequireToolAuthMiddleware()

	t.Run("alpha gets mapped user", func(t *testing.T) {
		t.Parallel()
		var sawCtx context.Context
		next := func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			sawCtx = ctx
			return nil, nil
		}
		handler := mw(next)
		params := &mcp.CallToolParamsRaw{
			Name: "health_check",
			Meta: mcp.Meta{"talkback": map[string]any{"apiKey": "alpha"}},
		}
		req := &mcp.ServerRequest[*mcp.CallToolParamsRaw]{Params: params}
		_, err := handler(context.Background(), "tools/call", req)
		require.NoError(t, err)
		got, ok := ActingUserID(sawCtx)
		require.True(t, ok)
		require.Equal(t, uidA, got)
	})

	t.Run("beta falls back to global acting user", func(t *testing.T) {
		t.Parallel()
		var sawCtx context.Context
		next := func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			sawCtx = ctx
			return nil, nil
		}
		handler := mw(next)
		params := &mcp.CallToolParamsRaw{
			Name: "health_check",
			Meta: mcp.Meta{"talkback": map[string]any{"apiKey": "beta"}},
		}
		req := &mcp.ServerRequest[*mcp.CallToolParamsRaw]{Params: params}
		_, err := handler(context.Background(), "tools/call", req)
		require.NoError(t, err)
		got, ok := ActingUserID(sawCtx)
		require.True(t, ok)
		require.Equal(t, uidB, got)
	})
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

func TestRequireToolAuthMiddleware_callerActingUserHeader(t *testing.T) {
	t.Parallel()
	callerUID := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	a := Auth{
		acceptedKeys:          []string{"good"},
		RequireClientKey:      true,
		AllowCallerActingUser: true,
	}
	mw := a.RequireToolAuthMiddleware()

	t.Run("header is used when flag is on", func(t *testing.T) {
		t.Parallel()
		var sawCtx context.Context
		next := func(ctx context.Context, _ string, _ mcp.Request) (mcp.Result, error) {
			sawCtx = ctx
			return nil, nil
		}
		handler := mw(next)
		params := &mcp.CallToolParamsRaw{
			Name: "list_sessions",
			Meta: mcp.Meta{"talkback": map[string]any{"apiKey": "good"}},
		}
		h := http.Header{}
		h.Set(callerActingUserHeader, callerUID.String())
		req := &mcp.ServerRequest[*mcp.CallToolParamsRaw]{
			Params: params,
			Extra:  &mcp.RequestExtra{Header: h},
		}
		_, err := handler(context.Background(), "tools/call", req)
		require.NoError(t, err)
		got, ok := ActingUserID(sawCtx)
		require.True(t, ok)
		require.Equal(t, callerUID, got)
	})

	t.Run("header is ignored when flag is off", func(t *testing.T) {
		t.Parallel()
		globalUID := uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
		a2 := Auth{
			acceptedKeys:          []string{"good"},
			actingUserID:          globalUID,
			RequireClientKey:      true,
			AllowCallerActingUser: false,
		}
		mw2 := a2.RequireToolAuthMiddleware()
		var sawCtx context.Context
		next := func(ctx context.Context, _ string, _ mcp.Request) (mcp.Result, error) {
			sawCtx = ctx
			return nil, nil
		}
		handler := mw2(next)
		params := &mcp.CallToolParamsRaw{
			Name: "list_sessions",
			Meta: mcp.Meta{"talkback": map[string]any{"apiKey": "good"}},
		}
		h := http.Header{}
		h.Set(callerActingUserHeader, callerUID.String())
		req := &mcp.ServerRequest[*mcp.CallToolParamsRaw]{
			Params: params,
			Extra:  &mcp.RequestExtra{Header: h},
		}
		_, err := handler(context.Background(), "tools/call", req)
		require.NoError(t, err)
		got, ok := ActingUserID(sawCtx)
		require.True(t, ok)
		require.Equal(t, globalUID, got, "should use global acting user, not caller header")
	})
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
