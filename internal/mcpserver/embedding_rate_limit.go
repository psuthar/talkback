// Per-session rate limits for query embedding calls (SCRUM-54).
// Applies only to user-query embeddings in search / search_all_sessions / raw retrieval / ask — not to bulk index builds inside EnsureSessionIndex.
package mcpserver

import (
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

const (
	sessionEmbeddingRateWindow       = time.Minute
	defaultMaxEmbeddingsPerSessionPM = 0 // 0 = unlimited (backward compatible)
	envMaxEmbeddingsPerSessionPerMin = "TALKBACK_MCP_MAX_EMBEDDING_CALLS_PER_SESSION_PER_MINUTE"
)

// sessionEmbeddingLimiter tracks per-session query-embedding timestamps in a sliding window (in-process only).
type sessionEmbeddingLimiter struct {
	mu    sync.Mutex
	calls map[uuid.UUID][]time.Time
}

var globalSessionEmbeddingLimiter = &sessionEmbeddingLimiter{calls: make(map[uuid.UUID][]time.Time)}

func maxSessionEmbeddingsPerMinute() int {
	s := strings.TrimSpace(os.Getenv(envMaxEmbeddingsPerSessionPerMin))
	if s == "" {
		return defaultMaxEmbeddingsPerSessionPM
	}
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return defaultMaxEmbeddingsPerSessionPM
	}
	return n
}

// mcpAcquireSessionEmbeddingQuota records one query-embedding attempt for the session.
// When TALKBACK_MCP_MAX_EMBEDDING_CALLS_PER_SESSION_PER_MINUTE is >0 and the limit is exceeded, returns mcpToolErr 429.
func mcpAcquireSessionEmbeddingQuota(sessionID uuid.UUID) error {
	max := maxSessionEmbeddingsPerMinute()
	if max <= 0 {
		return nil
	}
	now := time.Now()
	cutoff := now.Add(-sessionEmbeddingRateWindow)

	l := globalSessionEmbeddingLimiter
	l.mu.Lock()
	defer l.mu.Unlock()

	ts := l.calls[sessionID]
	out := ts[:0]
	for _, t := range ts {
		if t.After(cutoff) {
			out = append(out, t)
		}
	}
	if len(out) >= max {
		log.Printf("mcp event=embedding_rate_limit session_id=%s limit_per_min=%d", sessionID.String(), max)
		return mcpToolErrCode("session_embedding_rate_limit", 429,
			"Too many embedding requests for this session in a short window; wait and retry, or raise TALKBACK_MCP_MAX_EMBEDDING_CALLS_PER_SESSION_PER_MINUTE.")
	}
	out = append(out, now)
	l.calls[sessionID] = out
	return nil
}
