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

	"github.com/psuthar/talkback/internal/msgraph"
)

// TeamsStatusResponse for GET /api/teams/status
type TeamsStatusResponse struct {
	Enabled     bool   `json:"enabled"`
	Connected   bool   `json:"connected"`
	TeamsEmail  string `json:"teams_email,omitempty"`
	TeamsUserID string `json:"teams_user_id,omitempty"`
	ExpiresAt   string `json:"expires_at,omitempty"`
}

// TeamsConnectResponse for POST /api/teams/connect
type TeamsConnectResponse struct {
	AuthURL string `json:"auth_url"`
}

// TeamsRecordingsResponse for GET /api/teams/recordings
//
// SCRUM-466: items emit the normalized SPA shape (meeting_topic,
// meeting_uuid, instance_uuid, etc.) so the unified RecordingsPicker
// reads every platform the same way. The internal msgraph.RecordingListItem
// (subject / meeting_id / recording_id) is still used by the pipeline.
type TeamsRecordingsResponse struct {
	Items       []NormalizedRecordingListItem `json:"items"`
	Diagnostics *msgraph.RecordingsListDiag   `json:"diagnostics,omitempty"`
}

// normalizeTeamsItem maps an msgraph.RecordingListItem into the
// normalized SPA shape. Subject becomes the title (fallback to date for
// untitled); MeetingID + RecordingID become the meeting+instance UUIDs.
// has_transcript can't be cheaply determined from the listing — set
// false; the pipeline resolves it during ingest.
func normalizeTeamsItem(it msgraph.RecordingListItem) NormalizedRecordingListItem {
	title := strings.TrimSpace(it.Subject)
	if title == "" {
		title = "Teams recording " + it.StartTime
	}
	return NormalizedRecordingListItem{
		MeetingTopic:   title,
		StartTime:      it.StartTime,
		MeetingUUID:    it.MeetingID,
		InstanceUUID:   it.RecordingID,
		HasVideo:       true,
		HasTranscript:  false,
		RecordingCount: 1,
	}
}

// TeamsAPIStatus returns whether Teams is enabled and connection status.
func (h *Handlers) TeamsAPIStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !teamsEnabled() {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(TeamsStatusResponse{Enabled: false, Connected: false})
		return
	}
	creatorIdentity := r.Header.Get("X-Creator-Identity")
	if creatorIdentity == "" {
		creatorIdentity = r.URL.Query().Get("creator_identity")
	}
	// No creator identity yet (e.g. before login sync): still report enabled=true so the SPA can show "From Teams".
	if creatorIdentity == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(TeamsStatusResponse{Enabled: true, Connected: false})
		return
	}
	conn, err := h.DB.GetTeamsConnectionByCreatorIdentity(r.Context(), creatorIdentity)
	if err != nil || conn == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(TeamsStatusResponse{Enabled: true, Connected: false})
		return
	}
	email := ""
	if conn.TeamsUserEmail != nil {
		email = *conn.TeamsUserEmail
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(TeamsStatusResponse{
		Enabled:     true,
		Connected:   true,
		TeamsEmail:  email,
		TeamsUserID: conn.TeamsUserID,
		ExpiresAt:   conn.ExpiresAt.Format(time.RFC3339),
	})
}

// TeamsAPIConnect returns JSON with auth URL for SPA (mirrors ZoomAPIConnect).
func (h *Handlers) TeamsAPIConnect(w http.ResponseWriter, r *http.Request) {
	if !teamsEnabled() {
		http.Error(w, "Teams integration disabled", http.StatusNotFound)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	clientID, _, tenantID, _, redirectURI, err := teamsOAuthConfig()
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"message": "Teams OAuth misconfigured: " + err.Error()})
		return
	}
	if clientID == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"message": "Teams OAuth not configured"})
		return
	}
	creatorIdentity := r.Header.Get("X-Creator-Identity")
	if creatorIdentity == "" {
		creatorIdentity = r.URL.Query().Get("creator_identity")
	}
	if creatorIdentity == "" {
		creatorIdentity = fmt.Sprintf("creator-%d", time.Now().UnixNano())
	}
	scopes := strings.TrimSpace(os.Getenv("TEAMS_OAUTH_SCOPES"))
	if scopes == "" {
		scopes = teamsScopesDefault
	}
	params := url.Values{}
	params.Set("client_id", clientID)
	params.Set("response_type", "code")
	params.Set("redirect_uri", redirectURI)
	params.Set("response_mode", "query")
	params.Set("scope", scopes)
	params.Set("state", creatorIdentity)
	authURL := teamsAuthorizeURL(tenantID) + "?" + params.Encode()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(TeamsConnectResponse{AuthURL: authURL})
}

// TeamsAPIDisconnect removes Teams tokens for the creator.
func (h *Handlers) TeamsAPIDisconnect(w http.ResponseWriter, r *http.Request) {
	if !teamsEnabled() {
		http.Error(w, "Teams integration disabled", http.StatusNotFound)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	creatorIdentity := r.Header.Get("X-Creator-Identity")
	if creatorIdentity == "" {
		creatorIdentity = r.URL.Query().Get("creator_identity")
	}
	if creatorIdentity == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"code": "teams_identity_required", "message": "creator_identity required"})
		return
	}
	if err := h.DB.DeleteTeamsConnectionByCreatorIdentity(r.Context(), creatorIdentity); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"message": "failed to disconnect"})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// TeamsAPIRecordings lists Teams recordings via Microsoft Graph (best-effort).
func (h *Handlers) TeamsAPIRecordings(w http.ResponseWriter, r *http.Request) {
	if !teamsEnabled() {
		http.Error(w, "Teams integration disabled", http.StatusNotFound)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	creatorIdentity := r.Header.Get("X-Creator-Identity")
	if creatorIdentity == "" {
		creatorIdentity = r.URL.Query().Get("creator_identity")
	}
	if creatorIdentity == "" {
		log.Printf("[teams] TeamsAPIRecordings: no creator_identity in header or query — returning 401")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"code": "teams_not_connected", "message": "creator_identity required"})
		return
	}
	log.Printf("[teams] TeamsAPIRecordings: fetching token for creator_identity=%q", creatorIdentity)
	accessToken, conn, err := h.GetValidTeamsAccessTokenContext(r.Context(), creatorIdentity)
	if err != nil || conn == nil {
		log.Printf("[teams] TeamsAPIRecordings: token fetch failed — err=%v conn_nil=%v; returning 401 (check Teams connection for this identity)", err, conn == nil)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"code": "teams_not_connected", "message": "Teams not connected. Connect Microsoft Teams first."})
		return
	}
	log.Printf("[teams] TeamsAPIRecordings: token OK len=%d expires_at=%s", len(accessToken), conn.ExpiresAt.Format(time.RFC3339))
	debug := r.URL.Query().Get("debug") == "1" || r.URL.Query().Get("debug") == "true"
	if debug {
		log.Printf("[teams] TeamsAPIRecordings: debug mode — calling ListRecordingsDetailed")
		items, diag, err := msgraph.ListRecordingsDetailed(r.Context(), accessToken, msgraph.DefaultHTTPClient())
		if err != nil {
			log.Printf("[teams] TeamsAPIRecordings: ListRecordingsDetailed error: %v", err)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"message": fmt.Sprintf("Failed to list recordings: %v", err)})
			return
		}
		log.Printf("[teams] TeamsAPIRecordings: ListRecordingsDetailed returned %d items; diag=%+v", len(items), diag)
		normalized := make([]NormalizedRecordingListItem, 0, len(items))
		for _, it := range items {
			normalized = append(normalized, normalizeTeamsItem(it))
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(TeamsRecordingsResponse{Items: normalized, Diagnostics: &diag})
		return
	}
	log.Printf("[teams] TeamsAPIRecordings: calling ListRecordings")
	items, err := msgraph.ListRecordings(r.Context(), accessToken, msgraph.DefaultHTTPClient())
	if err != nil {
		log.Printf("[teams] TeamsAPIRecordings: ListRecordings error: %v", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"message": fmt.Sprintf("Failed to list recordings: %v", err)})
		return
	}
	log.Printf("[teams] TeamsAPIRecordings: returning %d items to client", len(items))
	normalized := make([]NormalizedRecordingListItem, 0, len(items))
	for _, it := range items {
		normalized = append(normalized, normalizeTeamsItem(it))
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(TeamsRecordingsResponse{Items: normalized})
}
