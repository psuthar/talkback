package debugfault

import (
	"net/http"
	"os"
	"strings"
)

const (
	// HeaderTestToken is the header callers must send when OBS_TEST_TOKEN is configured.
	HeaderTestToken = "X-Obs-Test-Token"
)

// Config holds fault-injection test mode settings from env.
type Config struct {
	Enabled bool   // OBS_TEST_MODE=true
	Token   string // OBS_TEST_TOKEN (optional; when set, require header match)
}

// LoadConfig reads OBS_TEST_MODE and OBS_TEST_TOKEN from env.
func LoadConfig() Config {
	enabled := isTruthy(os.Getenv("OBS_TEST_MODE"))
	token := strings.TrimSpace(os.Getenv("OBS_TEST_TOKEN"))
	return Config{Enabled: enabled, Token: token}
}

// RequireAuthorized returns true if the request is authorized for fault routes.
// If OBS_TEST_MODE is not true, returns false (caller should return 404).
// If OBS_TEST_TOKEN is set and header does not match, returns false (caller should return 403).
func RequireAuthorized(r *http.Request) bool {
	cfg := LoadConfig()
	if !cfg.Enabled {
		return false
	}
	if cfg.Token != "" {
		got := strings.TrimSpace(r.Header.Get(HeaderTestToken))
		if got != cfg.Token {
			return false
		}
	}
	return true
}

// Guard wraps an http.HandlerFunc: if not authorized, writes 404 or 403 and returns false.
// Caller should not invoke next when Guard returns false.
func Guard(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cfg := LoadConfig()
		if !cfg.Enabled {
			http.NotFound(w, r)
			return
		}
		if cfg.Token != "" {
			got := strings.TrimSpace(r.Header.Get(HeaderTestToken))
			if got != cfg.Token {
				http.Error(w, "Forbidden: invalid or missing X-Obs-Test-Token", http.StatusForbidden)
				return
			}
		}
		next(w, r)
	}
}

func isTruthy(s string) bool {
	s = strings.TrimSpace(strings.ToLower(s))
	return s == "1" || s == "true" || s == "yes" || s == "on"
}
