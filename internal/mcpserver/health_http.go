package mcpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/psuthar/talkback/internal/database"
)

// RegisterHealthHTTPRoutes mounts GET /healthz (liveness) and GET /ready (readiness) on mux.
// Used when TALKBACK_MCP_HEALTH_ADDR is set in cmd/talkback-mcp (Kubernetes probes; SCRUM-69).
// These endpoints are unauthenticated by design (in-cluster probes); do not expose them to untrusted networks without a boundary.
func RegisterHealthHTTPRoutes(mux *http.ServeMux, version string, db *database.DB) {
	mux.HandleFunc("/healthz", healthzHandler(version))
	mux.HandleFunc("/ready", readyHandler(db))
}

func healthzHandler(version string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(HealthCheckResponse{
			Status:  "ok",
			Service: TalkbackMCPName,
			Version: version,
		})
	}
}

type readyHTTPResponse struct {
	Status string `json:"status"`
}

func readyHandler(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if db != nil && db.Pool != nil {
			ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
			defer cancel()
			if err := db.Pool.Ping(ctx); err != nil {
				http.Error(w, "database unavailable", http.StatusServiceUnavailable)
				return
			}
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(readyHTTPResponse{Status: "ready"})
	}
}
