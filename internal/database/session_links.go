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

// GetSessionLinksByIDs returns the links whose ids are in the provided slice
// (single query, no extracted_text). Used by ListSessions to resolve session
// primary descriptors in O(1) extra queries regardless of page size. Empty
// input → empty map; missing ids are silently absent.
func (db *DB) GetSessionLinksByIDs(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]*models.SessionLink, error) {
	out := make(map[uuid.UUID]*models.SessionLink, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	query := `
		SELECT id, session_id, url, title, status, error_message, created_at, updated_at
		FROM session_links WHERE id = ANY($1)
	`
	rows, err := db.Pool.Query(ctx, query, ids)
	if err != nil {
		return nil, fmt.Errorf("query session links by ids: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var l models.SessionLink
		var title, errMsg *string
		if err := rows.Scan(
			&l.ID,
			&l.SessionID,
			&l.URL,
			&title,
			&l.Status,
			&errMsg,
			&l.CreatedAt,
			&l.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan session link: %w", err)
		}
		l.Title = title
		l.ErrorMessage = errMsg
		out[l.ID] = &l
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate session links by ids: %w", err)
	}
	return out, nil
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
//
// SCRUM-282: if the link is the session's primary, clear
// primary_content_kind + primary_session_link_id in the same transaction so
// the session can't end up with kind='link' and the pointer NULL — the
// silent-empty state the FK SET-NULL alone would leave behind.
func (db *DB) DeleteSessionLink(ctx context.Context, id uuid.UUID) error {
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `
		UPDATE sessions
		SET primary_content_kind = NULL, primary_session_link_id = NULL
		WHERE primary_content_kind = 'link' AND primary_session_link_id = $1`, id); err != nil {
		return fmt.Errorf("clear session primary on link delete: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM session_links WHERE id = $1`, id); err != nil {
		return fmt.Errorf("delete session link: %w", err)
	}
	return tx.Commit(ctx)
}
