package invitations

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Config holds invitation defaults.
type Config struct {
	AppBaseURL  string
	ExpiryHours int
}

// DefaultExpiryHours is used when ExpiryHours <= 0.
const DefaultExpiryHours = 168 // 7 days

// SessionInviterLookup provides session title and inviter display name/email for resolve/email.
type SessionInviterLookup interface {
	GetSessionTitle(ctx context.Context, sessionID uuid.UUID) (string, error)
	GetUserDisplayName(ctx context.Context, userID uuid.UUID) (string, error)
	GetUserEmail(ctx context.Context, userID uuid.UUID) (string, error)
}

// SendInviteFunc is called to send the invite email. Accept URL contains the raw token; must not be logged.
type SendInviteFunc func(ctx context.Context, to, inviterName, sessionTitle, invitedRole, acceptURL, expirationNote string) error

// Service implements invitation business logic.
type Service struct {
	Repo   *Repository
	Lookup SessionInviterLookup
	Config Config
	Send   SendInviteFunc
}

// NormalizeEmail trims and lowercases for storage and comparison.
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func (s *Service) expiryHours() int {
	if s.Config.ExpiryHours <= 0 {
		return DefaultExpiryHours
	}
	return s.Config.ExpiryHours
}

// Create creates a pending invitation and optionally sends email. If a pending invite exists for same session+email, revokes it and creates a new one.
// Returns CreateResult with one-time accept_url for frontend mailto or copy-link delivery.
// isAlreadyMember should return true if the invited email belongs to a user who is already a member of the session.
func (s *Service) Create(ctx context.Context, sessionID, inviterUserID uuid.UUID, invitedEmail string, role Role, isAlreadyMember func(sessionID uuid.UUID, email string) bool) (*CreateResult, error) {
	invitedEmail = NormalizeEmail(invitedEmail)
	if invitedEmail == "" {
		return nil, ErrInvalidRole
	}
	if role != RoleParticipant && role != RoleCreator {
		return nil, ErrInvalidRole
	}
	if isAlreadyMember(sessionID, invitedEmail) {
		return nil, ErrAlreadyMember
	}
	_, _ = s.Repo.RevokePendingBySessionAndEmail(ctx, sessionID, invitedEmail)
	raw, hash, err := GenerateToken()
	if err != nil {
		return nil, err
	}
	expiry := time.Now().Add(time.Duration(s.expiryHours()) * time.Hour)
	inv := &Invitation{
		ID:                     uuid.New(),
		SessionID:              sessionID,
		InvitedEmailNormalized: invitedEmail,
		InviterUserID:          inviterUserID,
		InvitedRole:            role,
		TokenHash:              hash,
		Status:                 StatusPending,
		ExpiresAt:              expiry,
		CreatedAt:              time.Now(),
		UpdatedAt:              time.Now(),
	}
	if err := s.Repo.Create(ctx, inv); err != nil {
		return nil, err
	}
	acceptURL := strings.TrimSuffix(s.Config.AppBaseURL, "/") + "/accept-invite?token=" + raw
	title, _ := s.Lookup.GetSessionTitle(ctx, sessionID)
	inviterName, _ := s.Lookup.GetUserDisplayName(ctx, inviterUserID)
	inviterEmail, _ := s.Lookup.GetUserEmail(ctx, inviterUserID)
	expNote := "This link expires in " + formatExpiry(s.expiryHours()) + "."
	if s.Send != nil {
		_ = s.Send(ctx, invitedEmail, inviterName, title, string(role), acceptURL, expNote)
	}
	inv.TokenHash = ""
	return &CreateResult{
		Invitation:   inv,
		AcceptURL:    acceptURL,
		SessionTitle: title,
		InviterName:  inviterName,
		InviterEmail: inviterEmail,
	}, nil
}

func formatExpiry(hours int) string {
	if hours >= 24 && hours%24 == 0 {
		return fmt.Sprintf("%d days", hours/24)
	}
	return fmt.Sprintf("%d hours", hours)
}

// ListBySessionID returns invitations for the session (for session creator UI).
func (s *Service) ListBySessionID(ctx context.Context, sessionID uuid.UUID) ([]*Invitation, error) {
	return s.Repo.ListBySessionID(ctx, sessionID)
}

// ListPendingForUser returns pending invitation items for the given email (for "Pending Invites" UI).
func (s *Service) ListPendingForUser(ctx context.Context, emailNormalized string) ([]*PendingInvitationItem, error) {
	invitations, err := s.Repo.ListPendingByInvitedEmail(ctx, emailNormalized)
	if err != nil {
		return nil, err
	}
	out := make([]*PendingInvitationItem, 0, len(invitations))
	for _, inv := range invitations {
		title, _ := s.Lookup.GetSessionTitle(ctx, inv.SessionID)
		inviterName, _ := s.Lookup.GetUserDisplayName(ctx, inv.InviterUserID)
		out = append(out, &PendingInvitationItem{
			ID:           inv.ID.String(),
			SessionID:    inv.SessionID.String(),
			SessionTitle: title,
			InviterName:  inviterName,
			InvitedRole:  string(inv.InvitedRole),
			ExpiresAt:    inv.ExpiresAt.Format(time.RFC3339),
		})
	}
	return out, nil
}

// AcceptByInvitationID accepts an invitation by ID when the authenticated user's email matches the invited email.
func (s *Service) AcceptByInvitationID(ctx context.Context, invitationID uuid.UUID, authenticatedEmail string, createMembership func(sessionID, userID uuid.UUID, role string, invitedBy *uuid.UUID) error, getUserID func(email string) (uuid.UUID, bool)) (sessionID uuid.UUID, err error) {
	authenticatedEmail = NormalizeEmail(authenticatedEmail)
	inv, err := s.Repo.GetByID(ctx, invitationID)
	if err != nil || inv == nil {
		return uuid.Nil, ErrNotFound
	}
	if inv.Status != StatusPending {
		return uuid.Nil, ErrAlreadyAccepted
	}
	if inv.ExpiresAt.Before(time.Now()) {
		return uuid.Nil, ErrExpired
	}
	if NormalizeEmail(inv.InvitedEmailNormalized) != authenticatedEmail {
		return uuid.Nil, ErrWrongEmail
	}
	userID, ok := getUserID(authenticatedEmail)
	if !ok {
		return uuid.Nil, ErrNoAccount
	}
	if err := createMembership(inv.SessionID, userID, string(inv.InvitedRole), &inv.InviterUserID); err != nil {
		return uuid.Nil, err
	}
	now := time.Now()
	if err := s.Repo.UpdateStatus(ctx, inv.ID, StatusAccepted, &now, &userID, nil); err != nil {
		return uuid.Nil, err
	}
	return inv.SessionID, nil
}

// Resolve looks up by token and returns minimal payload for the accept-invite page. Does not expose token or sensitive data.
func (s *Service) Resolve(ctx context.Context, rawToken string, accountExists func(email string) bool) (*ResolveResult, error) {
	if rawToken == "" {
		return nil, ErrInvalidToken
	}
	hash := HashToken(rawToken)
	inv, err := s.Repo.GetByTokenHash(ctx, hash)
	if err != nil || inv == nil {
		return nil, ErrInvalidToken
	}
	if inv.Status == StatusRevoked {
		return nil, ErrRevoked
	}
	if inv.Status == StatusAccepted {
		// Re-use: return session so client can open it (logged in → go to session; else login then go)
		title, _ := s.Lookup.GetSessionTitle(ctx, inv.SessionID)
		inviterName, _ := s.Lookup.GetUserDisplayName(ctx, inv.InviterUserID)
		return &ResolveResult{
			SessionID:    inv.SessionID.String(),
			SessionTitle: title,
			InviterName:  inviterName,
			InvitedEmail: inv.InvitedEmailNormalized,
			InvitedRole:  string(inv.InvitedRole),
			Status:       string(StatusAccepted),
			Expired:      false,
			AccountExists: accountExists(inv.InvitedEmailNormalized),
		}, nil
	}
	if inv.ExpiresAt.Before(time.Now()) {
		return &ResolveResult{
			InvitedEmail: inv.InvitedEmailNormalized,
			Status:       string(StatusExpired),
			Expired:      true,
			AccountExists: accountExists(inv.InvitedEmailNormalized),
		}, nil
	}
	title, _ := s.Lookup.GetSessionTitle(ctx, inv.SessionID)
	inviterName, _ := s.Lookup.GetUserDisplayName(ctx, inv.InviterUserID)
	return &ResolveResult{
		SessionID:     inv.SessionID.String(),
		SessionTitle:  title,
		InviterName:   inviterName,
		InvitedEmail:  inv.InvitedEmailNormalized,
		InvitedRole:   string(inv.InvitedRole),
		Status:        string(inv.Status),
		Expired:       false,
		AccountExists: accountExists(inv.InvitedEmailNormalized),
	}, nil
}

// Resend rotates the token, updates expiry, and optionally sends a new email. Caller must verify authorization.
// Returns ResendResult with new one-time accept_url for frontend mailto or copy-link delivery.
func (s *Service) Resend(ctx context.Context, invitationID uuid.UUID) (*ResendResult, error) {
	inv, err := s.Repo.GetByID(ctx, invitationID)
	if err != nil || inv == nil {
		return nil, ErrNotFound
	}
	if inv.Status != StatusPending {
		return nil, ErrAlreadyAccepted
	}
	if inv.ExpiresAt.Before(time.Now()) {
		return nil, ErrExpired
	}
	raw, hash, err := GenerateToken()
	if err != nil {
		return nil, err
	}
	expiry := time.Now().Add(time.Duration(s.expiryHours()) * time.Hour)
	if err := s.Repo.UpdateTokenAndExpiry(ctx, inv.ID, hash, expiry); err != nil {
		return nil, err
	}
	acceptURL := strings.TrimSuffix(s.Config.AppBaseURL, "/") + "/accept-invite?token=" + raw
	title, _ := s.Lookup.GetSessionTitle(ctx, inv.SessionID)
	inviterName, _ := s.Lookup.GetUserDisplayName(ctx, inv.InviterUserID)
	inviterEmail, _ := s.Lookup.GetUserEmail(ctx, inv.InviterUserID)
	expNote := "This link expires in " + formatExpiry(s.expiryHours()) + "."
	if s.Send != nil {
		_ = s.Send(ctx, inv.InvitedEmailNormalized, inviterName, title, string(inv.InvitedRole), acceptURL, expNote)
	}
	return &ResendResult{
		ID:           inv.ID.String(),
		InvitedEmail: inv.InvitedEmailNormalized,
		InvitedRole:  string(inv.InvitedRole),
		Status:       string(inv.Status),
		ExpiresAt:    expiry,
		AcceptURL:    acceptURL,
		SessionTitle: title,
		InviterName:  inviterName,
		InviterEmail: inviterEmail,
	}, nil
}

// GetAcceptURL rotates the token and returns a new accept URL for copy-link. Does not send email.
// Caller must verify authorization. Returns ErrNotFound, ErrAlreadyAccepted, or ErrExpired on invalid state.
func (s *Service) GetAcceptURL(ctx context.Context, invitationID uuid.UUID) (acceptURL string, err error) {
	inv, err := s.Repo.GetByID(ctx, invitationID)
	if err != nil || inv == nil {
		return "", ErrNotFound
	}
	if inv.Status != StatusPending {
		return "", ErrAlreadyAccepted
	}
	if inv.ExpiresAt.Before(time.Now()) {
		return "", ErrExpired
	}
	raw, hash, err := GenerateToken()
	if err != nil {
		return "", err
	}
	expiry := time.Now().Add(time.Duration(s.expiryHours()) * time.Hour)
	if err := s.Repo.UpdateTokenAndExpiry(ctx, inv.ID, hash, expiry); err != nil {
		return "", err
	}
	return strings.TrimSuffix(s.Config.AppBaseURL, "/") + "/accept-invite?token=" + raw, nil
}

// Revoke marks the invitation as revoked. Caller must verify authorization.
func (s *Service) Revoke(ctx context.Context, invitationID uuid.UUID) error {
	inv, err := s.Repo.GetByID(ctx, invitationID)
	if err != nil || inv == nil {
		return ErrNotFound
	}
	if inv.Status != StatusPending {
		return nil
	}
	return s.Repo.Revoke(ctx, invitationID)
}

// Accept is for existing users: token must be valid, authenticated email must match invited email. Creates membership via callback. Returns sessionID for redirect.
func (s *Service) Accept(ctx context.Context, rawToken, authenticatedEmail string, createMembership func(sessionID, userID uuid.UUID, role string, invitedBy *uuid.UUID) error, getUserID func(email string) (uuid.UUID, bool)) (sessionID uuid.UUID, err error) {
	authenticatedEmail = NormalizeEmail(authenticatedEmail)
	hash := HashToken(rawToken)
	inv, err := s.Repo.GetByTokenHash(ctx, hash)
	if err != nil || inv == nil {
		return uuid.Nil, ErrInvalidToken
	}
	if inv.Status != StatusPending {
		return uuid.Nil, ErrAlreadyAccepted
	}
	if inv.ExpiresAt.Before(time.Now()) {
		return uuid.Nil, ErrExpired
	}
	if NormalizeEmail(inv.InvitedEmailNormalized) != authenticatedEmail {
		return uuid.Nil, ErrWrongEmail
	}
	userID, ok := getUserID(authenticatedEmail)
	if !ok {
		return uuid.Nil, ErrNoAccount
	}
	if err := createMembership(inv.SessionID, userID, string(inv.InvitedRole), &inv.InviterUserID); err != nil {
		return uuid.Nil, err
	}
	now := time.Now()
	if err := s.Repo.UpdateStatus(ctx, inv.ID, StatusAccepted, &now, &userID, nil); err != nil {
		return uuid.Nil, err
	}
	return inv.SessionID, nil
}

// RegisterAndAccept is for new users: create account with email_verified_at, create membership, mark invitation accepted.
// Returns sessionID and userID so the handler can create a login session and set cookie.
func (s *Service) RegisterAndAccept(ctx context.Context, rawToken, name, passwordHash string, createUser func(email, name, passwordHash string) (userID uuid.UUID, err error), createMembership func(sessionID, userID uuid.UUID, role string, invitedBy *uuid.UUID) error) (sessionID, userID uuid.UUID, err error) {
	hash := HashToken(rawToken)
	inv, err := s.Repo.GetByTokenHash(ctx, hash)
	if err != nil || inv == nil {
		return uuid.Nil, uuid.Nil, ErrInvalidToken
	}
	if inv.Status != StatusPending {
		return uuid.Nil, uuid.Nil, ErrAlreadyAccepted
	}
	if inv.ExpiresAt.Before(time.Now()) {
		return uuid.Nil, uuid.Nil, ErrExpired
	}
	userID, err = createUser(inv.InvitedEmailNormalized, name, passwordHash)
	if err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	if err := createMembership(inv.SessionID, userID, string(inv.InvitedRole), &inv.InviterUserID); err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	now := time.Now()
	if err := s.Repo.UpdateStatus(ctx, inv.ID, StatusAccepted, &now, &userID, &now); err != nil {
		return uuid.Nil, uuid.Nil, err
	}
	return inv.SessionID, userID, nil
}
