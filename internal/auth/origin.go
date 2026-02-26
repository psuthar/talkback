package auth

import (
	"net/http"
	"strings"
)

// AllowOriginForAuth returns true if the request's Origin (if any) is allowed for auth routes.
// If Origin header is missing, returns true (e.g. same-origin or non-browser).
// If Origin is present and TB_ALLOWED_ORIGINS is set, Origin must be in the list.
func AllowOriginForAuth(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	if len(Config.AllowedOrigins) == 0 {
		return true
	}
	for _, o := range Config.AllowedOrigins {
		if o == origin {
			return true
		}
	}
	return false
}
