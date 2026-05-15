// SCRUM-418: GET /api/integrations/status — single endpoint the SPA can hit
// to discover, per platform: whether the backend has the integration
// enabled (env flag) and whether the current user has a stored OAuth
// connection. The Add Content tiles + Manage Connections modal both
// consume this so a tile is never rendered for a disabled integration.
package handlers

import (
	"encoding/json"
	"net/http"
	"os"
)

// IntegrationStatus is the per-platform sub-object.
type IntegrationStatus struct {
	Enabled      bool    `json:"enabled"`
	Connected    bool    `json:"connected"`
	AccountEmail *string `json:"account_email"`
}

// IntegrationsStatusResponse is the shape of GET /api/integrations/status.
type IntegrationsStatusResponse struct {
	Zoom       IntegrationStatus `json:"zoom"`
	GoogleMeet IntegrationStatus `json:"google_meet"`
	Teams      IntegrationStatus `json:"teams"`
}

// zoomEnabled mirrors googleMeetEnabled / teamsEnabled. Unlike Meet/Teams
// which default to off, Zoom is the legacy default-on integration; an
// unset ENABLE_ZOOM env is therefore treated as "true". Only an explicit
// ENABLE_ZOOM=false disables it.
func zoomEnabled() bool {
	v := os.Getenv("ENABLE_ZOOM")
	if v == "" {
		return true
	}
	return v != "false"
}

// IntegrationsStatus handles GET /api/integrations/status.
func (h *Handlers) IntegrationsStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONStatus(w, http.StatusMethodNotAllowed, map[string]string{"message": "method not allowed"})
		return
	}
	user := UserFromContext(r.Context())
	if user == nil {
		writeJSONStatus(w, http.StatusUnauthorized, map[string]string{"message": "unauthorized"})
		return
	}
	resp := IntegrationsStatusResponse{
		Zoom:       IntegrationStatus{Enabled: zoomEnabled()},
		GoogleMeet: IntegrationStatus{Enabled: googleMeetEnabled()},
		Teams:      IntegrationStatus{Enabled: teamsEnabled()},
	}
	identity := user.Email

	if resp.Zoom.Enabled {
		if conn, _ := h.DB.GetZoomConnectionByCreatorIdentity(r.Context(), identity); conn != nil {
			resp.Zoom.Connected = true
			if conn.ZoomUserEmail != nil {
				e := *conn.ZoomUserEmail
				resp.Zoom.AccountEmail = &e
			}
		}
	}
	if resp.GoogleMeet.Enabled {
		if conn, _ := h.DB.GetGoogleMeetConnectionByCreatorIdentity(r.Context(), identity); conn != nil {
			resp.GoogleMeet.Connected = true
			if conn.GoogleUserEmail != nil {
				e := *conn.GoogleUserEmail
				resp.GoogleMeet.AccountEmail = &e
			}
		}
	}
	if resp.Teams.Enabled {
		if conn, _ := h.DB.GetTeamsConnectionByCreatorIdentity(r.Context(), identity); conn != nil {
			resp.Teams.Connected = true
			if conn.TeamsUserEmail != nil {
				e := *conn.TeamsUserEmail
				resp.Teams.AccountEmail = &e
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(resp)
}
