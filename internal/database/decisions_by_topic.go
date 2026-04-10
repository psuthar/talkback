package database

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// DecisionTopicRow is one session whose premise / primary_decision / decision_outcome matches a topic substring (SCRUM-64).
type DecisionTopicRow struct {
	SessionID       uuid.UUID
	Title           string
	UpdatedAt       time.Time
	Premise         *string
	PrimaryDecision *string
	DecisionOutcome *string
	StanceCount     int
}

// MaxDecisionTopicResults caps rows returned for MCP get_decisions_by_topic.
const MaxDecisionTopicResults = 100

// ListSessionsMatchingDecisionTopic returns sessions the caller may access (via accessibleIDs) where the topic
// matches as a case-insensitive substring of premise, primary_decision, or decision_outcome. Ordered by updated_at DESC.
func (db *DB) ListSessionsMatchingDecisionTopic(ctx context.Context, accessibleIDs []uuid.UUID, topic string, limit int) ([]DecisionTopicRow, error) {
	if len(accessibleIDs) == 0 {
		return nil, nil
	}
	if limit <= 0 {
		limit = 40
	}
	if limit > MaxDecisionTopicResults {
		limit = MaxDecisionTopicResults
	}

	rows, err := db.Pool.Query(ctx, `
		SELECT s.id, s.title, s.updated_at, s.premise, s.primary_decision, s.decision_outcome,
			(SELECT COUNT(*)::int FROM decision_stances ds WHERE ds.session_id = s.id) AS stance_count
		FROM sessions s
		WHERE s.id = ANY($1::uuid[])
		AND (
			(s.premise IS NOT NULL AND position(lower($2::text) IN lower(s.premise)) > 0)
			OR (s.primary_decision IS NOT NULL AND position(lower($2::text) IN lower(s.primary_decision)) > 0)
			OR (s.decision_outcome IS NOT NULL AND position(lower($2::text) IN lower(s.decision_outcome)) > 0)
		)
		ORDER BY s.updated_at DESC
		LIMIT $3
	`, accessibleIDs, topic, limit)
	if err != nil {
		return nil, fmt.Errorf("list sessions matching decision topic: %w", err)
	}
	defer rows.Close()

	var out []DecisionTopicRow
	for rows.Next() {
		var r DecisionTopicRow
		if err := rows.Scan(&r.SessionID, &r.Title, &r.UpdatedAt, &r.Premise, &r.PrimaryDecision, &r.DecisionOutcome, &r.StanceCount); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
