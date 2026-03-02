package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/psuthar/talkback/internal/utils"
)

// ZoomStatusResponse for GET /api/zoom/status
type ZoomStatusResponse struct {
	Connected   bool   `json:"connected"`
	ZoomEmail   string `json:"zoom_email,omitempty"`
	ZoomUserID  string `json:"zoom_user_id,omitempty"`
	ExpiresAt   string `json:"expires_at,omitempty"`
}

// ZoomConnectResponse for POST /api/zoom/connect
type ZoomConnectResponse struct {
	AuthURL string `json:"auth_url"`
}

// ZoomRecordingsResponse for GET /api/zoom/recordings
type ZoomRecordingsResponse struct {
	Items []utils.RecordingListItem `json:"items"`
}

// ZoomAPIStatus returns Zoom connection status. X-Creator-Identity required.
func (h *Handlers) ZoomAPIStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
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
		json.NewEncoder(w).Encode(map[string]string{"code": "zoom_not_connected", "message": "creator_identity required"})
		return
	}
	conn, err := h.DB.GetZoomConnectionByCreatorIdentity(r.Context(), creatorIdentity)
	if err != nil || conn == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(ZoomStatusResponse{Connected: false})
		return
	}
	email := ""
	if conn.ZoomUserEmail != nil {
		email = *conn.ZoomUserEmail
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(ZoomStatusResponse{
		Connected:  true,
		ZoomEmail:  email,
		ZoomUserID: conn.ZoomUserID,
		ExpiresAt:  conn.ExpiresAt.Format(time.RFC3339),
	})
}

// ZoomAPIConnect returns auth URL for OAuth flow. X-Creator-Identity optional (generated if omitted).
func (h *Handlers) ZoomAPIConnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	clientID, _, baseURL, redirectURI, err := zoomOAuthConfig()
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"message": "Zoom OAuth redirect URL misconfigured: " + err.Error()})
		return
	}
	if clientID == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"message": "Zoom OAuth not configured"})
		return
	}
	creatorIdentity := r.Header.Get("X-Creator-Identity")
	if creatorIdentity == "" {
		creatorIdentity = r.URL.Query().Get("creator_identity")
	}
	if creatorIdentity == "" {
		creatorIdentity = fmt.Sprintf("creator-%d", time.Now().UnixNano())
	}
	scopes := os.Getenv("ZOOM_OAUTH_SCOPES")
	if scopes == "" {
		scopes = zoomScopesDefault
	}
	params := url.Values{}
	params.Set("response_type", "code")
	params.Set("client_id", clientID)
	params.Set("redirect_uri", redirectURI)
	params.Set("state", creatorIdentity)
	params.Set("scope", scopes)
	authURL := zoomAuthURL + "?" + params.Encode()
	_ = baseURL
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(ZoomConnectResponse{AuthURL: authURL})
}

// ZoomAPIDisconnect marks Zoom disconnected and deletes tokens. Returns 204.
func (h *Handlers) ZoomAPIDisconnect(w http.ResponseWriter, r *http.Request) {
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
		json.NewEncoder(w).Encode(map[string]string{"code": "zoom_not_connected", "message": "creator_identity required"})
		return
	}
	if err := h.DB.DeleteZoomConnectionByCreatorIdentity(r.Context(), creatorIdentity); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"message": "failed to disconnect"})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ZoomAPIRecordings returns a list of Zoom recordings for the connected user.
// Query: from=YYYY-MM-DD, to=YYYY-MM-DD, has_transcript=true|false, q=string (optional search).
func (h *Handlers) ZoomAPIRecordings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
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
		json.NewEncoder(w).Encode(map[string]string{"code": "zoom_not_connected", "message": "creator_identity required"})
		return
	}
	accessToken, conn, err := h.GetValidZoomAccessToken(r, creatorIdentity)
	if err != nil || conn == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"code": "zoom_not_connected", "message": "Zoom not connected. Connect Zoom first."})
		return
	}
	from := r.URL.Query().Get("from")
	to := r.URL.Query().Get("to")
	var items []utils.RecordingListItem
	if from == "" && to == "" {
		// Fetch all recordings (chunked; Zoom API has 1-month limit per call)
		items, err = utils.ListUserRecordingsAll(accessToken, "me")
	} else {
		if from == "" {
			from = time.Now().AddDate(0, 0, -14).Format("2006-01-02")
		}
		if to == "" {
			to = time.Now().Format("2006-01-02")
		}
		items, err = utils.ListUserRecordings(accessToken, "me", from, to)
	}
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]string{"message": fmt.Sprintf("Failed to list recordings: %v", err)})
		return
	}
	// Filter by has_transcript if requested
	hasTranscriptParam := r.URL.Query().Get("has_transcript")
	if hasTranscriptParam == "true" {
		filtered := items[:0]
		for _, it := range items {
			if it.HasTranscript {
				filtered = append(filtered, it)
			}
		}
		items = filtered
	}
	// Optional search filter q (on meeting_topic)
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q != "" {
		qLower := strings.ToLower(q)
		filtered := items[:0]
		for _, it := range items {
			if strings.Contains(strings.ToLower(it.MeetingTopic), qLower) {
				filtered = append(filtered, it)
			}
		}
		items = filtered
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(ZoomRecordingsResponse{Items: items})
}
