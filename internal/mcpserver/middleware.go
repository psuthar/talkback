package mcpserver

import (
	"context"
	"fmt"
	"log"

	"github.com/google/uuid"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// RequireToolAuthMiddleware enforces shared-secret API keys for MCP tool invocation (SCRUM-38).
//
// It applies only to the JSON-RPC method "tools/call". Other methods—e.g. "initialize", "tools/list",
// notifications—pass through without key checks so clients can complete the MCP handshake and discover tools.
//
// When [Auth.RequireClientKey] is true (default), the client key must match one of the keys loaded from
// TALKBACK_MCP_API_KEY (comma-separated rotation). Keys are compared in constant time per candidate of
// equal length; failed checks log tool name only, never the key. Extraction: [ExtractClientAPIKey] from
// _meta and HTTP extras when present.
//
// When RequireClientKey is false ("IDE mode", TALKBACK_MCP_REQUIRE_CLIENT_KEY=false), the process still
// requires env config at startup but does not require per-call keys—documented weaker trust for hosts
// that cannot attach MCP metadata.
func (a Auth) RequireToolAuthMiddleware() mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			if method != "tools/call" {
				return next(ctx, method, req)
			}
			p, ok := req.GetParams().(*mcp.CallToolParamsRaw)
			if !ok {
				return nil, fmt.Errorf("tools/call: unexpected params type %T", req.GetParams())
			}
			if a.RequireClientKey {
				key := ExtractClientAPIKey(p.Meta, req.GetExtra())
				if !a.ValidKey(key) {
					log.Printf("mcp auth failed tool=%s (missing or invalid API key)", p.Name)
					return nil, fmt.Errorf("unauthorized: invalid or missing API key")
				}
			}
			if a.actingUserID != uuid.Nil {
				ctx = context.WithValue(ctx, actingUserCtxKey{}, a.actingUserID)
			}
			return next(ctx, method, req)
		}
	}
}
