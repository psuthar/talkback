package guardrails

import (
	"context"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeWriter is a synchronous in-memory Writer for tests.
type fakeWriter struct {
	mu   sync.Mutex
	rows []LLMCallRow
	fail bool
}

func (f *fakeWriter) InsertLLMCallRow(_ context.Context, row LLMCallRow) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail {
		return assertErr
	}
	f.rows = append(f.rows, row)
	return nil
}

func (f *fakeWriter) Rows() []LLMCallRow {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]LLMCallRow, len(f.rows))
	copy(out, f.rows)
	return out
}

var assertErr = errBoom{}

type errBoom struct{}

func (errBoom) Error() string { return "boom (test)" }


func TestHashPrompt_SiteSaltMakesOriginalAndRetryDistinct(t *testing.T) {
	prompt := "What did Alex decide?"
	original := HashPrompt("qa_ask", prompt)
	retryCit := HashPrompt("qa_ask_retry_citation", prompt)
	retryGr := HashPrompt("qa_ask_retry_grounding", prompt)
	assert.NotEqual(t, original, retryCit, "qa_ask and qa_ask_retry_citation must produce distinct hashes for the same prompt — SCRUM-569 Gap 5")
	assert.NotEqual(t, original, retryGr)
	assert.NotEqual(t, retryCit, retryGr)
}

func TestHashPrompt_SameSiteSamePromptCollidesIntentionally(t *testing.T) {
	a := HashPrompt("qa_ask", "same question")
	b := HashPrompt("qa_ask", "same question")
	assert.Equal(t, a, b, "same site + same prompt must de-dup; that's the analytical point of the hash")
}

func TestLogLLMCall_FillsDefaultsAndPersistsViaFlushNow(t *testing.T) {
	ResetForTest()
	fake := &fakeWriter{}
	Init(fake)
	t.Cleanup(func() { Init(nil); ResetForTest() })

	row := LLMCallRow{
		Site:       "qa_ask",
		Model:      "gpt-4o-mini",
		PromptHash: HashPrompt("qa_ask", "hello"),
		LatencyMS:  42,
		Decision:   "allowed",
	}
	LogLLMCall(context.Background(), row)
	FlushNow(context.Background())

	got := fake.Rows()
	require.Len(t, got, 1)
	assert.NotEqual(t, uuid.UUID{}, got[0].ID, "ID is generated when zero")
	assert.False(t, got[0].TS.IsZero(), "TS is stamped when zero")
	assert.NotNil(t, got[0].GuardrailsFired, "GuardrailsFired is initialized to non-nil")
	assert.Equal(t, []string{}, got[0].GuardrailsFired)
}

func TestLogLLMCall_PicksUpUserAndSessionFromContext(t *testing.T) {
	ResetForTest()
	fake := &fakeWriter{}
	Init(fake)
	t.Cleanup(func() { Init(nil); ResetForTest() })

	uid := uuid.New()
	sid := uuid.New()
	ctx := WithSessionID(WithUserID(context.Background(), uid), sid)

	LogLLMCall(ctx, LLMCallRow{Site: "qa_ask", Model: "m", LatencyMS: 1, Decision: "allowed"})
	FlushNow(context.Background())

	got := fake.Rows()
	require.Len(t, got, 1)
	require.NotNil(t, got[0].UserID)
	require.NotNil(t, got[0].SessionID)
	assert.Equal(t, uid, *got[0].UserID)
	assert.Equal(t, sid, *got[0].SessionID)
}

func TestLogLLMCall_ExplicitRowFieldsBeatContext(t *testing.T) {
	ResetForTest()
	fake := &fakeWriter{}
	Init(fake)
	t.Cleanup(func() { Init(nil); ResetForTest() })

	ctxUID := uuid.New()
	ctx := WithUserID(context.Background(), ctxUID)
	explicit := uuid.New()

	LogLLMCall(ctx, LLMCallRow{
		Site: "qa_ask", Model: "m", LatencyMS: 1, Decision: "allowed",
		UserID: &explicit,
	})
	FlushNow(context.Background())

	got := fake.Rows()
	require.Len(t, got, 1)
	require.NotNil(t, got[0].UserID)
	assert.Equal(t, explicit, *got[0].UserID)
}

func TestLogLLMCall_DropsOldestOnOverflow(t *testing.T) {
	ResetForTest()
	// Don't Init a writer — keep the rows in the buffer so we can observe.
	t.Cleanup(func() { ResetForTest() })

	for i := 0; i < bufferCap+5; i++ {
		LogLLMCall(context.Background(), LLMCallRow{
			Site: "qa_ask", Model: "m", LatencyMS: i, Decision: "allowed",
		})
	}
	assert.Equal(t, uint64(5), DroppedCount(), "first 5 should be dropped to keep buffer at cap")

	bufMu.Lock()
	depth := len(buffer)
	first := buffer[0].LatencyMS
	last := buffer[len(buffer)-1].LatencyMS
	bufMu.Unlock()

	assert.Equal(t, bufferCap, depth, "buffer is exactly at cap after overflow")
	assert.Equal(t, 5, first, "oldest 5 dropped; row 5 is the new head")
	assert.Equal(t, bufferCap+4, last, "newest survives")
}

func TestFlushNow_NoOpWhenNoWriter(t *testing.T) {
	ResetForTest()
	Init(nil)
	t.Cleanup(func() { ResetForTest() })

	LogLLMCall(context.Background(), LLMCallRow{Site: "qa_ask", Model: "m", LatencyMS: 1, Decision: "allowed"})
	FlushNow(context.Background()) // should not panic, should not block
	// Buffer is unchanged because there was no writer to drain to.
	bufMu.Lock()
	depth := len(buffer)
	bufMu.Unlock()
	assert.Equal(t, 1, depth)
}

func TestLogLLMCall_WriterErrorDoesNotRebuffer(t *testing.T) {
	ResetForTest()
	fake := &fakeWriter{fail: true}
	Init(fake)
	t.Cleanup(func() { Init(nil); ResetForTest() })

	LogLLMCall(context.Background(), LLMCallRow{Site: "qa_ask", Model: "m", LatencyMS: 1, Decision: "allowed"})
	FlushNow(context.Background())

	// Writer failed; buffer should still be drained (we don't rebuffer on
	// failure — accept data loss over backpressure).
	bufMu.Lock()
	depth := len(buffer)
	bufMu.Unlock()
	assert.Equal(t, 0, depth)
}
