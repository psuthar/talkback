package handlers

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/auth"
	"github.com/psuthar/talkback/internal/models"
)

// AdminUserPayload is the JSON shape for one user in admin list (includes session_ids).
type AdminUserPayload struct {
	ID          string   `json:"id"`
	Email       string   `json:"email"`
	DisplayName string   `json:"display_name"`
	Status      string   `json:"status"`
	GlobalRole  string   `json:"global_role"`
	CreatedAt   string   `json:"created_at"`
	LastLoginAt *string  `json:"last_login_at,omitempty"`
	SessionIDs  []string `json:"session_ids"`
}

// AdminCreateUserRequest is the body for POST /api/admin/users.
type AdminCreateUserRequest struct {
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
	Password    string `json:"password"`
}

// ListAdminUsers handles GET /api/admin/users (RequireAuth + RequireAdmin).
func (h *Handlers) ListAdminUsers(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	ctx := r.Context()
	users, err := h.DB.ListUsers(ctx)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list users"})
		return
	}
	out := make([]AdminUserPayload, 0, len(users))
	for _, u := range users {
		sessionIDs, _ := h.DB.GetSessionIDsForUser(ctx, u.ID)
		ss := make([]string, 0, len(sessionIDs))
		for _, id := range sessionIDs {
			ss = append(ss, id.String())
		}
		createdAt := u.CreatedAt.Format("2006-01-02T15:04:05Z07:00")
		var lastLoginAt *string
		if u.LastLoginAt != nil {
			s := u.LastLoginAt.Format("2006-01-02T15:04:05Z07:00")
			lastLoginAt = &s
		}
		out = append(out, AdminUserPayload{
			ID:          u.ID.String(),
			Email:       u.Email,
			DisplayName: u.DisplayName,
			Status:      string(u.Status),
			GlobalRole:  string(u.GlobalRole),
			CreatedAt:   createdAt,
			LastLoginAt: lastLoginAt,
			SessionIDs:  ss,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// CreateAdminUser handles POST /api/admin/users (RequireAuth + RequireAdmin).
func (h *Handlers) CreateAdminUser(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var req AdminCreateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	email := strings.TrimSpace(strings.ToLower(req.Email))
	displayName := strings.TrimSpace(req.DisplayName)
	if email == "" || req.Password == "" || displayName == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "email, display_name, and password required"})
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
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create user"})
		return
	}
	user := &models.User{
		ID:          uuid.New(),
		Email:       email,
		DisplayName: displayName,
		Status:      models.UserStatusActive,
		GlobalRole:  models.GlobalRoleUser,
	}
	if err := h.DB.CreateUser(ctx, user); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create user"})
		return
	}
	cred := &models.PasswordCredential{
		UserID:            user.ID,
		PasswordHash:      hash,
		PasswordUpdatedAt: time.Now(),
	}
	if err := h.DB.CreatePasswordCredential(ctx, cred); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create user"})
		return
	}
	// Return same shape as list (no password)
	createdAt := user.CreatedAt.Format("2006-01-02T15:04:05Z07:00")
	writeJSON(w, http.StatusCreated, AdminUserPayload{
		ID:          user.ID.String(),
		Email:       user.Email,
		DisplayName: user.DisplayName,
		Status:      string(user.Status),
		GlobalRole:  string(user.GlobalRole),
		CreatedAt:   createdAt,
		SessionIDs:  []string{},
	})
}

// DeleteAdminUser handles DELETE /api/admin/users/:id (RequireAuth + RequireAdmin).
func (h *Handlers) DeleteAdminUser(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	path := strings.Trim(r.URL.Path, "/")
	parts := strings.Split(path, "/")
	// Expect /api/admin/users/:id
	var idStr string
	for i, p := range parts {
		if p == "users" && i+1 < len(parts) {
			idStr = parts[i+1]
			break
		}
	}
	if idStr == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "user id required"})
		return
	}
	userID, err := uuid.Parse(idStr)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid user id"})
		return
	}
	ctx := r.Context()
	existing, _ := h.DB.GetUserByID(ctx, userID)
	if existing == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "user not found"})
		return
	}
	// Do not allow removing the last admin.
	if existing.GlobalRole == models.GlobalRoleAdmin {
		count, err := h.DB.CountAdmins(ctx)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to check admin count"})
			return
		}
		if count <= 1 {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "cannot remove the last admin; there must be at least one admin"})
			return
		}
	}
	if err := h.DB.DeleteUser(ctx, userID); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to delete user"})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// AdminUsersHandler dispatches GET/POST /api/admin/users and DELETE /api/admin/users/:id.
func (h *Handlers) AdminUsersHandler(w http.ResponseWriter, r *http.Request) {
	path := strings.Trim(r.URL.Path, "/")
	parts := strings.Split(path, "/")
	// /api/admin/users or /api/admin/users/:id
	if len(parts) < 3 || parts[0] != "api" || parts[1] != "admin" || parts[2] != "users" {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	if len(parts) == 3 {
		// /api/admin/users
		switch r.Method {
		case http.MethodGet:
			h.ListAdminUsers(w, r)
			return
		case http.MethodPost:
			h.CreateAdminUser(w, r)
			return
		}
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if len(parts) == 4 && r.Method == http.MethodDelete {
		h.DeleteAdminUser(w, r)
		return
	}
	writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
}
