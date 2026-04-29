package database

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/psuthar/talkback/internal/models"
)

// CreateOrUpdateStance upserts a participant's stance for a session.
func (db *DB) CreateOrUpdateStance(ctx context.Context, sessionID, userID uuid.UUID, stance string, rationale *string) (*models.DecisionStance, error) {
	query := `
		INSERT INTO decision_stances (id, session_id, user_id, stance, rationale)
		VALUES (gen_random_uuid(), $1, $2, $3, $4)
		ON CONFLICT (session_id, user_id) DO UPDATE
			SET stance     = EXCLUDED.stance,
			    rationale  = EXCLUDED.rationale,
			    updated_at = now()
		RETURNING id, session_id, user_id, stance, rationale, created_at, updated_at
	`
	s := &models.DecisionStance{}
	err := db.Pool.QueryRow(ctx, query, sessionID, userID, stance, rationale).Scan(
		&s.ID, &s.SessionID, &s.UserID, &s.Stance, &s.Rationale, &s.CreatedAt, &s.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("CreateOrUpdateStance: %w", err)
	}
	return s, nil
}

// GetStanceByUserAndSession returns the stance row for a specific user in a session, or nil if none exists.
func (db *DB) GetStanceByUserAndSession(ctx context.Context, sessionID, userID uuid.UUID) (*models.DecisionStance, error) {
	query := `
		SELECT id, session_id, user_id, stance, rationale, created_at, updated_at
		FROM decision_stances
		WHERE session_id = $1 AND user_id = $2
	`
	s := &models.DecisionStance{}
	err := db.Pool.QueryRow(ctx, query, sessionID, userID).Scan(
		&s.ID, &s.SessionID, &s.UserID, &s.Stance, &s.Rationale, &s.CreatedAt, &s.UpdatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("GetStanceByUserAndSession: %w", err)
	}
	return s, nil
}

// GetStancesBySessionID returns all stances for a session joined with the submitter's email, newest first.
func (db *DB) GetStancesBySessionID(ctx context.Context, sessionID uuid.UUID) ([]*models.DecisionStanceWithUser, error) {
	query := `
		SELECT ds.id, ds.session_id, ds.user_id, ds.stance, ds.rationale, ds.created_at, ds.updated_at,
		       u.email
		FROM decision_stances ds
		JOIN users u ON u.id = ds.user_id
		WHERE ds.session_id = $1
		ORDER BY ds.updated_at DESC
	`
	rows, err := db.Pool.Query(ctx, query, sessionID)
	if err != nil {
		return nil, fmt.Errorf("GetStancesBySessionID: %w", err)
	}
	defer rows.Close()

	var out []*models.DecisionStanceWithUser
	for rows.Next() {
		sw := &models.DecisionStanceWithUser{}
		err := rows.Scan(
			&sw.ID, &sw.SessionID, &sw.UserID, &sw.Stance, &sw.Rationale, &sw.CreatedAt, &sw.UpdatedAt,
			&sw.UserEmail,
		)
		if err != nil {
			return nil, fmt.Errorf("GetStancesBySessionID scan: %w", err)
		}
		out = append(out, sw)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("GetStancesBySessionID rows: %w", err)
	}
	if out == nil {
		out = []*models.DecisionStanceWithUser{}
	}
	return out, nil
}

// GetStanceAggregate returns per-stance counts for a session.
func (db *DB) GetStanceAggregate(ctx context.Context, sessionID uuid.UUID) (*models.StanceAggregate, error) {
	query := `
		SELECT stance, COUNT(*) FROM decision_stances WHERE session_id = $1 GROUP BY stance
	`
	rows, err := db.Pool.Query(ctx, query, sessionID)
	if err != nil {
		return nil, fmt.Errorf("GetStanceAggregate: %w", err)
	}
	defer rows.Close()

	agg := &models.StanceAggregate{}
	for rows.Next() {
		var stance string
		var count int
		if err := rows.Scan(&stance, &count); err != nil {
			return nil, fmt.Errorf("GetStanceAggregate scan: %w", err)
		}
		switch stance {
		case models.StanceAgree:
			agg.Agree = count
		case models.StanceDisagree:
			agg.Disagree = count
		case models.StanceConditional:
			agg.Conditional = count
		case models.StanceAbstain:
			agg.Abstain = count
		case models.StanceNeedMoreInfo:
			agg.NeedMoreInfo = count
		}
		agg.Total += count
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("GetStanceAggregate rows: %w", err)
	}
	return agg, nil
}

// DeleteStance removes the current user's stance for the session. Idempotent (no error if no row).
func (db *DB) DeleteStance(ctx context.Context, sessionID, userID uuid.UUID) error {
	_, err := db.Pool.Exec(ctx, `DELETE FROM decision_stances WHERE session_id = $1 AND user_id = $2`, sessionID, userID)
	return err
}

// GetDecisionMakerReadiness reports total/voted Decision Makers on a session and whether the
// session is ready for closeout. ReadyToClose is true iff there is at least one Decision Maker,
// every Decision Maker has submitted a stance, primary_decision is set, and decision_outcome
// is not yet set. Sessions with zero Decision Makers always report ReadyToClose=false (the
// decision-maker readiness gate does not apply, but the field surface stays uniform).
func (db *DB) GetDecisionMakerReadiness(ctx context.Context, sessionID uuid.UUID) (*models.DecisionMakerReadiness, error) {
	const query = `
		WITH dms AS (
			SELECT user_id FROM session_memberships WHERE session_id = $1 AND role = 'decision_maker'
		),
		voted AS (
			SELECT COUNT(*)::int AS c
			FROM dms
			JOIN decision_stances ds ON ds.session_id = $1 AND ds.user_id = dms.user_id
		),
		ctx AS (
			SELECT
				COALESCE(NULLIF(primary_decision, ''), '') AS pd,
				COALESCE(NULLIF(decision_outcome, ''), '') AS doc
			FROM sessions WHERE id = $1
		)
		SELECT
			(SELECT COUNT(*)::int FROM dms) AS total,
			(SELECT c FROM voted) AS voted,
			(SELECT pd FROM ctx) AS primary_decision,
			(SELECT doc FROM ctx) AS decision_outcome
	`
	var total, voted int
	var primaryDecision, decisionOutcome string
	if err := db.Pool.QueryRow(ctx, query, sessionID).Scan(&total, &voted, &primaryDecision, &decisionOutcome); err != nil {
		return nil, fmt.Errorf("GetDecisionMakerReadiness: %w", err)
	}
	ready := total > 0 && voted == total && primaryDecision != "" && decisionOutcome == ""
	return &models.DecisionMakerReadiness{
		DecisionMakerTotal: total,
		DecisionMakerVoted: voted,
		ReadyToClose:       ready,
	}, nil
}
