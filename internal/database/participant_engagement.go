package database

import (
	"context"

	"github.com/google/uuid"
)

// ParticipantHasAnyMaterialView reports whether the participant has at least one material_views row for the session.
func (db *DB) ParticipantHasAnyMaterialView(ctx context.Context, sessionID uuid.UUID, participantRef string) (bool, error) {
	var exists bool
	err := db.Pool.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM material_views
			WHERE session_id = $1 AND participant_ref = $2
		)
	`, sessionID, participantRef).Scan(&exists)
	if err != nil {
		return false, err
	}
	return exists, nil
}

// ParticipantHasAnyQuestionView reports whether the participant has at least one question_views row for the session.
func (db *DB) ParticipantHasAnyQuestionView(ctx context.Context, sessionID uuid.UUID, participantRef string) (bool, error) {
	var exists bool
	err := db.Pool.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM question_views
			WHERE session_id = $1 AND participant_ref = $2
		)
	`, sessionID, participantRef).Scan(&exists)
	if err != nil {
		return false, err
	}
	return exists, nil
}
