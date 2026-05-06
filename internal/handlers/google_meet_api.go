package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
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

