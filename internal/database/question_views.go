package database

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

// GetUnreadQuestionIDs returns question IDs in the session that the participant has not yet
// "viewed" (expanded), or where new content (answer or reply) appeared after their last view.
// content_updated_at = max(question.created_at, answer.created_at if any, max(child.created_at) if any).
func (db *DB) GetUnreadQuestionIDs(ctx context.Context, sessionID uuid.UUID, participantRef string) ([]uuid.UUID, error) {
	query := `
		WITH content_at AS (
			SELECT q.id,
				GREATEST(
					q.created_at,
					COALESCE((SELECT MAX(a.created_at) FROM answers a WHERE a.question_id = q.id), q.created_at),
					COALESCE((SELECT MAX(c.created_at) FROM questions c WHERE c.parent_question_id = q.id), q.created_at)
				) AS content_updated_at
			FROM questions q
			WHERE q.session_id = $1
		)
		SELECT c.id FROM content_at c
		LEFT JOIN question_views v ON v.session_id = $1 AND v.participant_ref = $2 AND v.question_id = c.id
		WHERE v.viewed_at IS NULL OR c.content_updated_at > v.viewed_at
		ORDER BY c.id
	`
	rows, err := db.Pool.Query(ctx, query, sessionID, participantRef)
	if err != nil {
		return nil, fmt.Errorf("failed to query unread questions: %w", err)
	}
	defer rows.Close()
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("failed to scan question id: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if ids == nil {
		ids = []uuid.UUID{}
	}
	return ids, nil
}

// MarkQuestionViewed records that the participant viewed (expanded) the given question.
func (db *DB) MarkQuestionViewed(ctx context.Context, sessionID uuid.UUID, participantRef string, questionID uuid.UUID) error {
	_, err := db.Pool.Exec(ctx, `
		INSERT INTO question_views (session_id, participant_ref, question_id, viewed_at)
		VALUES ($1, $2, $3, now())
		ON CONFLICT (session_id, participant_ref, question_id) DO UPDATE SET viewed_at = now()
	`, sessionID, participantRef, questionID)
	if err != nil {
		return fmt.Errorf("failed to mark question viewed: %w", err)
	}
	return nil
}
