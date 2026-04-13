package mcpserver

import (
	"net/http"
)

// HTTPBearerAuthMiddleware returns an HTTP middleware that rejects requests without a valid
// Authorization: Bearer <token> header before they reach the MCP HTTP handler.
//
// This is a request-level guard for the HTTP transport (SCRUM-83). It supplements
// the existing tool-dispatch-level [Auth.RequireToolAuthMiddleware]; the HTTP guard runs
// first so unauthenticated connections never reach MCP protocol handling.
//
// The check always validates the Bearer token against the configured accepted keys,
// regardless of [Auth.RequireClientKey] — the HTTP transport is network-exposed so
// request-level auth is always enforced.
func (a Auth) HTTPBearerAuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := parseBearer(r.Header.Get("Authorization"))
		if !a.ValidKey(token) {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
			return
		}
		next.ServeHTTP(w, r)
	})
}
