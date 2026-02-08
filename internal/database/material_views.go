package database

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

// GetUnreadMaterialIDsForParticipant returns material IDs in this session that the participant has not yet viewed.
func (db *DB) GetUnreadMaterialIDsForParticipant(ctx context.Context, sessionID uuid.UUID, participantRef string) ([]uuid.UUID, error) {
	query := `
		SELECT m.id FROM materials m
		WHERE m.session_id = $1
		AND NOT EXISTS (
			SELECT 1 FROM material_views v
			WHERE v.session_id = m.session_id AND v.participant_ref = $2 AND v.material_id = m.id
		)
		ORDER BY m.created_at
	`
	rows, err := db.Pool.Query(ctx, query, sessionID, participantRef)
	if err != nil {
		return nil, fmt.Errorf("failed to query unread materials: %w", err)
	}
	defer rows.Close()
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("failed to scan material id: %w", err)
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

// MarkMaterialsSeenByParticipant records that the participant has viewed the given materials in this session.
func (db *DB) MarkMaterialsSeenByParticipant(ctx context.Context, sessionID uuid.UUID, participantRef string, materialIDs []uuid.UUID) error {
	if len(materialIDs) == 0 {
		return nil
	}
	for _, mid := range materialIDs {
		_, err := db.Pool.Exec(ctx, `
			INSERT INTO material_views (session_id, participant_ref, material_id)
			VALUES ($1, $2, $3)
			ON CONFLICT (session_id, participant_ref, material_id) DO NOTHING
		`, sessionID, participantRef, mid)
		if err != nil {
			return fmt.Errorf("failed to mark material seen: %w", err)
		}
	}
	return nil
}
