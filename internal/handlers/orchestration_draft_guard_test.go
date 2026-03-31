package handlers

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOrchestrationDraftGuard_allowRate(t *testing.T) {
	t.Parallel()
	g := &orchestrationDraftGuard{
		maxPerWindow: 2,
		window:       time.Minute,
		rate:         make(map[string][]time.Time),
		concurrent:   make(map[uuid.UUID]int),
		maxConc:      2,
	}
	u, s := uuid.New(), uuid.New()
	assert.True(t, g.allowRate(u, s))
	assert.True(t, g.allowRate(u, s))
	assert.False(t, g.allowRate(u, s))
	assert.True(t, g.allowRate(uuid.New(), s), "different user should have own bucket")
}

func TestOrchestrationDraftGuard_acquireConcurrency(t *testing.T) {
	t.Parallel()
	g := &orchestrationDraftGuard{
		maxPerWindow: 100,
		window:       time.Minute,
		rate:         make(map[string][]time.Time),
		concurrent:   make(map[uuid.UUID]int),
		maxConc:      1,
	}
	sid := uuid.New()
	rel1, ok1 := g.acquireConcurrency(sid)
	require.True(t, ok1)
	require.NotNil(t, rel1)
	_, ok2 := g.acquireConcurrency(sid)
	assert.False(t, ok2)
	rel1()
	rel3, ok3 := g.acquireConcurrency(sid)
	require.True(t, ok3)
	rel3()
}
