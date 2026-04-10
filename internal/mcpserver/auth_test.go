package mcpserver

import (
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

func TestLoadAuthFromEnv_keyUserMapJSON(t *testing.T) {
	alice := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	bob := uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
	t.Setenv("TALKBACK_MCP_API_KEY", "k1,k2")
	t.Setenv("TALKBACK_MCP_ACTING_USER_ID", "")
	t.Setenv("TALKBACK_MCP_KEY_USER_MAP_JSON", `{"k1":"`+alice.String()+`"}`)
	t.Setenv("TALKBACK_MCP_REQUIRE_CLIENT_KEY", "true")

	got, err := LoadAuthFromEnv()
	require.NoError(t, err)
	require.True(t, got.HasPerKeyActingUsers())
	u, ok := got.ActingUserForClientKey("k1")
	require.True(t, ok)
	require.Equal(t, alice, u)
	_, ok = got.ActingUserForClientKey("k2")
	require.False(t, ok)

	t.Setenv("TALKBACK_MCP_ACTING_USER_ID", bob.String())
	got2, err := LoadAuthFromEnv()
	require.NoError(t, err)
	u, ok = got2.ActingUserForClientKey("k2")
	require.True(t, ok)
	require.Equal(t, bob, u)
}

func TestLoadAuthFromEnv_keyUserMapJSON_errors(t *testing.T) {
	t.Setenv("TALKBACK_MCP_API_KEY", "only")
	t.Setenv("TALKBACK_MCP_ACTING_USER_ID", "")
	t.Setenv("TALKBACK_MCP_REQUIRE_CLIENT_KEY", "true")

	t.Setenv("TALKBACK_MCP_KEY_USER_MAP_JSON", `{not json`)
	_, err := LoadAuthFromEnv()
	require.Error(t, err)

	t.Setenv("TALKBACK_MCP_KEY_USER_MAP_JSON", `{"other":"11111111-1111-1111-1111-111111111111"}`)
	_, err = LoadAuthFromEnv()
	require.Error(t, err)
	require.Contains(t, err.Error(), "not listed")

	t.Setenv("TALKBACK_MCP_KEY_USER_MAP_JSON", `{"only":"00000000-0000-0000-0000-000000000000"}`)
	_, err = LoadAuthFromEnv()
	require.Error(t, err)
	require.Contains(t, err.Error(), "nil UUID")
}
