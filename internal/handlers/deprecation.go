// SCRUM-416: shared helpers for the deprecation signals on the legacy
// create-new import endpoints (POST /api/zoom/import, /api/meet/import,
// /api/teams/import). The attach-to-existing-session endpoints
// (/api/sessions/:id/import/{zoom,google-meet,teams}) are the canonical
// path for the multi-recording era. These helpers add the Deprecation /
// Sunset / Link response headers and emit a structured log line on every
// call so SCRUM-XX17 can verify zero post-removal traffic before deleting
// the handlers.
package handlers

import (
	"log"
	"net/http"
	"os"
	"time"
)

// LegacyImportSunsetDate is the date by which the legacy create-new
// endpoints will be removed. SCRUM-428 brings the removal forward; the
// originally-published date was Fri, 29 May 2026 but the removal ships
// today (Thu, 21 May 2026), so the Sunset header is updated to match
// the actual cutover. Override at runtime via the
// LEGACY_IMPORT_SUNSET_DATE env var (RFC1123 format).
const defaultLegacyImportSunsetDate = "Thu, 21 May 2026 00:00:00 GMT"

// legacyImportSunsetHeader returns the value for the Sunset response
// header. Env-override allows the deployment to push the removal date
// without a code change when the calendar slips.
func legacyImportSunsetHeader() string {
	if v := os.Getenv("LEGACY_IMPORT_SUNSET_DATE"); v != "" {
		// We don't try to parse — RFC1123 is what we send; if the operator
		// sets a different shape they get exactly what they typed.
		return v
	}
	return defaultLegacyImportSunsetDate
}

// markLegacyImportDeprecated writes the trio of deprecation headers and
// logs the structured DEPRECATED_ENDPOINT_HIT line. Call this at the top
// of each legacy create-new handler so EVERY response (including 4xx /
// 5xx) carries the signal.
//
// SCRUM-416 contract:
//   - Deprecation: true
//   - Sunset: <RFC1123 date string>
//   - Link: <url-to-attach-endpoint>; rel="alternate"
//   - log line: event=DEPRECATED_ENDPOINT_HIT path=<path> alternate=<url>
func markLegacyImportDeprecated(w http.ResponseWriter, r *http.Request, attachEndpoint string) {
	w.Header().Set("Deprecation", "true")
	w.Header().Set("Sunset", legacyImportSunsetHeader())
	w.Header().Set("Link", `<`+attachEndpoint+`>; rel="alternate"`)
	log.Printf("event=DEPRECATED_ENDPOINT_HIT path=%s method=%s alternate=%s sunset=%q remote=%s ts=%s",
		r.URL.Path, r.Method, attachEndpoint, legacyImportSunsetHeader(),
		r.RemoteAddr, time.Now().UTC().Format(time.RFC3339))
}
