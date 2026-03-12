package invitations

import (
	"time"

	"github.com/google/uuid"
)

// Role is the session-scoped role for an invite.
type Role string

const (
	RoleParticipant Role = "participant"
	RoleCreator     Role = "creator"
)

// Status is the invitation lifecycle state.
type Status string

const (
	StatusPending   Status = "pending"
	StatusAccepted  Status = "accepted"
	StatusRevoked   Status = "revoked"
	StatusExpired   Status = "expired"
)

// Invitation is the email-based, token-secured session invitation (DB row).
type Invitation struct {
	ID                   uuid.UUID  `json:"id"`
	SessionID            uuid.UUID  `json:"session_id"`
	InvitedEmailNormalized string   `json:"invited_email"`
	InviterUserID        uuid.UUID  `json:"inviter_user_id"`
	InvitedRole          Role       `json:"invited_role"`
	TokenHash            string     `json:"-"` // never expose
	Status               Status     `json:"status"`
	ExpiresAt            time.Time  `json:"expires_at"`
	AcceptedAt           *time.Time `json:"accepted_at,omitempty"`
	AcceptedByUserID    *uuid.UUID `json:"accepted_by_user_id,omitempty"`
	EmailVerifiedAt      *time.Time `json:"email_verified_at,omitempty"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
}

// CreateResult is returned by Service.Create for mailto/UI delivery. Contains one-time accept_url for frontend.
type CreateResult struct {
	Invitation   *Invitation
	AcceptURL    string
	SessionTitle string
	InviterName  string
	InviterEmail string
}

// ResendResult is returned by Service.Resend for mailto/UI delivery. Contains new one-time accept_url.
type ResendResult struct {
	ID            string
	InvitedEmail  string
	InvitedRole   string
	Status        string
	ExpiresAt     time.Time
	AcceptURL     string
	SessionTitle  string
	InviterName   string
	InviterEmail  string
}

// ResolveResult is the minimal payload for GET /api/invitations/resolve (no sensitive data).
// When Status is "accepted", SessionID is set so the client can open the session (re-use same link).
type ResolveResult struct {
	SessionID    string `json:"session_id,omitempty"`
	SessionTitle string `json:"session_title"`
	InviterName  string `json:"inviter_name"`
	InvitedEmail string `json:"invited_email"`
	InvitedRole  string `json:"invited_role"`
	Status       string `json:"status"`
	Expired      bool   `json:"expired"`
	AccountExists bool  `json:"account_exists"`
}

// PendingInvitationItem is one entry for GET /api/invitations/pending (logged-in user's pending invites).
type PendingInvitationItem struct {
	ID           string `json:"id"`
	SessionID    string `json:"session_id"`
	SessionTitle string `json:"session_title"`
	InviterName  string `json:"inviter_name"`
	InvitedRole  string `json:"invited_role"`
	ExpiresAt    string `json:"expires_at"`
}
