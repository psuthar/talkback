package handlers

import (
	"context"
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

func teamsEnabled() bool {
	return os.Getenv("ENABLE_TEAMS") == "true"
}

func teamsOAuthConfig() (clientID, clientSecret, tenantID, baseURL, redirectURI string, err error) {
	clientID = strings.TrimSpace(os.Getenv("TEAMS_CLIENT_ID"))
	clientSecret = strings.TrimSpace(os.Getenv("TEAMS_CLIENT_SECRET"))
	tenantID = strings.TrimSpace(os.Getenv("TEAMS_TENANT_ID"))
	if tenantID == "" {
		tenantID = "common"
	}
	baseURL = strings.TrimSpace(os.Getenv("APP_BASE_URL"))
	if baseURL == "" {
		baseURL = strings.TrimSpace(os.Getenv("BASE_URL"))
	}
	baseURL = strings.TrimSuffix(baseURL, "/")
	if baseURL == "" && os.Getenv("ENV") != "production" {
		baseURL = "http://localhost:8080"
	}
	redirectURI = strings.TrimSpace(os.Getenv("TEAMS_REDIRECT_URL"))
	if redirectURI == "" && os.Getenv("ENV") != "production" {
		redirectURI = "http://localhost:8081/auth/teams/callback"
	}
	if redirectURI != "" {
		redirectURI, err = utils.RequireAbsoluteURL("TEAMS_REDIRECT_URL", redirectURI)
		if err != nil {
			return "", "", "", "", "", err
		}
	} else {
		return "", "", "", "", "", fmt.Errorf("TEAMS_REDIRECT_URL must be set (e.g. https://your-api.onrender.com/auth/teams/callback)")
	}
	return clientID, clientSecret, tenantID, baseURL, redirectURI, nil
}

func teamsAuthorizeURL(tenantID string) string {
	return fmt.Sprintf("https://login.microsoftonline.com/%s/oauth2/v2.0/authorize", url.PathEscape(tenantID))
}

func teamsTokenURL(tenantID string) string {
	return fmt.Sprintf("https://login.microsoftonline.com/%s/oauth2/v2.0/token", url.PathEscape(tenantID))
}

// Default Teams delegated scopes (override with TEAMS_OAUTH_SCOPES).
// Use Graph permission names as scope values: *.Read (without .All) is not valid — see Microsoft Graph permissions reference.
// Calendars.Read: discover Teams meetings via calendar (getAllRecordings is not supported in delegated context on many tenants).
const teamsScopesDefault = "openid offline_access User.Read Calendars.Read Files.Read OnlineMeetings.Read OnlineMeetingRecording.Read.All OnlineMeetingTranscript.Read.All"

// MicrosoftTokenResponse is the token endpoint JSON.
type MicrosoftTokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
}

// TeamsGraphMe is a subset of GET https://graph.microsoft.com/v1.0/me
type TeamsGraphMe struct {
	ID    string `json:"id"`
	Mail  string `json:"mail"`
	Email string `json:"userPrincipalName"`
}

// TeamsAuthStart handles GET /auth/teams/start — redirects to Microsoft OAuth.
func (h *Handlers) TeamsAuthStart(w http.ResponseWriter, r *http.Request) {
	if !teamsEnabled() {
		http.Error(w, "Teams integration disabled", http.StatusNotFound)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	clientID, _, tenantID, _, redirectURI, err := teamsOAuthConfig()
	if err != nil {
		log.Printf("Teams OAuth config: %v", err)
		http.Error(w, "Teams OAuth redirect URL misconfigured: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if clientID == "" {
		http.Error(w, "Teams OAuth not configured (TEAMS_CLIENT_ID)", http.StatusInternalServerError)
		return
	}
	creatorIdentity := r.URL.Query().Get("creator_identity")
	if creatorIdentity == "" {
		creatorIdentity = uuid.New().String()
	}
	scopes := os.Getenv("TEAMS_OAUTH_SCOPES")
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
	http.Redirect(w, r, authURL, http.StatusFound)
}

// TeamsAuthCallback handles GET /auth/teams/callback.
func (h *Handlers) TeamsAuthCallback(w http.ResponseWriter, r *http.Request) {
	if !teamsEnabled() {
		http.Error(w, "Teams integration disabled", http.StatusNotFound)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")
	if code == "" || state == "" {
		http.Redirect(w, r, "/?teams=error&message=missing_code_or_state", http.StatusFound)
		return
	}
	creatorIdentity := state
	clientID, clientSecret, tenantID, baseURL, redirectURI, err := teamsOAuthConfig()
	if err != nil {
		log.Printf("Teams OAuth config: %v", err)
		http.Redirect(w, r, "/?teams=error&message=redirect_misconfigured", http.StatusFound)
		return
	}
	if clientID == "" || clientSecret == "" {
		http.Redirect(w, r, "/?teams=error&message=server_not_configured", http.StatusFound)
		return
	}
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", redirectURI)
	form.Set("client_id", clientID)
	form.Set("client_secret", clientSecret)
	req, err := http.NewRequest("POST", teamsTokenURL(tenantID), strings.NewReader(form.Encode()))
	if err != nil {
		http.Redirect(w, r, "/?teams=error&message=exchange_failed", http.StatusFound)
		return
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("Teams token request error: %v", err)
		http.Redirect(w, r, "/?teams=error&message=exchange_failed", http.StatusFound)
		return
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil || resp.StatusCode != http.StatusOK {
		log.Printf("Teams token error status=%d body=%s", resp.StatusCode, truncateTeams(string(body), 800))
		http.Redirect(w, r, "/?teams=error&message=exchange_failed", http.StatusFound)
		return
	}
	var tokenResp MicrosoftTokenResponse
	if err := json.Unmarshal(body, &tokenResp); err != nil || tokenResp.AccessToken == "" {
		http.Redirect(w, r, "/?teams=error&message=exchange_failed", http.StatusFound)
		return
	}
	encKey := os.Getenv("ENCRYPTION_KEY")
	if encKey == "" {
		encKey = "default-key-change-in-production"
	}
	accessEnc, err := utils.EncryptToken([]byte(tokenResp.AccessToken), encKey)
	if err != nil {
		http.Redirect(w, r, "/?teams=error&message=exchange_failed", http.StatusFound)
		return
	}
	refreshEnc, err := utils.EncryptToken([]byte(tokenResp.RefreshToken), encKey)
	if err != nil {
		http.Redirect(w, r, "/?teams=error&message=exchange_failed", http.StatusFound)
		return
	}
	expiresAt := time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)

	teamsUserID := "unknown"
	var teamsEmail *string
	if tokenResp.AccessToken != "" {
		meReq, _ := http.NewRequest("GET", "https://graph.microsoft.com/v1.0/me", nil)
		meReq.Header.Set("Authorization", "Bearer "+tokenResp.AccessToken)
		meResp, err := http.DefaultClient.Do(meReq)
		if err == nil && meResp.StatusCode == 200 {
			var me TeamsGraphMe
			if json.NewDecoder(meResp.Body).Decode(&me) == nil {
				teamsUserID = me.ID
				em := strings.TrimSpace(me.Mail)
				if em == "" {
					em = strings.TrimSpace(me.Email)
				}
				if em != "" {
					teamsEmail = &em
				}
			}
			meResp.Body.Close()
		}
	}

	conn := &models.TeamsConnection{
		ID:                    uuid.New(),
		CreatorIdentityID:     creatorIdentity,
		TenantID:              tenantID,
		TeamsUserID:           teamsUserID,
		TeamsUserEmail:        teamsEmail,
		AccessTokenEncrypted:  accessEnc,
		RefreshTokenEncrypted: refreshEnc,
		ExpiresAt:             expiresAt,
	}
	if err := h.DB.CreateTeamsConnection(r.Context(), conn); err != nil {
		log.Printf("Teams create connection error: %v", err)
		http.Redirect(w, r, "/?teams=error&message=save_failed", http.StatusFound)
		return
	}
	redirectTo := os.Getenv("APP_REDIRECT_URL")
	if redirectTo == "" {
		redirectTo = baseURL
	}
	redirectTo = strings.TrimSuffix(redirectTo, "/") + "/?teams=connected&creator_identity=" + url.QueryEscape(creatorIdentity)
	http.Redirect(w, r, redirectTo, http.StatusFound)
}

func truncateTeams(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// GetValidTeamsAccessTokenContext returns a valid Graph access token, refreshing when near expiry.
func (h *Handlers) GetValidTeamsAccessTokenContext(ctx context.Context, creatorIdentityID string) (accessToken string, conn *models.TeamsConnection, err error) {
	conn, err = h.DB.GetTeamsConnectionByCreatorIdentity(ctx, creatorIdentityID)
	if err != nil || conn == nil {
		return "", nil, fmt.Errorf("teams not connected")
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
	needsRefresh := time.Until(conn.ExpiresAt) < 5*time.Minute
	if !needsRefresh {
		return accessToken, conn, nil
	}
	refreshPlain, decErr := utils.DecryptToken(conn.RefreshTokenEncrypted, encKey)
	if decErr != nil {
		return "", nil, fmt.Errorf("decrypt refresh token: %w", decErr)
	}
	clientID, clientSecret, tenantID, _, _, cfgErr := teamsOAuthConfig()
	if cfgErr != nil || clientID == "" || clientSecret == "" {
		return accessToken, conn, nil
	}
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", string(refreshPlain))
	form.Set("client_id", clientID)
	form.Set("client_secret", clientSecret)
	req, _ := http.NewRequestWithContext(ctx, "POST", teamsTokenURL(tenantID), strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, doErr := http.DefaultClient.Do(req)
	if doErr != nil {
		return "", nil, fmt.Errorf("teams token refresh request failed: %w", doErr)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", nil, fmt.Errorf("teams token refresh failed: status %d: %s", resp.StatusCode, truncateTeams(string(body), 400))
	}
	var tr MicrosoftTokenResponse
	if json.Unmarshal(body, &tr) != nil || tr.AccessToken == "" {
		return "", nil, fmt.Errorf("teams token refresh: invalid response")
	}
	newAccessEnc, err := utils.EncryptToken([]byte(tr.AccessToken), encKey)
	if err != nil {
		return "", nil, err
	}
	refreshEnc := conn.RefreshTokenEncrypted
	if tr.RefreshToken != "" {
		if re, err := utils.EncryptToken([]byte(tr.RefreshToken), encKey); err == nil {
			refreshEnc = re
		}
	}
	expiresAt := time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second)
	if err := h.DB.UpdateTeamsConnectionTokens(ctx, creatorIdentityID, newAccessEnc, refreshEnc, expiresAt); err != nil {
		log.Printf("Teams: failed to persist refreshed tokens: %v", err)
	}
	accessToken = tr.AccessToken
	conn.ExpiresAt = expiresAt
	return accessToken, conn, nil
}
