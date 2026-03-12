package database

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/models"
)

// CreateSessionLink inserts a session link. ExtractedText and ErrorMessage should be set after verify+extract.
func (db *DB) CreateSessionLink(ctx context.Context, link *models.SessionLink) error {
	query := `
		INSERT INTO session_links (id, session_id, url, title, status, extracted_text, error_message, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, now(), now())
	`
	_, err := db.Pool.Exec(ctx, query,
		link.ID,
		link.SessionID,
		link.URL,
		link.Title,
		link.Status,
		link.ExtractedText,
		link.ErrorMessage,
	)
	if err != nil {
		return fmt.Errorf("create session link: %w", err)
	}
	return nil
}

// UpdateSessionLink updates status, title, extracted_text, error_message, updated_at.
func (db *DB) UpdateSessionLink(ctx context.Context, link *models.SessionLink) error {
	query := `
		UPDATE session_links SET title = $2, status = $3, extracted_text = $4, error_message = $5, updated_at = now()
		WHERE id = $1
	`
	_, err := db.Pool.Exec(ctx, query, link.ID, link.Title, link.Status, link.ExtractedText, link.ErrorMessage)
	if err != nil {
		return fmt.Errorf("update session link: %w", err)
	}
	return nil
}

// GetSessionLinkByID returns a link by id, or nil if not found.
func (db *DB) GetSessionLinkByID(ctx context.Context, id uuid.UUID) (*models.SessionLink, error) {
	query := `
		SELECT id, session_id, url, title, status, extracted_text, error_message, created_at, updated_at
		FROM session_links WHERE id = $1
	`
	var link models.SessionLink
	var title, extractedText, errMsg *string
	err := db.Pool.QueryRow(ctx, query, id).Scan(
		&link.ID,
		&link.SessionID,
		&link.URL,
		&title,
		&link.Status,
		&extractedText,
		&errMsg,
		&link.CreatedAt,
		&link.UpdatedAt,
	)
	if err != nil {
		if err.Error() == "no rows in result set" {
			return nil, nil
		}
		return nil, fmt.Errorf("get session link: %w", err)
	}
	link.Title = title
	link.ExtractedText = extractedText
	link.ErrorMessage = errMsg
	return &link, nil
}

// GetSessionLinksBySessionID returns all links for the session (for API list). Does not include extracted_text to keep payload small.
func (db *DB) GetSessionLinksBySessionID(ctx context.Context, sessionID uuid.UUID) ([]*models.SessionLink, error) {
	query := `
		SELECT id, session_id, url, title, status, error_message, created_at, updated_at
		FROM session_links WHERE session_id = $1 ORDER BY created_at
	`
	rows, err := db.Pool.Query(ctx, query, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list session links: %w", err)
	}
	defer rows.Close()
	var out []*models.SessionLink
	for rows.Next() {
		var link models.SessionLink
		var title, errMsg *string
		err := rows.Scan(&link.ID, &link.SessionID, &link.URL, &title, &link.Status, &errMsg, &link.CreatedAt, &link.UpdatedAt)
		if err != nil {
			return nil, err
		}
		link.Title = title
		link.ErrorMessage = errMsg
		out = append(out, &link)
	}
	if out == nil {
		out = []*models.SessionLink{}
	}
	return out, rows.Err()
}

// GetVerifiedSessionLinksBySessionID returns links with status=verified and non-empty extracted_text (for RAG indexing).
func (db *DB) GetVerifiedSessionLinksBySessionID(ctx context.Context, sessionID uuid.UUID) ([]*models.SessionLink, error) {
	query := `
		SELECT id, session_id, url, title, status, extracted_text, error_message, created_at, updated_at
		FROM session_links
		WHERE session_id = $1 AND status = 'verified' AND extracted_text IS NOT NULL AND trim(extracted_text) != ''
		ORDER BY created_at
	`
	rows, err := db.Pool.Query(ctx, query, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list verified session links: %w", err)
	}
	defer rows.Close()
	var out []*models.SessionLink
	for rows.Next() {
		var link models.SessionLink
		var title, extractedText, errMsg *string
		err := rows.Scan(&link.ID, &link.SessionID, &link.URL, &title, &link.Status, &extractedText, &errMsg, &link.CreatedAt, &link.UpdatedAt)
		if err != nil {
			return nil, err
		}
		link.Title = title
		link.ExtractedText = extractedText
		link.ErrorMessage = errMsg
		out = append(out, &link)
	}
	if out == nil {
		out = []*models.SessionLink{}
	}
	return out, rows.Err()
}

// CountSessionLinksBySessionID returns the number of links for the session (for cap enforcement).
func (db *DB) CountSessionLinksBySessionID(ctx context.Context, sessionID uuid.UUID) (int, error) {
	var n int
	err := db.Pool.QueryRow(ctx, `SELECT COUNT(*) FROM session_links WHERE session_id = $1`, sessionID).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count session links: %w", err)
	}
	return n, nil
}

// DeleteSessionLink deletes a link by id. Caller should delete chunks with source_type='link' and source_id=id, then reindex if desired.
func (db *DB) DeleteSessionLink(ctx context.Context, id uuid.UUID) error {
	_, err := db.Pool.Exec(ctx, `DELETE FROM session_links WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete session link: %w", err)
	}
	return nil
}
