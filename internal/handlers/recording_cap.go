// SCRUM-414: per-session recording cap, shared across the three
// SessionImport* attach handlers. Reads MAX_RECORDINGS_PER_SESSION from
// the environment (default 10) and short-circuits a 429 when the session
// is at or above the cap.
package handlers

import (
	"net/http"
	"os"
	"strconv"

	"github.com/google/uuid"
)

const defaultMaxRecordingsPerSession = 10

// maxRecordingsPerSession returns the configured cap or the default when
// the env var is unset / unparseable / non-positive.
func maxRecordingsPerSession() int {
	v := os.Getenv("MAX_RECORDINGS_PER_SESSION")
	if v == "" {
		return defaultMaxRecordingsPerSession
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		return defaultMaxRecordingsPerSession
	}
	return n
}

// enforceRecordingCap returns true when the request should be rejected
// (caller MUST stop processing and return). Writes a 429 response with
// the standard body shape on rejection. Counts only non-failed recordings
// — a session whose previous import failed cleanly is not blocked.
//
// Per SCRUM-414's documented order, call this AFTER authz + dedupe so the
// cap doesn't reject an idempotent re-import of an already-counted row.
func (h *Handlers) enforceRecordingCap(w http.ResponseWriter, r *http.Request, sessionID uuid.UUID) bool {
	cap := maxRecordingsPerSession()
	current, err := h.DB.CountActiveRecordingsForSession(r.Context(), sessionID)
	if err != nil {
		// On a count error, let the import through — a 5xx here would be
		// worse than a rare cap overshoot, and CountActiveRecordingsForSession
		// is a simple aggregate that should rarely fail in isolation.
		return false
	}
	if current < cap {
		return false
	}
	writeJSONStatus(w, http.StatusTooManyRequests, map[string]interface{}{
		"error":   "session_recording_cap_exceeded",
		"cap":     cap,
		"current": current,
	})
	return true
}
