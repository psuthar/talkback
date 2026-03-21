// Package browserurl builds SPA-safe paths that do not collide with API GET /sessions/:id (JSON).
package browserurl

import (
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// CanonicalSessionPrefix is the browser route prefix for session deep links (Mission 4+).
const CanonicalSessionPrefix = "/app/sessions"

// CanonicalSessionPath returns the relative path for a session in the SPA, e.g. /app/sessions/<uuid>.
func CanonicalSessionPath(sessionID uuid.UUID) string {
	return fmt.Sprintf("%s/%s", CanonicalSessionPrefix, sessionID.String())
}

// ParticipantSessionRedirectPath is used for first-party redirects after invite accept / register-and-accept:
// canonical path with mode=view so the participant lands in the correct UI.
func ParticipantSessionRedirectPath(sessionID uuid.UUID) string {
	return CanonicalSessionPath(sessionID) + "?mode=view"
}

// ParticipantSessionAbsoluteURL joins APP_BASE_URL (or any web app origin) with the canonical participant session path.
func ParticipantSessionAbsoluteURL(appBaseURL string, sessionID uuid.UUID) string {
	base := strings.TrimSuffix(strings.TrimSpace(appBaseURL), "/")
	return base + ParticipantSessionRedirectPath(sessionID)
}
