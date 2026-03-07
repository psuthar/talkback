package handlers

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/auth"
	"github.com/psuthar/talkback/internal/invitations"
	"github.com/psuthar/talkback/internal/models"
)

// CreateInvitationRequest is the body for POST /api/sessions/:sessionId/invitations.
type CreateInvitationRequest struct {
	Email string `json:"email"`
	Role  string `json:"role"`
}

// CreateInvitation handles POST /api/sessions/:sessionId/invitations (RequireAuth). Session creator or member can invite by email.
func (h *Handlers) CreateInvitation(w http.ResponseWriter, r *http.Request) {
	if h.InvitationService == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "invitation service not configured"})
		return
	}
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	sessionID, err := parseSessionIDFromPath(r.URL.Path, "api", "sessions", "invitations")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid session id"})
		return
	}
	user := UserFromContext(r.Context())
	if user == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	ctx := r.Context()
	canAccess, err := h.DB.UserCanAccessSession(ctx, sessionID, user)
	if err != nil || !canAccess {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "only session creator or a member can invite"})
		return
	}
	var req CreateInvitationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	email := invitations.NormalizeEmail(req.Email)
	if email == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "email required"})
		return
	}
	role := invitations.Role(req.Role)
	if role != invitations.RoleParticipant && role != invitations.RoleCreator {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "role must be participant or creator"})
		return
	}
	isAlreadyMember := func(sid uuid.UUID, e string) bool {
		u, _ := h.DB.GetUserByEmail(ctx, e)
		if u == nil {
			return false
		}
		ok, _ := h.DB.UserIsSessionMember(ctx, sid, u.ID)
		return ok
	}
	result, err := h.InvitationService.Create(ctx, sessionID, user.ID, email, role, isAlreadyMember)
	if err != nil {
		if err == invitations.ErrAlreadyMember {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "user is already a member of this session"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create invitation"})
		return
	}
	inv := result.Invitation
	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"invitation": map[string]interface{}{
			"id":               inv.ID.String(),
			"session_id":       inv.SessionID.String(),
			"invited_email":    inv.InvitedEmailNormalized,
			"invited_role":     string(inv.InvitedRole),
			"status":          string(inv.Status),
			"expires_at":      inv.ExpiresAt,
			"created_at":      inv.CreatedAt,
			"accept_url":      result.AcceptURL,
			"session_title":   result.SessionTitle,
			"inviter_name":    result.InviterName,
			"inviter_email":   result.InviterEmail,
		},
	})
}

// ListInvitations handles GET /api/sessions/:sessionId/invitations (RequireAuth).
func (h *Handlers) ListInvitations(w http.ResponseWriter, r *http.Request) {
	if h.InvitationService == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "invitation service not configured"})
		return
	}
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	sessionID, err := parseSessionIDFromPath(r.URL.Path, "api", "sessions", "invitations")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid session id"})
		return
	}
	user := UserFromContext(r.Context())
	if user == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	ctx := r.Context()
	canAccess, err := h.DB.UserCanAccessSession(ctx, sessionID, user)
	if err != nil || !canAccess {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}
	list, err := h.InvitationService.ListBySessionID(ctx, sessionID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list invitations"})
		return
	}
	items := make([]map[string]interface{}, 0, len(list))
	for _, inv := range list {
		inviterName := ""
		if u, _ := h.DB.GetUserByID(ctx, inv.InviterUserID); u != nil {
			inviterName = u.DisplayName
		}
		items = append(items, map[string]interface{}{
			"id":            inv.ID.String(),
			"invited_email": inv.InvitedEmailNormalized,
			"invited_role":  string(inv.InvitedRole),
			"status":        string(inv.Status),
			"inviter_name":  inviterName,
			"expires_at":    inv.ExpiresAt,
			"accepted_at":   inv.AcceptedAt,
			"created_at":    inv.CreatedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"invitations": items})
}

// parseSessionIDFromPath expects path like /api/sessions/<uuid>/invitations and returns the uuid.
func parseSessionIDFromPath(path, a, b, suffix string) (uuid.UUID, error) {
	path = strings.Trim(path, "/")
	parts := strings.Split(path, "/")
	// api, sessions, <id>, invitations
	if len(parts) < 4 || parts[0] != a || parts[1] != b || parts[3] != suffix {
		return uuid.Nil, nil
	}
	return uuid.Parse(parts[2])
}

// ResolveInvitation handles GET /api/invitations/resolve?token=... (public).
func (h *Handlers) ResolveInvitation(w http.ResponseWriter, r *http.Request) {
	if h.InvitationService == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "invitation service not configured"})
		return
	}
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	token := r.URL.Query().Get("token")
	if token == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "token required"})
		return
	}
	ctx := r.Context()
	accountExists := func(email string) bool {
		u, _ := h.DB.GetUserByEmail(ctx, email)
		return u != nil
	}
	result, err := h.InvitationService.Resolve(ctx, token, accountExists)
	if err != nil {
		if err == invitations.ErrInvalidToken || err == invitations.ErrRevoked || err == invitations.ErrAlreadyAccepted {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to resolve"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"invitation": result})
}

// AcceptInvitationRequest is the body for POST /api/invitations/accept.
type AcceptInvitationRequest struct {
	Token          string `json:"token"`
	AcceptAuthToken string `json:"accept_auth_token,omitempty"`
}

// AcceptInvitation handles POST /api/invitations/accept. User may be authenticated via session cookie or accept_auth_token (short-lived token from login with for_accept_invite).
func (h *Handlers) AcceptInvitation(w http.ResponseWriter, r *http.Request) {
	if h.InvitationService == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "invitation service not configured"})
		return
	}
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var req AcceptInvitationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Token == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "token required"})
		return
	}
	user := UserFromContext(r.Context())
	if user == nil && req.AcceptAuthToken != "" {
		userID, err := auth.ValidateAcceptToken(req.AcceptAuthToken)
		if err == nil {
			user, _ = h.DB.GetUserByID(r.Context(), userID)
		}
	}
	if user == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	ctx := r.Context()
	createMembership := func(sessionID, userID uuid.UUID, role string, invitedBy *uuid.UUID) error {
		return h.DB.CreateSessionMembership(ctx, sessionID, userID, role, invitedBy)
	}
	getUserID := func(email string) (uuid.UUID, bool) {
		u, _ := h.DB.GetUserByEmail(ctx, invitations.NormalizeEmail(email))
		if u == nil {
			return uuid.Nil, false
		}
		return u.ID, true
	}
	sessionID, err := h.InvitationService.Accept(ctx, req.Token, user.Email, createMembership, getUserID)
	if err != nil {
		if err == invitations.ErrWrongEmail {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "this invitation was sent to a different email; sign in with the invited email to continue"})
			return
		}
		if err == invitations.ErrInvalidToken || err == invitations.ErrExpired || err == invitations.ErrAlreadyAccepted || err == invitations.ErrNoAccount {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to accept"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true, "session_id": sessionID.String(), "redirect_to": "/?session=" + sessionID.String()})
}

// RegisterAndAcceptInvitationRequest is the body for POST /api/invitations/register-and-accept.
type RegisterAndAcceptInvitationRequest struct {
	Token    string `json:"token"`
	Name     string `json:"name"`
	Password string `json:"password"`
}

// RegisterAndAcceptInvitation handles POST /api/invitations/register-and-accept (public). Creates user with email_verified_at and membership.
func (h *Handlers) RegisterAndAcceptInvitation(w http.ResponseWriter, r *http.Request) {
	if h.InvitationService == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "invitation service not configured"})
		return
	}
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var req RegisterAndAcceptInvitationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if req.Token == "" || req.Name == "" || req.Password == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "token, name, and password required"})
		return
	}
	ctx := r.Context()
	passwordHash, err := auth.HashPassword(req.Password)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to process password"})
		return
	}
	createUser := func(email, name, hash string) (uuid.UUID, error) {
		u := &models.User{
			ID:               uuid.New(),
			Email:            email,
			DisplayName:      name,
			Status:           models.UserStatusActive,
			GlobalRole:       models.GlobalRoleParticipant,
			EmailVerifiedAt:  func() *time.Time { t := time.Now(); return &t }(),
		}
		if err := h.DB.CreateUser(ctx, u); err != nil {
			return uuid.Nil, err
		}
		cred := &models.PasswordCredential{UserID: u.ID, PasswordHash: hash, PasswordUpdatedAt: time.Now()}
		if err := h.DB.CreatePasswordCredential(ctx, cred); err != nil {
			return uuid.Nil, err
		}
		return u.ID, nil
	}
	createMembership := func(sessionID, userID uuid.UUID, role string, invitedBy *uuid.UUID) error {
		return h.DB.CreateSessionMembership(ctx, sessionID, userID, role, invitedBy)
	}
	sessionID, newUserID, err := h.InvitationService.RegisterAndAccept(ctx, req.Token, req.Name, passwordHash, createUser, createMembership)
	if err != nil {
		if err == invitations.ErrInvalidToken || err == invitations.ErrExpired || err == invitations.ErrAlreadyAccepted || err == invitations.ErrAccountExists {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to register and accept"})
		return
	}
	// Sign in the new user: create login session and set cookie (same as signup flow)
	if auth.AllowOriginForAuth(r) {
		expiresAt := time.Now().Add(time.Duration(auth.Config.SessionTTLHours) * time.Hour)
		ip, ua := r.RemoteAddr, r.UserAgent()
		loginSess := &models.LoginSession{
			ID:        uuid.New(),
			UserID:    newUserID,
			CreatedAt: time.Now(),
			ExpiresAt: expiresAt,
			IP:        &ip,
			UserAgent: &ua,
		}
		if err := h.DB.CreateLoginSession(ctx, loginSess); err == nil {
			auth.SetSessionCookie(w, loginSess.ID, expiresAt)
		}
	}
	user, _ := h.DB.GetUserByID(ctx, newUserID)
	me := map[string]interface{}{"ok": true, "session_id": sessionID.String(), "redirect_to": "/?session=" + sessionID.String()}
	if user != nil {
		me["user"] = map[string]interface{}{"id": user.ID.String(), "email": user.Email, "display_name": user.DisplayName, "global_role": string(user.GlobalRole), "status": string(user.Status)}
	}
	// Issue short-lived accept token so frontend can stay "logged in" in incognito (cookie may not be sent cross-origin)
	if acceptTok, err := auth.IssueAcceptToken(newUserID); err == nil {
		me["accept_token"] = acceptTok
	}
	writeJSON(w, http.StatusOK, me)
}

// ResendInvitation handles POST /api/invitations/:id/resend (RequireAuth).
func (h *Handlers) ResendInvitation(w http.ResponseWriter, r *http.Request) {
	if h.InvitationService == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "invitation service not configured"})
		return
	}
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	invitationID, err := parseUUIDFromPath(r.URL.Path, "api", "invitations", "resend")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid invitation id"})
		return
	}
	user := UserFromContext(r.Context())
	if user == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	ctx := r.Context()
	if h.InvitationService.Repo != nil {
		inv, _ := h.InvitationService.Repo.GetByID(ctx, invitationID)
		if inv != nil {
			canAccess, _ := h.DB.UserCanAccessSession(ctx, inv.SessionID, user)
			if !canAccess {
				writeJSON(w, http.StatusForbidden, map[string]string{"error": "not authorized to resend this invitation"})
				return
			}
		}
	}
	res, err := h.InvitationService.Resend(ctx, invitationID)
	if err != nil {
		if err == invitations.ErrNotFound || err == invitations.ErrAlreadyAccepted || err == invitations.ErrExpired {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to resend"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"ok": true,
		"invitation": map[string]interface{}{
			"id":              res.ID,
			"invited_email":   res.InvitedEmail,
			"invited_role":    res.InvitedRole,
			"status":          res.Status,
			"expires_at":      res.ExpiresAt,
			"accept_url":      res.AcceptURL,
			"session_title":   res.SessionTitle,
			"inviter_name":    res.InviterName,
			"inviter_email":   res.InviterEmail,
		},
	})
}

// RevokeInvitation handles POST /api/invitations/:id/revoke (RequireAuth).
func (h *Handlers) RevokeInvitation(w http.ResponseWriter, r *http.Request) {
	if h.InvitationService == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "invitation service not configured"})
		return
	}
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	invitationID, err := parseUUIDFromPath(r.URL.Path, "api", "invitations", "revoke")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid invitation id"})
		return
	}
	user := UserFromContext(r.Context())
	if user == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
		return
	}
	ctx := r.Context()
	if h.InvitationService.Repo != nil {
		inv, _ := h.InvitationService.Repo.GetByID(ctx, invitationID)
		if inv != nil {
			canAccess, _ := h.DB.UserCanAccessSession(ctx, inv.SessionID, user)
			if !canAccess {
				writeJSON(w, http.StatusForbidden, map[string]string{"error": "not authorized to revoke this invitation"})
				return
			}
		}
	}
	err = h.InvitationService.Revoke(ctx, invitationID)
	if err != nil {
		if err == invitations.ErrNotFound {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to revoke"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": true})
}

func parseUUIDFromPath(path, a, b, suffix string) (uuid.UUID, error) {
	path = strings.Trim(path, "/")
	parts := strings.Split(path, "/")
	if len(parts) < 4 || parts[0] != a || parts[1] != b || parts[3] != suffix {
		return uuid.Nil, nil
	}
	return uuid.Parse(parts[2])
}
