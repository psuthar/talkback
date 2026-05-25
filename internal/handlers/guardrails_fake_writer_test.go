package handlers

import (
	"context"
	"sync"

	"github.com/psuthar/talkback/internal/guardrails"
)

// handlerFakeGuardrailsWriter is a synchronous in-memory implementation
// of guardrails.Writer used by SCRUM-564 handler integration tests. The
// guardrails package has its own internal fakeWriter, but it's
// unexported — the handlers package needs its own to exercise the
// Init / LogLLMCall / FlushNow path through real handler code.
type handlerFakeGuardrailsWriter struct {
	mu   sync.Mutex
	rows []guardrails.LLMCallRow
}

func (f *handlerFakeGuardrailsWriter) InsertLLMCallRow(_ context.Context, row guardrails.LLMCallRow) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rows = append(f.rows, row)
	return nil
}

func (f *handlerFakeGuardrailsWriter) Rows() []guardrails.LLMCallRow {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]guardrails.LLMCallRow, len(f.rows))
	copy(out, f.rows)
	return out
}
