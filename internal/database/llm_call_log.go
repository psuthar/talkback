package database

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
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

// CountLLMCallsBySiteAndUserSince returns the row count for one
// (site, user_id) pair since the given time. Backs the SCRUM-566
// per-user judge-call rate limit — when this returns >= the configured
// cap, qa.go skips the grounding judge for that request and logs
// guardrails_fired=[grounding_judge_rate_limited] on the resulting
// llm_call_log row. Cheap O(log N) via the (site, ts DESC) +
// (user_id) indexes; the per-user filter is selective enough that the
// scan stays bounded even at high QA volume.
func (db *DB) CountLLMCallsBySiteAndUserSince(ctx context.Context, site string, userID uuid.UUID, since time.Time) (int, error) {
	var n int
	err := db.Pool.QueryRow(
		ctx,
		`SELECT count(*) FROM llm_call_log WHERE site = $1 AND user_id = $2 AND ts >= $3`,
		site, userID, since,
	).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count llm_call_log by site+user+since: %w", err)
	}
	return n, nil
}

// SumLLMCallTokens returns the SUM of input_tokens and output_tokens
// across all llm_call_log rows since the given time. SCRUM-578 (Slice 1
// of SCRUM-577): backs the per-window cost rollup the admin UI surfaces
// as a 5th big-number card.
//
// Both returns are nullable: NULL when no rows in the window, or when
// every row has NULL token columns (the obsworker site logs without
// token counts since LLMClient.Complete doesn't expose them). The
// SUM() aggregate returns NULL on empty / all-null inputs — passed
// straight through to the JSON payload so the UI can render "—" with
// a "no token data in window" subtitle.
func (db *DB) SumLLMCallTokens(ctx context.Context, since time.Time) (inputTokens, outputTokens *int64, err error) {
	err = db.Pool.QueryRow(
		ctx,
		`SELECT SUM(input_tokens), SUM(output_tokens) FROM llm_call_log WHERE ts >= $1`,
		since,
	).Scan(&inputTokens, &outputTokens)
	if err != nil {
		return nil, nil, fmt.Errorf("sum llm_call_log tokens: %w", err)
	}
	return inputTokens, outputTokens, nil
}

// CountLLMCallsByModel groups rows by their `model` enum value since
// the given time. SCRUM-578 (Slice 1 of SCRUM-577): lets the admin UI
// answer "which models are we calling and how often" alongside the
// existing by_site rollup.
//
// Empty groups omitted (same as CountLLMCallsBySite). Empty-string
// models — which the qa.go refusal paths emit when no LLM was invoked
// — are excluded so the Models table doesn't get a "" row that means
// nothing to an operator.
func (db *DB) CountLLMCallsByModel(ctx context.Context, since time.Time) (map[string]int, error) {
	rows, err := db.Pool.Query(
		ctx,
		`SELECT model, count(*) FROM llm_call_log
		 WHERE ts >= $1 AND model IS NOT NULL AND model <> ''
		 GROUP BY model`,
		since,
	)
	if err != nil {
		return nil, fmt.Errorf("group llm_call_log by model: %w", err)
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var model string
		var n int
		if err := rows.Scan(&model, &n); err != nil {
			return nil, err
		}
		out[model] = n
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
