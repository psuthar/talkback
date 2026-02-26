package database

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/psuthar/talkback/internal/models"
)

// CreateUser inserts a new user. Email should already be normalized (trimmed, lowercased).
func (db *DB) CreateUser(ctx context.Context, user *models.User) error {
	query := `
		INSERT INTO users (id, email, display_name, status, global_role)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING created_at, updated_at
	`
	err := db.Pool.QueryRow(ctx, query,
		user.ID,
		user.Email,
		user.DisplayName,
		string(user.Status),
		string(user.GlobalRole),
	).Scan(&user.CreatedAt, &user.UpdatedAt)
	if err != nil {
		return fmt.Errorf("failed to create user: %w", err)
	}
	return nil
}

// GetUserByID returns the user by ID.
func (db *DB) GetUserByID(ctx context.Context, id uuid.UUID) (*models.User, error) {
	return db.getUserBy(ctx, "id = $1", id)
}

// GetUserByEmail returns the user by email (case-insensitive via lower(email)).
func (db *DB) GetUserByEmail(ctx context.Context, email string) (*models.User, error) {
	return db.getUserBy(ctx, "lower(email) = lower($1)", email)
}

func (db *DB) getUserBy(ctx context.Context, where string, arg interface{}) (*models.User, error) {
	query := `SELECT id, email, display_name, status, global_role, created_at, updated_at, last_login_at FROM users WHERE ` + where
	u := &models.User{}
	err := db.Pool.QueryRow(ctx, query, arg).Scan(
		&u.ID, &u.Email, &u.DisplayName, &u.Status, &u.GlobalRole,
		&u.CreatedAt, &u.UpdatedAt, &u.LastLoginAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get user: %w", err)
	}
	return u, nil
}

// UpdateUserLastLoginAt sets last_login_at to now() for the user.
func (db *DB) UpdateUserLastLoginAt(ctx context.Context, userID uuid.UUID) error {
	_, err := db.Pool.Exec(ctx, `UPDATE users SET last_login_at = now(), updated_at = now() WHERE id = $1`, userID)
	return err
}

// SetUserGlobalRole sets global_role for the user (e.g. bootstrap admin).
func (db *DB) SetUserGlobalRole(ctx context.Context, userID uuid.UUID, role models.GlobalRole) error {
	_, err := db.Pool.Exec(ctx, `UPDATE users SET global_role = $1, updated_at = now() WHERE id = $2`, string(role), userID)
	return err
}

// CountUsers returns the number of users (for bootstrap: first user can be admin).
func (db *DB) CountUsers(ctx context.Context) (int, error) {
	var n int
	err := db.Pool.QueryRow(ctx, `SELECT count(*) FROM users`).Scan(&n)
	return n, err
}

// CountAdmins returns the number of users with global_role = 'admin'.
func (db *DB) CountAdmins(ctx context.Context) (int, error) {
	var n int
	err := db.Pool.QueryRow(ctx, `SELECT count(*) FROM users WHERE global_role = $1`, string(models.GlobalRoleAdmin)).Scan(&n)
	return n, err
}

// ListUsers returns all users (for admin list).
func (db *DB) ListUsers(ctx context.Context) ([]*models.User, error) {
	query := `SELECT id, email, display_name, status, global_role, created_at, updated_at, last_login_at FROM users ORDER BY created_at`
	rows, err := db.Pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to list users: %w", err)
	}
	defer rows.Close()
	var out []*models.User
	for rows.Next() {
		u := &models.User{}
		err := rows.Scan(&u.ID, &u.Email, &u.DisplayName, &u.Status, &u.GlobalRole, &u.CreatedAt, &u.UpdatedAt, &u.LastLoginAt)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// DeleteUser deletes a user by ID. Cascades to password_credentials, login_sessions, session_invitations.
func (db *DB) DeleteUser(ctx context.Context, userID uuid.UUID) error {
	_, err := db.Pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, userID)
	return err
}

// CreatePasswordCredential inserts a password credential for the user.
func (db *DB) CreatePasswordCredential(ctx context.Context, cred *models.PasswordCredential) error {
	query := `
		INSERT INTO password_credentials (user_id, password_hash, password_updated_at)
		VALUES ($1, $2, $3)
		RETURNING created_at, updated_at
	`
	err := db.Pool.QueryRow(ctx, query, cred.UserID, cred.PasswordHash, cred.PasswordUpdatedAt).Scan(&cred.CreatedAt, &cred.UpdatedAt)
	if err != nil {
		return fmt.Errorf("failed to create password credential: %w", err)
	}
	return nil
}

// GetPasswordCredentialByUserID returns the password credential for the user, or nil.
func (db *DB) GetPasswordCredentialByUserID(ctx context.Context, userID uuid.UUID) (*models.PasswordCredential, error) {
	query := `SELECT user_id, password_hash, password_updated_at, created_at, updated_at FROM password_credentials WHERE user_id = $1`
	c := &models.PasswordCredential{}
	err := db.Pool.QueryRow(ctx, query, userID).Scan(&c.UserID, &c.PasswordHash, &c.PasswordUpdatedAt, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get password credential: %w", err)
	}
	return c, nil
}

// UpdatePasswordCredential updates the password hash for the user (e.g. bootstrap admin sync).
func (db *DB) UpdatePasswordCredential(ctx context.Context, userID uuid.UUID, passwordHash string) error {
	_, err := db.Pool.Exec(ctx, `UPDATE password_credentials SET password_hash = $1, password_updated_at = now(), updated_at = now() WHERE user_id = $2`, passwordHash, userID)
	return err
}
