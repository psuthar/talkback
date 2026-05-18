package handlers

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/psuthar/talkback/internal/googlemeet"
)

// GoogleMeetStatusResponse is the response for GET /api/google-meet/status.
type GoogleMeetStatusResponse struct {
	Enabled           bool   `json:"enabled"`
	Connected         bool   `json:"connected"`
	GoogleEmail       string `json:"google_email,omitempty"`
	GoogleUserID      string `json:"google_user_id,omitempty"`
	WorkspaceEligible *bool  `json:"workspace_eligible,omitempty"`
	ExpiresAt         string `json:"expires_at,omitempty"`
}

// GoogleMeetConnectResponse is the response for POST /api/google-meet/connect.
type GoogleMeetConnectResponse struct {
	AuthURL string `json:"auth_url"`
}

// GoogleMeetAPIStatus is always-registered (mirrors /api/teams/status). When the flag is off,
// returns {enabled:false, connected:false} so the SPA can degrade gracefully.
func (h *Handlers) GoogleMeetAPIStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !googleMeetEnabled() {
		writeJSONStatus(w, http.StatusOK, GoogleMeetStatusResponse{Enabled: false, Connected: false})
		return
	}
	creatorIdentity := readCreatorIdentity(r)
	if creatorIdentity == "" {
		// Enabled but no identity yet: SPA can still render the "From Google Meet" tile.
		writeJSONStatus(w, http.StatusOK, GoogleMeetStatusResponse{Enabled: true, Connected: false})
		return
	}
	conn, err := h.DB.GetGoogleMeetConnectionByCreatorIdentity(r.Context(), creatorIdentity)
	if err != nil || conn == nil {
		writeJSONStatus(w, http.StatusOK, GoogleMeetStatusResponse{Enabled: true, Connected: false})
		return
	}
	resp := GoogleMeetStatusResponse{
		Enabled:           true,
		Connected:         true,
		GoogleUserID:      conn.GoogleUserID,
		WorkspaceEligible: conn.WorkspaceEligible,
		ExpiresAt:         conn.ExpiresAt.Format(time.RFC3339),
	}
	if conn.GoogleUserEmail != nil {
		resp.GoogleEmail = *conn.GoogleUserEmail
	}
	writeJSONStatus(w, http.StatusOK, resp)
}

// GoogleMeetAPIConnect returns the OAuth authorize URL the SPA should redirect to.
func (h *Handlers) GoogleMeetAPIConnect(w http.ResponseWriter, r *http.Request) {
	if !googleMeetEnabled() {
		http.Error(w, "Google Meet integration disabled", http.StatusNotFound)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	clientID, _, _, redirectURI, err := googleMeetOAuthConfig()
	if err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]string{"message": "Google Meet OAuth misconfigured: " + err.Error()})
		return
	}
	if clientID == "" {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]string{"message": "Google Meet OAuth not configured"})
		return
	}
	creatorIdentity := readCreatorIdentity(r)
	if creatorIdentity == "" {
		creatorIdentity = fmt.Sprintf("creator-%d", time.Now().UnixNano())
	}
	scopes := strings.TrimSpace(os.Getenv("GOOGLE_MEET_OAUTH_SCOPES"))
	if scopes == "" {
		scopes = googleMeetScopesDefault
	}
	params := url.Values{}
	params.Set("client_id", clientID)
	params.Set("response_type", "code")
	params.Set("redirect_uri", redirectURI)
	params.Set("scope", scopes)
	params.Set("state", creatorIdentity)
	params.Set("access_type", "offline")
	params.Set("prompt", "consent")
	params.Set("include_granted_scopes", "true")
	writeJSONStatus(w, http.StatusOK, GoogleMeetConnectResponse{AuthURL: googleMeetAuthorizeURL + "?" + params.Encode()})
}

// GoogleMeetAPIDisconnect deletes the connection (idempotent).
func (h *Handlers) GoogleMeetAPIDisconnect(w http.ResponseWriter, r *http.Request) {
	if !googleMeetEnabled() {
		http.Error(w, "Google Meet integration disabled", http.StatusNotFound)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	creatorIdentity := readCreatorIdentity(r)
	if creatorIdentity == "" {
		writeJSONStatus(w, http.StatusUnauthorized, map[string]string{"code": "google_meet_identity_required", "message": "creator_identity required"})
		return
	}
	if err := h.DB.DeleteGoogleMeetConnectionByCreatorIdentity(r.Context(), creatorIdentity); err != nil {
		writeJSONStatus(w, http.StatusInternalServerError, map[string]string{"message": "failed to disconnect"})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GoogleMeetRecordingsResponse is the response for GET /api/google-meet/recordings.
//
// SCRUM-466: items are normalized to the Zoom-shaped fields the SPA's
// RecordingsPicker reads uniformly (meeting_topic, start_time,
// duration_minutes, meeting_uuid, instance_uuid, has_transcript). The
// internal googlemeet.RecordingListItem (subject / conference_record_name /
// recording_name) is still used by the pipeline; this is a response-layer
// shim so the picker doesn't have to special-case per platform.
type GoogleMeetRecordingsResponse struct {
	Items []NormalizedRecordingListItem `json:"items"`
}

// NormalizedRecordingListItem is the SPA-facing shape every recordings
// endpoint emits regardless of platform.
type NormalizedRecordingListItem struct {
	MeetingTopic    string `json:"meeting_topic"`
	StartTime       string `json:"start_time"`
	DurationMinutes int    `json:"duration_minutes"`
	MeetingUUID     string `json:"meeting_uuid"`
	InstanceUUID    string `json:"instance_uuid,omitempty"`
	HasVideo        bool   `json:"has_video"`
	HasTranscript   bool   `json:"has_transcript"`
	RecordingCount  int    `json:"recording_count"`
}

// normalizeMeetItem maps a Google Meet RecordingListItem into the
// normalized SPA shape. Subject becomes the title (with a date fallback
// for untitled meetings); ConferenceRecordName + RecordingName become
// the meeting+instance UUIDs the picker uses as opaque external IDs;
// TranscriptState=="ready" sets has_transcript.
func normalizeMeetItem(it googlemeet.RecordingListItem) NormalizedRecordingListItem {
	title := strings.TrimSpace(it.Subject)
	if title == "" {
		title = "Meet recording " + it.StartTime
	}
	return NormalizedRecordingListItem{
		MeetingTopic:   title,
		StartTime:      it.StartTime,
		MeetingUUID:    it.ConferenceRecordName,
		InstanceUUID:   it.RecordingName,
		HasVideo:       it.DriveFileID != "" || it.ExportURI != "",
		HasTranscript:  it.TranscriptState == "ready",
		RecordingCount: 1,
	}
}

// GoogleMeetAPIRecordings lists Meet recordings the user can import. If the
// connection's granted_scope is missing drive.readonly, returns 401 with code
// meet_missing_scopes so the SPA can prompt reconnect.
func (h *Handlers) GoogleMeetAPIRecordings(w http.ResponseWriter, r *http.Request) {
	if !googleMeetEnabled() {
		http.Error(w, "Google Meet integration disabled", http.StatusNotFound)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	creatorIdentity := readCreatorIdentity(r)
	if creatorIdentity == "" {
		writeJSONStatus(w, http.StatusUnauthorized, map[string]string{"code": "google_meet_not_connected", "message": "creator_identity required"})
		return
	}
	accessToken, conn, err := h.GetValidGoogleMeetAccessTokenContext(r.Context(), creatorIdentity)
	if err != nil || conn == nil {
		log.Printf("[google_meet] recordings token fetch failed err=%v conn_nil=%v", err, conn == nil)
		writeJSONStatus(w, http.StatusUnauthorized, map[string]string{"code": "google_meet_not_connected", "message": "Google Meet not connected. Connect Google Meet first."})
		return
	}
	if conn.GrantedScope != nil && !strings.Contains(*conn.GrantedScope, "drive.readonly") {
		writeJSONStatus(w, http.StatusUnauthorized, map[string]string{"code": "meet_missing_scopes", "message": "Reconnect Google Meet and accept Drive read-only access"})
		return
	}
	items, err := googlemeet.ListRecordingsAll(r.Context(), accessToken, googlemeet.DefaultHTTPClient())
	if err != nil {
		log.Printf("[google_meet] ListRecordingsAll error: %v", err)
		writeJSONStatus(w, http.StatusInternalServerError, map[string]string{"message": "Failed to list Google Meet recordings"})
		return
	}
	normalized := make([]NormalizedRecordingListItem, 0, len(items))
	for _, it := range items {
		normalized = append(normalized, normalizeMeetItem(it))
	}
	writeJSONStatus(w, http.StatusOK, GoogleMeetRecordingsResponse{Items: normalized})
}

// readCreatorIdentity returns the creator identity from the X-Creator-Identity header
// or the creator_identity query string. Empty if neither is set.
func readCreatorIdentity(r *http.Request) string {
	v := r.Header.Get("X-Creator-Identity")
	if v == "" {
		v = r.URL.Query().Get("creator_identity")
	}
	return v
}

// writeJSONStatus writes a JSON body with the given status. Mirrors writeJSONError shape.
func writeJSONStatus(w http.ResponseWriter, status int, body interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

