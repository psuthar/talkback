package handlers

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/models"
	"github.com/psuthar/talkback/internal/utils"
)

const (
	zoomAuthURL       = "https://zoom.us/oauth/authorize"
	zoomTokenURL      = "https://zoom.us/oauth/token"
	zoomScopesDefault = "cloud_recording:read:list_user_recordings cloud_recording:read:list_recording_files user:read"
)

// ZoomOAuthConfig holds Zoom OAuth app config (from env)
func zoomOAuthConfig() (clientID, clientSecret, baseURL, redirectURI string) {
	clientID = os.Getenv("ZOOM_CLIENT_ID")
	clientSecret = os.Getenv("ZOOM_CLIENT_SECRET")
	baseURL = os.Getenv("BASE_URL")
	if baseURL == "" {
		baseURL = "http://localhost:8080" // default for local dev
	}
	redirectURI = strings.TrimSuffix(baseURL, "/") + "/auth/zoom/callback"
	return clientID, clientSecret, baseURL, redirectURI
}

// ZoomTokenResponse is the response from Zoom token endpoint
type ZoomTokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	Scope        string `json:"scope"`
}

// ZoomUserResponse for /users/me
type ZoomUserResponse struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
}

// ZoomAuthStart handles GET /auth/zoom/start - redirects to Zoom OAuth
func (h *Handlers) ZoomAuthStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	clientID, _, _, redirectURI := zoomOAuthConfig()
	if clientID == "" {
		http.Error(w, "Zoom OAuth not configured (ZOOM_CLIENT_ID)", http.StatusInternalServerError)
		return
	}
	// state = creator_identity (from query so frontend can pass it)
	creatorIdentity := r.URL.Query().Get("creator_identity")
	if creatorIdentity == "" {
		// Generate a new one and pass back via redirect so frontend can store it
		creatorIdentity = uuid.New().String()
	}
	scopes := os.Getenv("ZOOM_OAUTH_SCOPES")
	if scopes == "" {
		scopes = zoomScopesDefault
	}
	authURL := fmt.Sprintf("%s?response_type=code&client_id=%s&redirect_uri=%s&state=%s&scope=%s",
		zoomAuthURL, url.QueryEscape(clientID), url.QueryEscape(redirectURI), url.QueryEscape(creatorIdentity), url.QueryEscape(scopes))
	// Redirect to app after callback with creator_identity so frontend can store it
	http.Redirect(w, r, authURL, http.StatusFound)
}

// ZoomAuthCallback handles GET /auth/zoom/callback - exchanges code for tokens
func (h *Handlers) ZoomAuthCallback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")
	if code == "" || state == "" {
		http.Redirect(w, r, "/?zoom=error&message=missing_code_or_state", http.StatusFound)
		return
	}
	creatorIdentity := state
	clientID, clientSecret, baseURL, redirectURI := zoomOAuthConfig()
	if clientID == "" || clientSecret == "" {
		http.Redirect(w, r, "/?zoom=error&message=server_not_configured", http.StatusFound)
		return
	}

	// Exchange code for tokens
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", redirectURI)
	req, err := http.NewRequest("POST", zoomTokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		log.Printf("Zoom token request build error: %v", err)
		http.Redirect(w, r, "/?zoom=error&message=exchange_failed", http.StatusFound)
		return
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.SetBasicAuth(clientID, clientSecret)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("Zoom token request error: %v", err)
		http.Redirect(w, r, "/?zoom=error&message=exchange_failed", http.StatusFound)
		return
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("Zoom token read body error: %v", err)
		http.Redirect(w, r, "/?zoom=error&message=exchange_failed", http.StatusFound)
		return
	}
	if resp.StatusCode != http.StatusOK {
		log.Printf("Zoom token error response: %s", string(body))
		http.Redirect(w, r, "/?zoom=error&message=exchange_failed", http.StatusFound)
		return
	}
	var tokenResp ZoomTokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil {
		log.Printf("Zoom token parse error: %v", err)
		http.Redirect(w, r, "/?zoom=error&message=exchange_failed", http.StatusFound)
		return
	}
	// Debug: emit access token in debug window (remove in production)
	log.Printf("[Zoom debug] access_token=%s", tokenResp.AccessToken)

	encKey := os.Getenv("ENCRYPTION_KEY")
	if encKey == "" {
		log.Printf("ENCRYPTION_KEY not set - storing tokens unencrypted is unsafe")
		encKey = "default-key-change-in-production"
	}
	accessEnc, err := utils.EncryptToken([]byte(tokenResp.AccessToken), encKey)
	if err != nil {
		log.Printf("Zoom encrypt access token error: %v", err)
		http.Redirect(w, r, "/?zoom=error&message=exchange_failed", http.StatusFound)
		return
	}
	refreshEnc, err := utils.EncryptToken([]byte(tokenResp.RefreshToken), encKey)
	if err != nil {
		log.Printf("Zoom encrypt refresh token error: %v", err)
		http.Redirect(w, r, "/?zoom=error&message=exchange_failed", http.StatusFound)
		return
	}

	expiresAt := time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	zoomUserID := "unknown"
	zoomUserEmail := ""
	// Optional: call Zoom /users/me to get email
	if tokenResp.AccessToken != "" {
		meReq, _ := http.NewRequest("GET", "https://api.zoom.us/v2/users/me", nil)
		meReq.Header.Set("Authorization", "Bearer "+tokenResp.AccessToken)
		meResp, err := http.DefaultClient.Do(meReq)
		if err == nil && meResp.StatusCode == 200 {
			var me ZoomUserResponse
			if json.NewDecoder(meResp.Body).Decode(&me) == nil {
				zoomUserID = me.ID
				zoomUserEmail = me.Email
			}
			meResp.Body.Close()
		}
	}
	var zoomUserEmailPtr *string
	if zoomUserEmail != "" {
		zoomUserEmailPtr = &zoomUserEmail
	}

	conn := &models.ZoomConnection{
		ID:                    uuid.New(),
		CreatorIdentityID:     creatorIdentity,
		ZoomUserID:            zoomUserID,
		ZoomUserEmail:         zoomUserEmailPtr,
		AccessTokenEncrypted:  accessEnc,
		RefreshTokenEncrypted: refreshEnc,
		ExpiresAt:             expiresAt,
	}
	if err := h.DB.CreateZoomConnection(r.Context(), conn); err != nil {
		log.Printf("Zoom create connection error: %v", err)
		http.Redirect(w, r, "/?zoom=error&message=save_failed", http.StatusFound)
		return
	}

	// Redirect back to app (frontend); use APP_REDIRECT_URL when API and app are different origins
	redirectTo := os.Getenv("APP_REDIRECT_URL")
	if redirectTo == "" {
		redirectTo = baseURL
	}
	redirectTo = strings.TrimSuffix(redirectTo, "/") + "/?zoom=connected&creator_identity=" + url.QueryEscape(creatorIdentity)
	http.Redirect(w, r, redirectTo, http.StatusFound)
}

// ZoomAuthDisconnect handles POST /auth/zoom/disconnect
func (h *Handlers) ZoomAuthDisconnect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	creatorIdentity := r.Header.Get("X-Creator-Identity")
	if creatorIdentity == "" {
		creatorIdentity = r.URL.Query().Get("creator_identity")
	}
	if creatorIdentity == "" {
		http.Error(w, "creator identity required (X-Creator-Identity header or creator_identity query)", http.StatusBadRequest)
		return
	}
	if err := h.DB.DeleteZoomConnectionByCreatorIdentity(r.Context(), creatorIdentity); err != nil {
		log.Printf("Zoom disconnect error: %v", err)
		http.Error(w, "failed to disconnect", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(map[string]string{"status": "disconnected"})
}

// GetValidZoomAccessToken returns a valid access token for the creator identity, refreshing if expired
func (h *Handlers) GetValidZoomAccessToken(r *http.Request, creatorIdentityID string) (accessToken string, conn *models.ZoomConnection, err error) {
	conn, err = h.DB.GetZoomConnectionByCreatorIdentity(r.Context(), creatorIdentityID)
	if err != nil || conn == nil {
		return "", nil, fmt.Errorf("zoom not connected")
	}
	encKey := os.Getenv("ENCRYPTION_KEY")
	if encKey == "" {
		encKey = "default-key-change-in-production"
	}
	accessPlain, err := utils.DecryptToken(conn.AccessTokenEncrypted, encKey)
	if err != nil {
		return "", nil, fmt.Errorf("decrypt access token: %w", err)
	}
	accessToken = string(accessPlain)
	// Refresh if expired or expiring in 5 minutes
	if time.Until(conn.ExpiresAt) < 5*time.Minute {
		refreshPlain, decErr := utils.DecryptToken(conn.RefreshTokenEncrypted, encKey)
		if decErr != nil {
			return "", nil, fmt.Errorf("decrypt refresh token: %w", decErr)
		}
		clientID, clientSecret, _, _ := zoomOAuthConfig()
		if clientID == "" || clientSecret == "" {
			return accessToken, conn, nil // return current token even if we can't refresh
		}
		form := url.Values{}
		form.Set("grant_type", "refresh_token")
		form.Set("refresh_token", string(refreshPlain))
		req, _ := http.NewRequest("POST", zoomTokenURL, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.SetBasicAuth(clientID, clientSecret)
		resp, doErr := http.DefaultClient.Do(req)
		if doErr != nil {
			return accessToken, conn, nil
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		if resp.StatusCode != 200 {
			log.Printf("Zoom token refresh failed: %s", string(body))
			return accessToken, conn, nil
		}
		var tokenResp ZoomTokenResponse
		if json.Unmarshal(body, &tokenResp) != nil {
			return accessToken, conn, nil
		}
		accessEnc, encErr := utils.EncryptToken([]byte(tokenResp.AccessToken), encKey)
		if encErr != nil {
			return accessToken, conn, nil
		}
		refreshEnc, encErr := utils.EncryptToken([]byte(tokenResp.RefreshToken), encKey)
		if encErr != nil {
			return accessToken, conn, nil
		}
		expiresAt := time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
		if updateErr := h.DB.UpdateZoomConnectionTokens(r.Context(), creatorIdentityID, accessEnc, refreshEnc, expiresAt); updateErr != nil {
			log.Printf("Zoom update tokens after refresh: %v", updateErr)
			return accessToken, conn, nil
		}
		accessToken = tokenResp.AccessToken
		conn.ExpiresAt = expiresAt
	}
	return accessToken, conn, nil
}

// ZoomAuthMe handles GET /auth/zoom/me - returns connection status
func (h *Handlers) ZoomAuthMe(w http.ResponseWriter, r *http.Request) {
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
		json.NewEncoder(w).Encode(map[string]interface{}{"connected": false, "message": "creator_identity required"})
		return
	}
	conn, err := h.DB.GetZoomConnectionByCreatorIdentity(r.Context(), creatorIdentity)
	if err != nil || conn == nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"connected": false})
		return
	}
	out := map[string]interface{}{
		"connected":       true,
		"zoom_user_id":    conn.ZoomUserID,
		"zoom_user_email": conn.ZoomUserEmail,
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(out)
}
