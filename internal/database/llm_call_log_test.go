package database

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/guardrails"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// truncateLLMCallLog isolates SCRUM-568 tests from each other and from
// test.TruncateTables (which CASCADEs from sessions/users/artifacts and
// doesn't touch llm_call_log — the new table has no FKs to those roots).
func truncateLLMCallLog(t *testing.T, db *DB) {
	t.Helper()
	_, err := db.Pool.Exec(context.Background(), "TRUNCATE TABLE llm_call_log")
	require.NoError(t, err)
}

func sampleRow(site string) guardrails.LLMCallRow {
	return guardrails.LLMCallRow{
		ID:              uuid.New(),
		TS:              time.Now().UTC(),
		Site:            site,
		Model:           "gpt-4o-mini",
		PromptHash:      guardrails.HashPrompt(site, "hello"),
		InputTokens:     guardrails.IntPtr(120),
		OutputTokens:    guardrails.IntPtr(30),
		LatencyMS:       50,
		GuardrailsFired: []string{},
		Decision:        "allowed",
	}
}

func TestInsertLLMCallRow_Persists(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer truncateLLMCallLog(t, db)
	ctx := context.Background()

	row := sampleRow("qa_ask")
	require.NoError(t, db.InsertLLMCallRow(ctx, row))

	n, err := db.CountLLMCalls(ctx, time.Now().Add(-1*time.Hour))
	require.NoError(t, err)
	assert.Equal(t, 1, n)
}

func TestInsertLLMCallRow_WithRefusal(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer truncateLLMCallLog(t, db)
	ctx := context.Background()

	row := sampleRow("qa_ask")
	row.Decision = "refused"
	row.GuardrailsFired = []string{"input_injection"}
	row.RefusalCode = guardrails.StrPtr("input_injection")
	row.RefusalUserMessage = guardrails.StrPtr("Question detected to have unsafe content — not processed.")
	require.NoError(t, db.InsertLLMCallRow(ctx, row))

	since := time.Now().Add(-1 * time.Hour)
	byDecision, err := db.CountLLMCallsByDecision(ctx, since)
	require.NoError(t, err)
	assert.Equal(t, 1, byDecision["refused"])

	top, err := db.TopLLMCallRefusalCodes(ctx, since, 3)
	require.NoError(t, err)
	require.Len(t, top, 1)
	assert.Equal(t, "input_injection", top[0].Code)
	assert.Equal(t, 1, top[0].Count)
}

func TestCountLLMCallsBySite_GroupsCorrectly(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer truncateLLMCallLog(t, db)
	ctx := context.Background()

	for _, site := range []string{"qa_ask", "qa_ask", "qa_ask", "action_items", "question_polish", "question_polish"} {
		require.NoError(t, db.InsertLLMCallRow(ctx, sampleRow(site)))
	}

	bySite, err := db.CountLLMCallsBySite(ctx, time.Now().Add(-1*time.Hour))
	require.NoError(t, err)
	assert.Equal(t, 3, bySite["qa_ask"])
	assert.Equal(t, 1, bySite["action_items"])
	assert.Equal(t, 2, bySite["question_polish"])
}

func TestTopLLMCallRefusalCodes_OrderedByCount(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer truncateLLMCallLog(t, db)
	ctx := context.Background()

	// 5 input_injection, 3 grounding_failed, 1 citation_missing → top 3 in that order.
	codes := []string{
		"input_injection", "input_injection", "input_injection", "input_injection", "input_injection",
		"grounding_failed", "grounding_failed", "grounding_failed",
		"citation_missing",
	}
	for _, code := range codes {
		row := sampleRow("qa_ask")
		row.Decision = "refused"
		row.GuardrailsFired = []string{code}
		row.RefusalCode = guardrails.StrPtr(code)
		require.NoError(t, db.InsertLLMCallRow(ctx, row))
	}

	top, err := db.TopLLMCallRefusalCodes(ctx, time.Now().Add(-1*time.Hour), 3)
	require.NoError(t, err)
	require.Len(t, top, 3)
	assert.Equal(t, "input_injection", top[0].Code)
	assert.Equal(t, 5, top[0].Count)
	assert.Equal(t, "grounding_failed", top[1].Code)
	assert.Equal(t, 3, top[1].Count)
	assert.Equal(t, "citation_missing", top[2].Code)
	assert.Equal(t, 1, top[2].Count)
}

func TestP95LLMCallLatencyMS_ComputesOverWindow(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer truncateLLMCallLog(t, db)
	ctx := context.Background()

	// 19 rows at 100ms + 1 at 1000ms → p95 with percentile_cont is between
	// the 95th-percentile data points; for n=20 the percentile_cont(0.95)
	// interpolates near the top of the distribution.
	for i := 0; i < 19; i++ {
		row := sampleRow("qa_ask")
		row.LatencyMS = 100
		require.NoError(t, db.InsertLLMCallRow(ctx, row))
	}
	row := sampleRow("qa_ask")
	row.LatencyMS = 1000
	require.NoError(t, db.InsertLLMCallRow(ctx, row))

	p95, err := db.P95LLMCallLatencyMS(ctx, time.Now().Add(-1*time.Hour))
	require.NoError(t, err)
	require.NotNil(t, p95)
	// percentile_cont(0.95) over [100]*19 + [1000] = 100 + 0.05*(1000-100) = 145ms.
	assert.InDelta(t, 145.0, *p95, 1.0, "percentile_cont(0.95) over the test set")
}

func TestP95LLMCallLatencyMS_NullOnEmpty(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer truncateLLMCallLog(t, db)
	ctx := context.Background()

	p95, err := db.P95LLMCallLatencyMS(ctx, time.Now().Add(-1*time.Hour))
	require.NoError(t, err)
	assert.Nil(t, p95)
}

func TestCountLLMCalls_RespectsSinceWindow(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	defer truncateLLMCallLog(t, db)
	ctx := context.Background()

	old := sampleRow("qa_ask")
	old.TS = time.Now().Add(-48 * time.Hour)
	require.NoError(t, db.InsertLLMCallRow(ctx, old))

	recent := sampleRow("qa_ask")
	require.NoError(t, db.InsertLLMCallRow(ctx, recent))

	n7d, err := db.CountLLMCalls(ctx, time.Now().Add(-7*24*time.Hour))
	require.NoError(t, err)
	assert.Equal(t, 2, n7d, "both rows within 7-day window")

	n1h, err := db.CountLLMCalls(ctx, time.Now().Add(-1*time.Hour))
	require.NoError(t, err)
	assert.Equal(t, 1, n1h, "only the recent row within 1-hour window")
}

func TestSchemaIndexesExist(t *testing.T) {
	db := setupTestDB(t)
	defer db.Close()
	ctx := context.Background()

	for _, name := range []string{
		"llm_call_log_ts_desc",
		"llm_call_log_site_ts_desc",
		"llm_call_log_decision_ts_desc",
	} {
		var found string
		err := db.Pool.QueryRow(ctx, `SELECT indexname FROM pg_indexes WHERE tablename='llm_call_log' AND indexname=$1`, name).Scan(&found)
		require.NoError(t, err, "index %s missing", name)
		assert.Equal(t, name, found)
	}
}
