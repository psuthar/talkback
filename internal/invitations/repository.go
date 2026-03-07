package invitations

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Repository does persistence for session invitations.
type Repository struct {
	Pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{Pool: pool}
}

func (r *Repository) Create(ctx context.Context, inv *Invitation) error {
	query := `
		INSERT INTO session_invitations (
			id, session_id, invited_email_normalized, inviter_user_id, invited_role,
			token_hash, status, expires_at, accepted_at, accepted_by_user_id, email_verified_at, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
	`
	_, err := r.Pool.Exec(ctx, query,
		inv.ID, inv.SessionID, inv.InvitedEmailNormalized, inv.InviterUserID, string(inv.InvitedRole),
		inv.TokenHash, string(inv.Status), inv.ExpiresAt, inv.AcceptedAt, inv.AcceptedByUserID, inv.EmailVerifiedAt,
		inv.CreatedAt, inv.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("create invitation: %w", err)
	}
	return nil
}

func (r *Repository) GetByID(ctx context.Context, id uuid.UUID) (*Invitation, error) {
	query := `
		SELECT id, session_id, invited_email_normalized, inviter_user_id, invited_role,
			token_hash, status, expires_at, accepted_at, accepted_by_user_id, email_verified_at, created_at, updated_at
		FROM session_invitations WHERE id = $1
	`
	return r.scanOne(r.Pool.QueryRow(ctx, query, id))
}

func (r *Repository) GetByTokenHash(ctx context.Context, tokenHash string) (*Invitation, error) {
	query := `
		SELECT id, session_id, invited_email_normalized, inviter_user_id, invited_role,
			token_hash, status, expires_at, accepted_at, accepted_by_user_id, email_verified_at, created_at, updated_at
		FROM session_invitations WHERE token_hash = $1
	`
	return r.scanOne(r.Pool.QueryRow(ctx, query, tokenHash))
}

func (r *Repository) scanOne(row pgx.Row) (*Invitation, error) {
	inv := &Invitation{}
	var role, status string
	err := row.Scan(
		&inv.ID, &inv.SessionID, &inv.InvitedEmailNormalized, &inv.InviterUserID, &role,
		&inv.TokenHash, &status, &inv.ExpiresAt, &inv.AcceptedAt, &inv.AcceptedByUserID, &inv.EmailVerifiedAt,
		&inv.CreatedAt, &inv.UpdatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	inv.InvitedRole = Role(role)
	inv.Status = Status(status)
	return inv, nil
}

func (r *Repository) UpdateStatus(ctx context.Context, id uuid.UUID, status Status, acceptedAt *time.Time, acceptedByUserID *uuid.UUID, emailVerifiedAt *time.Time) error {
	query := `
		UPDATE session_invitations SET status = $1, updated_at = now(),
			accepted_at = COALESCE($2, accepted_at),
			accepted_by_user_id = COALESCE($3, accepted_by_user_id),
			email_verified_at = COALESCE($4, email_verified_at)
		WHERE id = $5
	`
	_, err := r.Pool.Exec(ctx, query, string(status), acceptedAt, acceptedByUserID, emailVerifiedAt, id)
	if err != nil {
		return fmt.Errorf("update invitation status: %w", err)
	}
	return nil
}

func (r *Repository) UpdateTokenAndExpiry(ctx context.Context, id uuid.UUID, tokenHash string, expiresAt time.Time) error {
	_, err := r.Pool.Exec(ctx,
		`UPDATE session_invitations SET token_hash = $1, expires_at = $2, updated_at = now() WHERE id = $3`,
		tokenHash, expiresAt, id,
	)
	if err != nil {
		return fmt.Errorf("update invitation token/expiry: %w", err)
	}
	return nil
}

func (r *Repository) Revoke(ctx context.Context, id uuid.UUID) error {
	return r.UpdateStatus(ctx, id, StatusRevoked, nil, nil, nil)
}

func (r *Repository) ListBySessionID(ctx context.Context, sessionID uuid.UUID) ([]*Invitation, error) {
	query := `
		SELECT id, session_id, invited_email_normalized, inviter_user_id, invited_role,
			token_hash, status, expires_at, accepted_at, accepted_by_user_id, email_verified_at, created_at, updated_at
		FROM session_invitations WHERE session_id = $1 ORDER BY created_at DESC
	`
	rows, err := r.Pool.Query(ctx, query, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []*Invitation
	for rows.Next() {
		inv, err := r.scanOne(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, inv)
	}
	return list, rows.Err()
}

// PendingInvitationBySessionAndEmail returns a pending invitation for the given session and normalized email, if any.
func (r *Repository) PendingInvitationBySessionAndEmail(ctx context.Context, sessionID uuid.UUID, emailNormalized string) (*Invitation, error) {
	query := `
		SELECT id, session_id, invited_email_normalized, inviter_user_id, invited_role,
			token_hash, status, expires_at, accepted_at, accepted_by_user_id, email_verified_at, created_at, updated_at
		FROM session_invitations
		WHERE session_id = $1 AND invited_email_normalized = $2 AND status = 'pending' AND expires_at > now()
		ORDER BY created_at DESC LIMIT 1
	`
	return r.scanOne(r.Pool.QueryRow(ctx, query, sessionID, emailNormalized))
}

// RevokePendingBySessionAndEmail revokes all pending invitations for session+email (for "resend" flow).
func (r *Repository) RevokePendingBySessionAndEmail(ctx context.Context, sessionID uuid.UUID, emailNormalized string) (int, error) {
	res, err := r.Pool.Exec(ctx,
		`UPDATE session_invitations SET status = 'revoked', updated_at = now() WHERE session_id = $1 AND invited_email_normalized = $2 AND status = 'pending'`,
		sessionID, emailNormalized,
	)
	if err != nil {
		return 0, err
	}
	return int(res.RowsAffected()), nil
}
