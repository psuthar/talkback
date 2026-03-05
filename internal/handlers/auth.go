package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/newrelic/go-agent/v3/newrelic"
	"github.com/psuthar/talkback/internal/auth"
	"github.com/psuthar/talkback/internal/models"
)

type contextKey string

const userContextKey contextKey = "user"

// UserFromContext returns the authenticated user from the request context, or nil.
func UserFromContext(ctx context.Context) *models.User {
	u, _ := ctx.Value(userContextKey).(*models.User)
	return u
}

// RequireAuth wraps a handler and ensures a valid session and active user are in context; otherwise returns 401.
func (h *Handlers) RequireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sessionID := auth.SessionIDFromRequest(r)
		if sessionID == nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		session, err := h.DB.GetLoginSessionByID(r.Context(), *sessionID)
		if err != nil || session == nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		user, err := h.DB.GetUserByID(r.Context(), session.UserID)
		if err != nil || user == nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		if user.Status != models.UserStatusActive {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "account disabled"})
			return
		}
		ctx := context.WithValue(r.Context(), userContextKey, user)
		next(w, r.WithContext(ctx))
	}
}

// RequireAdmin wraps a handler and requires the user to have global_role admin; use after RequireAuth.
func (h *Handlers) RequireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user := UserFromContext(r.Context())
		if user == nil {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		if user.GlobalRole != models.GlobalRoleAdmin {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "admin required"})
			return
		}
		next(w, r)
	}
}

// --- Auth request/response DTOs ---

type signupRequest struct {
	Email       string `json:"email"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name"`
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type meResponse struct {
	ID          string `json:"id"`
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
	GlobalRole  string `json:"global_role"`
	Status      string `json:"status"`
}

// --- Handlers ---

// AuthSignup handles POST /api/auth/signup
func (h *Handlers) AuthSignup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if !auth.AllowOriginForAuth(r) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "origin not allowed"})
		return
	}
	var req signupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	email := strings.TrimSpace(strings.ToLower(req.Email))
	displayName := strings.TrimSpace(req.DisplayName)
	if email == "" || req.Password == "" || displayName == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "email, password, and display_name required"})
		return
	}
	ctx := r.Context()
	existing, _ := h.DB.GetUserByEmail(ctx, email)
	if existing != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "email already registered"})
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create account"})
		return
	}
	user := &models.User{
		ID:          uuid.New(),
		Email:       email,
		DisplayName: displayName,
		Status:      models.UserStatusActive,
		GlobalRole:  models.GlobalRoleCreator, // default for signup; bootstrap can override to admin
	}
	if auth.Config.BootstrapAdminEmail != "" && email == auth.Config.BootstrapAdminEmail {
		count, _ := h.DB.CountUsers(ctx)
		if count == 0 {
			user.GlobalRole = models.GlobalRoleAdmin
		}
	}
	if err := h.DB.CreateUser(ctx, user); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create account"})
		return
	}
	cred := &models.PasswordCredential{UserID: user.ID, PasswordHash: hash, PasswordUpdatedAt: time.Now()}
	if err := h.DB.CreatePasswordCredential(ctx, cred); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create account"})
		return
	}
	expiresAt := time.Now().Add(time.Duration(auth.Config.SessionTTLHours) * time.Hour)
	ip, ua := r.RemoteAddr, r.UserAgent()
	session := &models.LoginSession{
		ID:        uuid.New(),
		UserID:    user.ID,
		CreatedAt: time.Now(),
		ExpiresAt: expiresAt,
		IP:        &ip,
		UserAgent: &ua,
	}
	if err := h.DB.CreateLoginSession(ctx, session); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create session"})
		return
	}
	auth.SetSessionCookie(w, session.ID, expiresAt)
	writeJSON(w, http.StatusCreated, meResponse{
		ID:          user.ID.String(),
		Email:       user.Email,
		DisplayName: user.DisplayName,
		GlobalRole:  string(user.GlobalRole),
		Status:      string(user.Status),
	})
}

// setAuthFailureAttributes sets New Relic custom attributes on the transaction for failed login (observability gating).
// Only the attempted username (email) is set; never passwords or tokens.
func setAuthFailureAttributes(r *http.Request, email string) {
	txn := newrelic.FromContext(r.Context())
	if txn == nil {
		return
	}
	user := strings.TrimSpace(strings.ToLower(email))
	if user != "" {
		txn.AddAttribute("auth.username", user)
	}
	txn.AddAttribute("auth.outcome", "failure")
	txn.AddAttribute("auth.reason", "unauthorized")
	txn.AddAttribute("auth.endpoint", "POST /api/auth/login")
}

// AuthLogin handles POST /api/auth/login
func (h *Handlers) AuthLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if !auth.AllowOriginForAuth(r) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "origin not allowed"})
		return
	}
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid credentials"})
		return
	}
	email := strings.TrimSpace(strings.ToLower(req.Email))
	if email == "" || req.Password == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid credentials"})
		return
	}
	ctx := r.Context()
	user, err := h.DB.GetUserByEmail(ctx, email)
	if err != nil || user == nil {
		setAuthFailureAttributes(r, email)
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid credentials"})
		return
	}
	if user.Status != models.UserStatusActive {
		setAuthFailureAttributes(r, email)
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "invalid credentials"})
		return
	}
	cred, err := h.DB.GetPasswordCredentialByUserID(ctx, user.ID)
	if err != nil || cred == nil {
		setAuthFailureAttributes(r, email)
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid credentials"})
		return
	}
	if !auth.CheckPassword(cred.PasswordHash, req.Password) {
		setAuthFailureAttributes(r, email)
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid credentials"})
		return
	}
	if err := h.DB.UpdateUserLastLoginAt(ctx, user.ID); err != nil {
		// non-fatal
	}
	// Bootstrap: if this is the configured admin email and first user, make admin
	if auth.Config.BootstrapAdminEmail != "" && email == auth.Config.BootstrapAdminEmail {
		count, _ := h.DB.CountUsers(ctx)
		if count == 1 {
			_ = h.DB.SetUserGlobalRole(ctx, user.ID, models.GlobalRoleAdmin)
			user.GlobalRole = models.GlobalRoleAdmin
		}
	}
	expiresAt := time.Now().Add(time.Duration(auth.Config.SessionTTLHours) * time.Hour)
	ip, ua := r.RemoteAddr, r.UserAgent()
	session := &models.LoginSession{
		ID:        uuid.New(),
		UserID:    user.ID,
		CreatedAt: time.Now(),
		ExpiresAt: expiresAt,
		IP:        &ip,
		UserAgent: &ua,
	}
	if err := h.DB.CreateLoginSession(ctx, session); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "login failed"})
		return
	}
	auth.SetSessionCookie(w, session.ID, expiresAt)
	writeJSON(w, http.StatusOK, meResponse{
		ID:          user.ID.String(),
		Email:       user.Email,
		DisplayName: user.DisplayName,
		GlobalRole:  string(user.GlobalRole),
		Status:      string(user.Status),
	})
}

// AuthLogout handles POST /api/auth/logout
func (h *Handlers) AuthLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if !auth.AllowOriginForAuth(r) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "origin not allowed"})
		return
	}
	sessionID := auth.SessionIDFromRequest(r)
	if sessionID != nil {
		_ = h.DB.RevokeSessionByID(r.Context(), *sessionID)
	}
	auth.ClearSessionCookie(w)
	writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
}

// AuthMe handles GET /api/me (requires auth)
func (h *Handlers) AuthMe(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	user := UserFromContext(r.Context())
	if user == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	writeJSON(w, http.StatusOK, meResponse{
		ID:          user.ID.String(),
		Email:       user.Email,
		DisplayName: user.DisplayName,
		GlobalRole:  string(user.GlobalRole),
		Status:      string(user.Status),
	})
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
