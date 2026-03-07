package email

import "context"

// SendInviteParams holds data for sending an invitation email.
type SendInviteParams struct {
	ToEmail       string
	InviterName   string
	SessionTitle  string
	InvitedRole   string
	AcceptURL     string
	ExpirationNote string
}

// Sender sends invitation emails. Implementations must not log raw tokens or accept URLs with tokens.
type Sender interface {
	SendInviteEmail(ctx context.Context, params SendInviteParams) error
}
