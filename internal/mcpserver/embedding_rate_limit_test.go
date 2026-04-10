package mcpserver

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestMcpAcquireSessionEmbeddingQuota(t *testing.T) {
	globalSessionEmbeddingLimiter.mu.Lock()
	globalSessionEmbeddingLimiter.calls = make(map[uuid.UUID][]time.Time)
	globalSessionEmbeddingLimiter.mu.Unlock()

	t.Setenv(envMaxEmbeddingsPerSessionPerMin, "2")
	sid := uuid.MustParse("00000000-0000-4000-8000-0000000000a1")

	if err := mcpAcquireSessionEmbeddingQuota(sid); err != nil {
		t.Fatalf("first: %v", err)
	}
	if err := mcpAcquireSessionEmbeddingQuota(sid); err != nil {
		t.Fatalf("second: %v", err)
	}
	err := mcpAcquireSessionEmbeddingQuota(sid)
	if err == nil {
		t.Fatal("expected rate limit error")
	}
	var m map[string]any
	if e := json.Unmarshal([]byte(err.Error()), &m); e != nil {
		t.Fatal(e)
	}
	if m["http_status"] != float64(429) {
		t.Fatalf("http_status: %v", m["http_status"])
	}
	if m["error_code"] != "session_embedding_rate_limit" {
		t.Fatalf("error_code: %v", m["error_code"])
	}
}
