package handlers

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestZoomScopesDefault_CoversRequiredScopes is the SCRUM-407 audit regression:
// the default Zoom OAuth scope string must contain every entry in
// ZoomRequiredScopes (the closed set the SCRUM-401 multi-recording flow needs).
// If a future PR drops a scope from the default set without updating
// ZoomRequiredScopes, this test will catch it.
func TestZoomScopesDefault_CoversRequiredScopes(t *testing.T) {
	t.Parallel()
	defaults := strings.Fields(zoomScopesDefault)
	have := map[string]bool{}
	for _, s := range defaults {
		have[s] = true
	}
	for _, want := range ZoomRequiredScopes {
		assert.True(t, have[want],
			"zoomScopesDefault must contain %q — silent removal would break SCRUM-401 multi-recording attach for connected users", want)
	}
}
