package database

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/psuthar/talkback/internal/models"
)

// ErrOrchestrationRecommendationNotFound is returned when no recommendation row matches id and session.
var ErrOrchestrationRecommendationNotFound = errors.New("orchestration recommendation not found")

// UpdateOrchestrationRecommendationStatusWithAudit updates status and appends an audit row when the status value changes.
func (db *DB) UpdateOrchestrationRecommendationStatusWithAudit(
	ctx context.Context,
	sessionID, recommendationID uuid.UUID,
	newStatus models.OrchestrationRecommendationStatus,
	actorUserID uuid.UUID,
	clientSurface *string,
) error {
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	var fromStr string
	err = tx.QueryRow(ctx, `
		SELECT status::text FROM orchestration_recommendations
		WHERE id = $1 AND session_id = $2
		FOR UPDATE
	`, recommendationID, sessionID).Scan(&fromStr)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrOrchestrationRecommendationNotFound
		}
		return fmt.Errorf("load orchestration recommendation: %w", err)
	}
	if fromStr == string(newStatus) {
		return tx.Commit(ctx)
	}

	cmd, err := tx.Exec(ctx, `
		UPDATE orchestration_recommendations
		SET status = $1, updated_at = now()
		WHERE id = $2 AND session_id = $3
	`, newStatus, recommendationID, sessionID)
	if err != nil {
		return fmt.Errorf("update orchestration recommendation status: %w", err)
	}
	if cmd.RowsAffected() == 0 {
		return ErrOrchestrationRecommendationNotFound
	}

	_, err = tx.Exec(ctx, `
		INSERT INTO orchestration_recommendation_status_audit (
			session_id,
			recommendation_id,
			actor_user_id,
			from_status,
			to_status,
			client_surface
		)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, sessionID, recommendationID, actorUserID, fromStr, string(newStatus), clientSurface)
	if err != nil {
		return fmt.Errorf("insert orchestration recommendation status audit: %w", err)
	}

	return tx.Commit(ctx)
}

// ListOrchestrationRecommendationStatusAuditBySessionID returns recent status-change audit rows for a session (newest first).
func (db *DB) ListOrchestrationRecommendationStatusAuditBySessionID(ctx context.Context, sessionID uuid.UUID, limit int) ([]*models.OrchestrationRecommendationStatusAudit, error) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	rows, err := db.Pool.Query(ctx, `
		SELECT
			id,
			session_id,
			recommendation_id,
			actor_user_id,
			from_status,
			to_status,
			client_surface,
			created_at
		FROM orchestration_recommendation_status_audit
		WHERE session_id = $1
		ORDER BY created_at DESC
		LIMIT $2
	`, sessionID, limit)
	if err != nil {
		return nil, fmt.Errorf("list orchestration recommendation status audit: %w", err)
	}
	defer rows.Close()

	out := make([]*models.OrchestrationRecommendationStatusAudit, 0)
	for rows.Next() {
		a := &models.OrchestrationRecommendationStatusAudit{}
		var fromStatus *string
		var clientSurf *string
		if err := rows.Scan(
			&a.ID,
			&a.SessionID,
			&a.RecommendationID,
			&a.ActorUserID,
			&fromStatus,
			&a.ToStatus,
			&clientSurf,
			&a.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan orchestration recommendation status audit: %w", err)
		}
		if fromStatus != nil && *fromStatus != "" {
			s := models.OrchestrationRecommendationStatus(*fromStatus)
			a.FromStatus = &s
		}
		a.ClientSurface = clientSurf
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate orchestration recommendation status audit: %w", err)
	}
	return out, nil
}
