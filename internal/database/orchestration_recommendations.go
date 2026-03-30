package database

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/models"
)

// CreateOrchestrationRecommendation inserts a new session-scoped recommendation.
func (db *DB) CreateOrchestrationRecommendation(ctx context.Context, rec *models.OrchestrationRecommendation) error {
	evidenceJSON, err := json.Marshal(rec.Evidence)
	if err != nil {
		return fmt.Errorf("marshal recommendation evidence: %w", err)
	}
	metadataJSON, err := json.Marshal(rec.MetadataJSON)
	if err != nil {
		return fmt.Errorf("marshal recommendation metadata: %w", err)
	}

	query := `
		INSERT INTO orchestration_recommendations (
			id,
			session_id,
			recommendation_type,
			status,
			summary,
			suggested_action,
			evidence_json,
			confidence_score,
			metadata_json
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8, $9::jsonb)
		RETURNING created_at, updated_at
	`

	if rec.ID == uuid.Nil {
		rec.ID = uuid.New()
	}
	if rec.Status == "" {
		rec.Status = models.RecommendationStatusNew
	}

	err = db.Pool.QueryRow(
		ctx,
		query,
		rec.ID,
		rec.SessionID,
		rec.RecommendationType,
		rec.Status,
		rec.Summary,
		rec.SuggestedAction,
		evidenceJSON,
		rec.ConfidenceScore,
		metadataJSON,
	).Scan(&rec.CreatedAt, &rec.UpdatedAt)
	if err != nil {
		return fmt.Errorf("create orchestration recommendation: %w", err)
	}
	return nil
}

// ListOrchestrationRecommendationsBySessionID returns recommendations for a session, newest first.
func (db *DB) ListOrchestrationRecommendationsBySessionID(ctx context.Context, sessionID uuid.UUID) ([]*models.OrchestrationRecommendation, error) {
	query := `
		SELECT
			id,
			session_id,
			recommendation_type,
			status,
			summary,
			suggested_action,
			evidence_json,
			confidence_score,
			metadata_json,
			created_at,
			updated_at
		FROM orchestration_recommendations
		WHERE session_id = $1
		ORDER BY created_at DESC
	`

	rows, err := db.Pool.Query(ctx, query, sessionID)
	if err != nil {
		return nil, fmt.Errorf("list orchestration recommendations: %w", err)
	}
	defer rows.Close()

	out := make([]*models.OrchestrationRecommendation, 0)
	for rows.Next() {
		rec := &models.OrchestrationRecommendation{}
		var evidenceJSON []byte
		var metadataJSON []byte
		if err := rows.Scan(
			&rec.ID,
			&rec.SessionID,
			&rec.RecommendationType,
			&rec.Status,
			&rec.Summary,
			&rec.SuggestedAction,
			&evidenceJSON,
			&rec.ConfidenceScore,
			&metadataJSON,
			&rec.CreatedAt,
			&rec.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan orchestration recommendation: %w", err)
		}
		if len(evidenceJSON) > 0 {
			if err := json.Unmarshal(evidenceJSON, &rec.Evidence); err != nil {
				return nil, fmt.Errorf("unmarshal recommendation evidence: %w", err)
			}
		}
		if len(metadataJSON) > 0 && string(metadataJSON) != "null" {
			if err := json.Unmarshal(metadataJSON, &rec.MetadataJSON); err != nil {
				return nil, fmt.Errorf("unmarshal recommendation metadata: %w", err)
			}
		}
		out = append(out, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate orchestration recommendations: %w", err)
	}
	return out, nil
}

// DeleteOrchestrationRecommendationsBySessionID removes all recommendations for a session (e.g. before re-sync).
func (db *DB) DeleteOrchestrationRecommendationsBySessionID(ctx context.Context, sessionID uuid.UUID) error {
	_, err := db.Pool.Exec(ctx, `DELETE FROM orchestration_recommendations WHERE session_id = $1`, sessionID)
	if err != nil {
		return fmt.Errorf("delete orchestration recommendations by session: %w", err)
	}
	return nil
}

// UpdateOrchestrationRecommendationStatus changes the recommendation lifecycle state.
func (db *DB) UpdateOrchestrationRecommendationStatus(ctx context.Context, recommendationID uuid.UUID, status models.OrchestrationRecommendationStatus) error {
	_, err := db.Pool.Exec(
		ctx,
		`UPDATE orchestration_recommendations SET status = $1, updated_at = now() WHERE id = $2`,
		status,
		recommendationID,
	)
	if err != nil {
		return fmt.Errorf("update orchestration recommendation status: %w", err)
	}
	return nil
}
