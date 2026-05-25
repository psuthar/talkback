package database

import (
	"context"
	"fmt"
	"time"

	"github.com/psuthar/talkback/internal/guardrails"
)

// InsertLLMCallRow persists one telemetry row from the guardrails buffer.
// Implements the guardrails.Writer interface; wire via guardrails.Init(db).
// Schema + field semantics: docs/guardrails/log-shape.md.
func (db *DB) InsertLLMCallRow(ctx context.Context, row guardrails.LLMCallRow) error {
	const q = `
		INSERT INTO llm_call_log (
			id, ts, site, model, user_id, session_id, prompt_hash,
			input_tokens, output_tokens, latency_ms,
			guardrails_fired, decision, refusal_code, refusal_user_message
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7,
			$8, $9, $10,
			$11, $12, $13, $14
		)
	`
	_, err := db.Pool.Exec(
		ctx, q,
		row.ID, row.TS, row.Site, row.Model, row.UserID, row.SessionID, row.PromptHash,
		row.InputTokens, row.OutputTokens, row.LatencyMS,
		row.GuardrailsFired, row.Decision, row.RefusalCode, row.RefusalUserMessage,
	)
	if err != nil {
		return fmt.Errorf("insert llm_call_log: %w", err)
	}
	return nil
}

// CountLLMCalls returns the total rows since the given time. Backs the
// /api/admin/llm-stats endpoint's headline metric.
func (db *DB) CountLLMCalls(ctx context.Context, since time.Time) (int, error) {
	var n int
	err := db.Pool.QueryRow(
		ctx, `SELECT count(*) FROM llm_call_log WHERE ts >= $1`, since,
	).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count llm_call_log: %w", err)
	}
	return n, nil
}

// CountLLMCallsByDecision groups rows by their `decision` enum value
// (allowed | refused | redacted). Empty groups are omitted.
func (db *DB) CountLLMCallsByDecision(ctx context.Context, since time.Time) (map[string]int, error) {
	rows, err := db.Pool.Query(
		ctx,
		`SELECT decision, count(*) FROM llm_call_log WHERE ts >= $1 GROUP BY decision`,
		since,
	)
	if err != nil {
		return nil, fmt.Errorf("group llm_call_log by decision: %w", err)
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var decision string
		var n int
		if err := rows.Scan(&decision, &n); err != nil {
			return nil, err
		}
		out[decision] = n
	}
	return out, rows.Err()
}

// CountLLMCallsBySite groups rows by their `site` enum value. Empty
// groups are omitted.
func (db *DB) CountLLMCallsBySite(ctx context.Context, since time.Time) (map[string]int, error) {
	rows, err := db.Pool.Query(
		ctx,
		`SELECT site, count(*) FROM llm_call_log WHERE ts >= $1 GROUP BY site`,
		since,
	)
	if err != nil {
		return nil, fmt.Errorf("group llm_call_log by site: %w", err)
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var site string
		var n int
		if err := rows.Scan(&site, &n); err != nil {
			return nil, err
		}
		out[site] = n
	}
	return out, rows.Err()
}

// LLMCallRefusalCodeCount is a single `(code, count)` row for the
// top-refusal-codes admin rollup.
type LLMCallRefusalCodeCount struct {
	Code  string `json:"code"`
	Count int    `json:"count"`
}

// TopLLMCallRefusalCodes returns the top `limit` refusal_code values (by
// count, descending) since the given time. Only counts rows where
// decision='refused' and refusal_code IS NOT NULL.
func (db *DB) TopLLMCallRefusalCodes(ctx context.Context, since time.Time, limit int) ([]LLMCallRefusalCodeCount, error) {
	if limit <= 0 {
		limit = 3
	}
	rows, err := db.Pool.Query(
		ctx,
		`SELECT refusal_code, count(*) AS c
		 FROM llm_call_log
		 WHERE ts >= $1 AND decision = 'refused' AND refusal_code IS NOT NULL
		 GROUP BY refusal_code
		 ORDER BY c DESC, refusal_code ASC
		 LIMIT $2`,
		since, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("top llm_call_log refusal codes: %w", err)
	}
	defer rows.Close()
	out := []LLMCallRefusalCodeCount{}
	for rows.Next() {
		var code string
		var n int
		if err := rows.Scan(&code, &n); err != nil {
			return nil, err
		}
		out = append(out, LLMCallRefusalCodeCount{Code: code, Count: n})
	}
	return out, rows.Err()
}

// P95LLMCallLatencyMS returns the 95th-percentile latency across all
// rows since the given time. Nil when no rows. Computed via Postgres
// percentile_cont so the math matches across deploys.
func (db *DB) P95LLMCallLatencyMS(ctx context.Context, since time.Time) (*float64, error) {
	var v *float64
	err := db.Pool.QueryRow(
		ctx,
		`SELECT percentile_cont(0.95) WITHIN GROUP (ORDER BY latency_ms)
		 FROM llm_call_log
		 WHERE ts >= $1`,
		since,
	).Scan(&v)
	if err != nil {
		return nil, fmt.Errorf("p95 llm_call_log latency: %w", err)
	}
	return v, nil
}
