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

// Package-level URLs are vars (not consts) so tests can stub them.
var (
	googleMeetAuthorizeURL  = "https://accounts.google.com/o/oauth2/v2/auth"
	googleMeetTokenURL      = "https://oauth2.googleapis.com/token"
	googleMeetUserInfoURL   = "https://openidconnect.googleapis.com/v1/userinfo"
	googleMeetProbeURL      = "https://meet.googleapis.com/v2/conferenceRecords?pageSize=1"
)

// Default scopes; override with GOOGLE_MEET_OAUTH_SCOPES.
const googleMeetScopesDefault = "openid email profile https://www.googleapis.com/auth/meetings.space.readonly https://www.googleapis.com/auth/drive.readonly"

func googleMeetEnabled() bool { return os.Getenv("ENABLE_GOOGLE_MEET") == "true" }

func googleMeetOAuthConfig() (clientID, clientSecret, baseURL, redirectURI string, err error) {
	clientID = strings.TrimSpace(os.Getenv("GOOGLE_MEET_CLIENT_ID"))
	clientSecret = strings.TrimSpace(os.Getenv("GOOGLE_MEET_CLIENT_SECRET"))
	baseURL = strings.TrimSpace(os.Getenv("APP_BASE_URL"))
	if baseURL == "" {
		baseURL = strings.TrimSpace(os.Getenv("BASE_URL"))
	}
	baseURL = strings.TrimSuffix(baseURL, "/")
	if baseURL == "" && os.Getenv("ENV") != "production" {
		baseURL = "http://localhost:8080"
	}
	redirectURI = strings.TrimSpace(os.Getenv("GOOGLE_MEET_REDIRECT_URL"))
	if redirectURI == "" && os.Getenv("ENV") != "production" {
		redirectURI = "http://localhost:8081/auth/google-meet/callback"
	}
	if redirectURI != "" {
		redirectURI, err = utils.RequireAbsoluteURL("GOOGLE_MEET_REDIRECT_URL", redirectURI)
		if err != nil {
			return "", "", "", "", err
		}
	} else {
		return "", "", "", "", fmt.Errorf("GOOGLE_MEET_REDIRECT_URL must be set")
	}
	return clientID, clientSecret, baseURL, redirectURI, nil
}

// googleTokenResponse is the shape returned by oauth2.googleapis.com/token.
type googleTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	Scope        string `json:"scope"`
	TokenType    string `json:"token_type"`
	IDToken      string `json:"id_token,omitempty"`
}

// googleUserInfo is a subset of OIDC /userinfo.
type googleUserInfo struct {
	Sub   string `json:"sub"`
	Email string `json:"email"`
	HD    string `json:"hd,omitempty"` // hosted-domain claim; empty for consumer accounts
}

// GoogleMeetAuthStart redirects to Google OAuth with prompt=consent + access_type=offline.
// prompt=consent on every authorize is mandatory: Google does not re-issue refresh_token without it.
func (h *Handlers) GoogleMeetAuthStart(w http.ResponseWriter, r *http.Request) {
	if !googleMeetEnabled() {
		http.Error(w, "Google Meet integration disabled", http.StatusNotFound)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	clientID, _, _, redirectURI, err := googleMeetOAuthConfig()
	if err != nil {
		http.Error(w, "Google Meet OAuth misconfigured: "+err.Error(), http.StatusInternalServerError)
		return
	}
	if clientID == "" {
		http.Error(w, "Google Meet OAuth not configured (GOOGLE_MEET_CLIENT_ID)", http.StatusInternalServerError)
		return
	}
	creatorIdentity := r.URL.Query().Get("creator_identity")
	if creatorIdentity == "" {
		creatorIdentity = uuid.New().String()
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
	http.Redirect(w, r, googleMeetAuthorizeURL+"?"+params.Encode(), http.StatusFound)
}

// GoogleMeetAuthCallback exchanges code, fetches userinfo, probes Workspace eligibility,
// persists encrypted tokens, redirects to the SPA.
func (h *Handlers) GoogleMeetAuthCallback(w http.ResponseWriter, r *http.Request) {
	if !googleMeetEnabled() {
		http.Error(w, "Google Meet integration disabled", http.StatusNotFound)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	code := r.URL.Query().Get("code")
	state := r.URL.Query().Get("state")
	if code == "" || state == "" {
		http.Redirect(w, r, "/?google_meet=error&message=missing_code_or_state", http.StatusFound)
		return
	}
	creatorIdentity := state
	clientID, clientSecret, baseURL, redirectURI, err := googleMeetOAuthConfig()
	if err != nil || clientID == "" || clientSecret == "" {
		http.Redirect(w, r, "/?google_meet=error&message=server_not_configured", http.StatusFound)
		return
	}

	// Token exchange.
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", redirectURI)
	form.Set("client_id", clientID)
	form.Set("client_secret", clientSecret)
	tokenResp, err := postFormJSON[googleTokenResponse](r.Context(), googleMeetTokenURL, form)
	if err != nil || tokenResp.AccessToken == "" {
		log.Printf("[google_meet] token exchange failed: %v", err)
		http.Redirect(w, r, "/?google_meet=error&message=exchange_failed", http.StatusFound)
		return
	}
	if tokenResp.RefreshToken == "" {
		// Google did not issue a refresh token. Without one we cannot keep the access token
		// valid past 1h, so we refuse the connection rather than silently accept it.
		log.Printf("[google_meet] callback: refresh_token missing in token response")
		http.Redirect(w, r, "/?google_meet=error&message=refresh_token_missing", http.StatusFound)
		return
	}
	encKey := os.Getenv("ENCRYPTION_KEY")
	if encKey == "" {
		encKey = "default-key-change-in-production"
	}
	accessEnc, err := utils.EncryptToken([]byte(tokenResp.AccessToken), encKey)
	if err != nil {
		http.Redirect(w, r, "/?google_meet=error&message=exchange_failed", http.StatusFound)
		return
	}
	refreshEnc, err := utils.EncryptToken([]byte(tokenResp.RefreshToken), encKey)
	if err != nil {
		http.Redirect(w, r, "/?google_meet=error&message=exchange_failed", http.StatusFound)
		return
	}

	// /userinfo for sub + email.
	var googleUserID, googleEmail string
	if me, err := getJSONBearer[googleUserInfo](r.Context(), googleMeetUserInfoURL, tokenResp.AccessToken); err == nil {
		googleUserID = me.Sub
		googleEmail = me.Email
	}
	if googleUserID == "" {
		googleUserID = "unknown"
	}
	var emailPtr *string
	if googleEmail != "" {
		emailPtr = &googleEmail
	}

	// workspace_eligible probe: a single conferenceRecords.list call.
	// 200 → eligible; 403/missing-scope → not eligible; any other error → unknown (nil).
	eligible := probeWorkspaceEligible(r.Context(), tokenResp.AccessToken)

	scopeStr := tokenResp.Scope
	var scopePtr *string
	if scopeStr != "" {
		scopePtr = &scopeStr
	}
	conn := &models.GoogleMeetConnection{
		ID:                    uuid.New(),
		CreatorIdentityID:     creatorIdentity,
		GoogleUserID:          googleUserID,
		GoogleUserEmail:       emailPtr,
		GrantedScope:          scopePtr,
		WorkspaceEligible:     eligible,
		AccessTokenEncrypted:  accessEnc,
		RefreshTokenEncrypted: refreshEnc,
		ExpiresAt:             time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second),
	}
	if err := h.DB.CreateGoogleMeetConnection(r.Context(), conn); err != nil {
		log.Printf("[google_meet] persist connection failed: %v", err)
		http.Redirect(w, r, "/?google_meet=error&message=save_failed", http.StatusFound)
		return
	}
	redirectTo := strings.TrimSuffix(os.Getenv("APP_REDIRECT_URL"), "/")
	if redirectTo == "" {
		redirectTo = baseURL
	}
	http.Redirect(w, r, redirectTo+"/?google_meet=connected&creator_identity="+url.QueryEscape(creatorIdentity), http.StatusFound)
}

// GoogleMeetAuthDisconnect deletes the connection (idempotent).
func (h *Handlers) GoogleMeetAuthDisconnect(w http.ResponseWriter, r *http.Request) {
	if !googleMeetEnabled() {
		http.Error(w, "Google Meet integration disabled", http.StatusNotFound)
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
		http.Error(w, "creator identity required", http.StatusBadRequest)
		return
	}
	if err := h.DB.DeleteGoogleMeetConnectionByCreatorIdentity(r.Context(), creatorIdentity); err != nil {
		http.Error(w, "failed to disconnect", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "disconnected"})
}

// GetValidGoogleMeetAccessTokenContext returns a valid access token, refreshing if within 5 minutes of expiry.
// Returns sentinel error containing "meet_refresh_token_missing" if the stored refresh token is empty
// (which indicates the user must reconnect with prompt=consent).
func (h *Handlers) GetValidGoogleMeetAccessTokenContext(ctx context.Context, creatorIdentityID string) (string, *models.GoogleMeetConnection, error) {
	conn, err := h.DB.GetGoogleMeetConnectionByCreatorIdentity(ctx, creatorIdentityID)
	if err != nil || conn == nil {
		return "", nil, fmt.Errorf("google_meet not connected")
	}
	encKey := os.Getenv("ENCRYPTION_KEY")
	if encKey == "" {
		encKey = "default-key-change-in-production"
	}
	if len(conn.RefreshTokenEncrypted) == 0 {
		return "", conn, fmt.Errorf("meet_refresh_token_missing: refresh token not stored; reconnect Google Meet")
	}
	accessPlain, err := utils.DecryptToken(conn.AccessTokenEncrypted, encKey)
	if err != nil {
		return "", conn, fmt.Errorf("decrypt access token: %w", err)
	}
	access := string(accessPlain)
	if time.Until(conn.ExpiresAt) >= 5*time.Minute {
		return access, conn, nil
	}
	// Refresh.
	refreshPlain, err := utils.DecryptToken(conn.RefreshTokenEncrypted, encKey)
	if err != nil {
		return "", conn, fmt.Errorf("decrypt refresh token: %w", err)
	}
	clientID, clientSecret, _, _, cfgErr := googleMeetOAuthConfig()
	if cfgErr != nil || clientID == "" || clientSecret == "" {
		return access, conn, nil // no creds; return current token
	}
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", string(refreshPlain))
	form.Set("client_id", clientID)
	form.Set("client_secret", clientSecret)
	tr, err := postFormJSON[googleTokenResponse](ctx, googleMeetTokenURL, form)
	if err != nil || tr.AccessToken == "" {
		return "", conn, fmt.Errorf("google_meet token refresh failed: %w", err)
	}
	newAccessEnc, err := utils.EncryptToken([]byte(tr.AccessToken), encKey)
	if err != nil {
		return "", conn, err
	}
	newRefreshEnc := conn.RefreshTokenEncrypted
	if tr.RefreshToken != "" {
		if re, err := utils.EncryptToken([]byte(tr.RefreshToken), encKey); err == nil {
			newRefreshEnc = re
		}
	}
	expiresAt := time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second)
	if err := h.DB.UpdateGoogleMeetConnectionTokens(ctx, creatorIdentityID, newAccessEnc, newRefreshEnc, expiresAt); err != nil {
		log.Printf("[google_meet] update tokens after refresh: %v", err)
	}
	conn.ExpiresAt = expiresAt
	return tr.AccessToken, conn, nil
}

// probeWorkspaceEligible calls conferenceRecords.list?pageSize=1 with the given access token.
// 200 → returns &true; 403 → returns &false; other errors → returns nil (unknown).
func probeWorkspaceEligible(ctx context.Context, accessToken string) *bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, googleMeetProbeURL, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusOK:
		v := true
		return &v
	case resp.StatusCode == http.StatusForbidden:
		v := false
		return &v
	default:
		return nil
	}
}

// postFormJSON POSTs form-encoded body and decodes JSON response into T.
func postFormJSON[T any](ctx context.Context, target string, form url.Values) (*T, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, truncateGM(string(body), 400))
	}
	var out T
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// getJSONBearer GETs a URL with bearer auth and decodes JSON into T.
func getJSONBearer[T any](ctx context.Context, target, accessToken string) (*T, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, truncateGM(string(body), 400))
	}
	var out T
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func truncateGM(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
