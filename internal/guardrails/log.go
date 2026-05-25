package guardrails

import (
	"context"
	"log"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

// Writer persists LLMCallRows. The DB-backed implementation lives in
// internal/database; tests inject a fake.
type Writer interface {
	InsertLLMCallRow(ctx context.Context, row LLMCallRow) error
}

// bufferCap is the max in-memory backlog before drop-oldest kicks in.
// Sized so a flush failure of one cycle (5s) at peak QA volume isn't
// fully lost. On overflow we drop the oldest row and bump dropCount —
// better to lose telemetry than to backpressure the LLM critical path.
const bufferCap = 1024

// flushInterval is the steady-state background flush cadence.
const flushInterval = 5 * time.Second

var (
	writerMu sync.RWMutex
	writer   Writer

	bufMu     sync.Mutex
	buffer    []LLMCallRow
	dropCount atomic.Uint64

	initOnce    sync.Once
	flusherStop context.CancelFunc
)

// Init registers the writer and starts the background flusher. Idempotent
// — the flusher only starts on the first call (subsequent calls just
// replace the writer). main() calls this once at startup; tests call
// Init(fake) per-test and use FlushNow for synchronous drain.
func Init(w Writer) {
	writerMu.Lock()
	writer = w
	writerMu.Unlock()

	initOnce.Do(func() {
		var ctx context.Context
		ctx, flusherStop = context.WithCancel(context.Background())
		go runFlusher(ctx)
	})
}

// LogLLMCall appends a row to the bounded buffer. Never blocks the LLM
// critical path; on overflow drops the oldest row and increments
// DroppedCount. The async flusher then writes batched rows to the
// registered Writer.
//
// Honors GUARDRAIL_LOG_TO_DB. Default `on`. When `off`, falls back to
// one log line per call and skips the buffer (escape hatch — not the
// steady state).
//
// If UserID/SessionID on the row are nil, falls back to values stamped
// on the context via WithUserID / WithSessionID. This lets per-call
// sites stay terse — handlers stamp ctx once, sites just log.
func LogLLMCall(ctx context.Context, row LLMCallRow) {
	if row.ID == (uuid.UUID{}) {
		row.ID = uuid.New()
	}
	if row.TS.IsZero() {
		row.TS = time.Now().UTC()
	}
	if row.GuardrailsFired == nil {
		row.GuardrailsFired = []string{}
	}
	if row.UserID == nil {
		row.UserID = userIDFromCtx(ctx)
	}
	if row.SessionID == nil {
		row.SessionID = sessionIDFromCtx(ctx)
	}

	if strings.EqualFold(os.Getenv("GUARDRAIL_LOG_TO_DB"), "off") {
		log.Printf(
			"event=LLM_CALL site=%s model=%s decision=%s latency_ms=%d guardrails_fired=%v refusal_code=%v",
			row.Site, row.Model, row.Decision, row.LatencyMS, row.GuardrailsFired, derefStr(row.RefusalCode),
		)
		return
	}

	bufMu.Lock()
	if len(buffer) >= bufferCap {
		buffer = buffer[1:]
		dropCount.Add(1)
	}
	buffer = append(buffer, row)
	bufMu.Unlock()
}

// FlushNow drains the buffer to the registered Writer synchronously.
// Used by tests + Stop. Safe to call when no Writer is registered (no-op).
func FlushNow(ctx context.Context) {
	drain(ctx)
}

// Stop halts the background flusher and drains the buffer one last time.
// Called from main's graceful shutdown path. No-op if Init never ran.
func Stop(ctx context.Context) {
	if flusherStop != nil {
		flusherStop()
	}
	drain(ctx)
}

// DroppedCount returns the cumulative drop-oldest count. Surfaced by the
// admin /api/admin/llm-stats endpoint so operators can see when telemetry
// is being shed.
func DroppedCount() uint64 { return dropCount.Load() }

// ResetForTest clears the buffer and drop counter. Test-only convenience.
// Production code should never call this.
func ResetForTest() {
	bufMu.Lock()
	buffer = nil
	bufMu.Unlock()
	dropCount.Store(0)
}

func runFlusher(ctx context.Context) {
	t := time.NewTicker(flushInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			drain(ctx)
		}
	}
}

func drain(ctx context.Context) {
	writerMu.RLock()
	w := writer
	writerMu.RUnlock()
	if w == nil {
		return
	}

	bufMu.Lock()
	if len(buffer) == 0 {
		bufMu.Unlock()
		return
	}
	rows := buffer
	buffer = nil
	bufMu.Unlock()

	for _, row := range rows {
		if err := w.InsertLLMCallRow(ctx, row); err != nil {
			// Don't re-buffer on failure — accepting data loss over backpressure.
			log.Printf("guardrails.LogLLMCall: insert failed for site=%s id=%s: %v", row.Site, row.ID, err)
		}
	}
}

// --- ctx helpers ---

type ctxKey int

const (
	ctxKeyUserID ctxKey = iota + 1
	ctxKeySessionID
)

// WithUserID stamps the acting user on ctx so downstream LogLLMCall calls
// can pick it up without threading it through every signature.
func WithUserID(ctx context.Context, uid uuid.UUID) context.Context {
	if uid == (uuid.UUID{}) {
		return ctx
	}
	return context.WithValue(ctx, ctxKeyUserID, uid)
}

// WithSessionID stamps the session on ctx so downstream LogLLMCall calls
// can pick it up.
func WithSessionID(ctx context.Context, sid uuid.UUID) context.Context {
	if sid == (uuid.UUID{}) {
		return ctx
	}
	return context.WithValue(ctx, ctxKeySessionID, sid)
}

func userIDFromCtx(ctx context.Context) *uuid.UUID {
	v, ok := ctx.Value(ctxKeyUserID).(uuid.UUID)
	if !ok || v == (uuid.UUID{}) {
		return nil
	}
	return &v
}

func sessionIDFromCtx(ctx context.Context) *uuid.UUID {
	v, ok := ctx.Value(ctxKeySessionID).(uuid.UUID)
	if !ok || v == (uuid.UUID{}) {
		return nil
	}
	return &v
}

// IntPtr is a small helper for sites to wrap response.Usage.* values.
func IntPtr(n int) *int { return &n }

// StrPtr is a small helper for sites to wrap a refusal code / message.
func StrPtr(s string) *string { return &s }

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
