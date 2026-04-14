package mcpserver

import (
	"net/http"
)

// HTTPBearerAuthMiddleware returns an HTTP middleware that rejects requests without a valid
// token before they reach the MCP HTTP handler.
//
// Token lookup order:
//  1. Authorization: Bearer <token> header
//  2. ?key=<token> query parameter (for clients such as Claude Code that cannot attach
//     custom headers in their MCP URL config)
//
// This is a request-level guard for the HTTP transport (SCRUM-83). It supplements
// the existing tool-dispatch-level [Auth.RequireToolAuthMiddleware]; the HTTP guard runs
// first so unauthenticated connections never reach MCP protocol handling.
//
// The check always validates the token against the configured accepted keys,
// regardless of [Auth.RequireClientKey] — the HTTP transport is network-exposed so
// request-level auth is always enforced.
func (a Auth) HTTPBearerAuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := parseBearer(r.Header.Get("Authorization"))
		if token == "" {
			token = r.URL.Query().Get("key")
		}
		if !a.ValidKey(token) {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
			return
		}
		next.ServeHTTP(w, r)
	})
}
