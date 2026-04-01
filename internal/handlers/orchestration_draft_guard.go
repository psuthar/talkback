package handlers

import (
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"
)

// orchestrationDraftGuard enforces per-(user,session) rate limits and per-session concurrency caps
// for POST .../orchestration/draft-answers (SCRUM-20).
type orchestrationDraftGuard struct {
	mu sync.Mutex

	maxPerWindow int
	window       time.Duration

	// key: userID.String() + "|" + sessionID.String() — timestamps of allowed requests in window
	rate map[string][]time.Time

	// concurrent draft generations per session
	concurrent map[uuid.UUID]int
	maxConc    int
}

func newOrchestrationDraftGuardFromEnv() *orchestrationDraftGuard {
	maxPer := 15
	if v := os.Getenv("ORCHESTRATION_DRAFT_MAX_PER_MIN"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			maxPer = n
		}
	}
	maxConc := 2
	if v := os.Getenv("ORCHESTRATION_DRAFT_MAX_CONCURRENT_PER_SESSION"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			maxConc = n
		}
	}
	return &orchestrationDraftGuard{
		maxPerWindow: maxPer,
		window:       time.Minute,
		rate:         make(map[string][]time.Time),
		concurrent:   make(map[uuid.UUID]int),
		maxConc:      maxConc,
	}
}

func rateLimitKey(userID, sessionID uuid.UUID) string {
	return userID.String() + "|" + sessionID.String()
}

// allowRate returns true if the request is under the sliding-window limit for this user+session.
func (g *orchestrationDraftGuard) allowRate(userID, sessionID uuid.UUID) bool {
	if g == nil {
		return true
	}
	key := rateLimitKey(userID, sessionID)
	now := time.Now()
	cutoff := now.Add(-g.window)

	g.mu.Lock()
	defer g.mu.Unlock()

	ts := g.rate[key]
	pruned := ts[:0]
	for _, t := range ts {
		if t.After(cutoff) {
			pruned = append(pruned, t)
		}
	}
	if len(pruned) >= g.maxPerWindow {
		g.rate[key] = pruned
		return false
	}
	pruned = append(pruned, now)
	g.rate[key] = pruned
	return true
}

// acquireConcurrency returns a release function, or false if the session is at capacity.
func (g *orchestrationDraftGuard) acquireConcurrency(sessionID uuid.UUID) (release func(), ok bool) {
	if g == nil {
		return func() {}, true
	}
	g.mu.Lock()
	if g.concurrent[sessionID] >= g.maxConc {
		g.mu.Unlock()
		return nil, false
	}
	g.concurrent[sessionID]++
	g.mu.Unlock()

	return func() {
		g.mu.Lock()
		defer g.mu.Unlock()
		g.concurrent[sessionID]--
		if g.concurrent[sessionID] <= 0 {
			delete(g.concurrent, sessionID)
		}
	}, true
}
