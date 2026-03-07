package invitations

import "errors"

var (
	ErrNotFound         = errors.New("invitation not found")
	ErrInvalidToken     = errors.New("invalid invitation token")
	ErrExpired          = errors.New("invitation expired")
	ErrRevoked          = errors.New("invitation revoked")
	ErrAlreadyAccepted  = errors.New("invitation already accepted")
	ErrWrongEmail       = errors.New("authenticated user email does not match invited email")
	ErrAlreadyMember    = errors.New("user is already a member of this session")
	ErrAccountExists    = errors.New("account already exists for this email; use sign-in flow")
	ErrNoAccount        = errors.New("no account for this email; use register-and-accept flow")
	ErrInvalidRole      = errors.New("invalid role; must be participant or creator")
	ErrUnauthorized     = errors.New("not authorized to perform this action on the invitation")
)
