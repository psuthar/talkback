package debugfault

import (
	"net/http"
	"strings"
)

// FaultRouter handles /debug/fault/* routes. All routes require OBS_TEST_MODE=true
// and optionally OBS_TEST_TOKEN (X-Obs-Test-Token header).
func FaultRouter(w http.ResponseWriter, r *http.Request) {
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/debug/fault"), "/")
	parts := strings.Split(path, "/")

	if len(parts) == 0 || (len(parts) == 1 && parts[0] == "") {
		http.NotFound(w, r)
		return
	}

	route := parts[0]
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	switch route {
	case "error":
		Guard(HandleError)(w, r)
	case "latency":
		Guard(HandleLatency)(w, r)
	case "r2":
		Guard(HandleR2)(w, r)
	default:
		http.NotFound(w, r)
	}
}
