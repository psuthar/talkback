package utils

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExtractActionItemsFromContext_emptyChunksNoAPI(t *testing.T) {
	t.Parallel()
	out, err := ExtractActionItemsFromContext(context.Background(), nil, "Session", SessionContext{})
	require.NoError(t, err)
	require.NotNil(t, out)
	require.True(t, out.LowSignal)
	require.Empty(t, out.ActionItems)
}

func TestSanitizeActionItems_dedupAndTruncate(t *testing.T) {
	t.Parallel()
	owner := "x"
	out := &ActionItemsExtraction{
		ActionItems: []ActionItemRow{
			{Description: "  Do the thing  "},
			{Description: "do the thing", Owner: &owner},
			{Description: ""},
		},
		LowSignal: false,
	}
	sanitizeActionItems(out)
	require.Len(t, out.ActionItems, 1)
	require.Equal(t, "Do the thing", out.ActionItems[0].Description)
	require.False(t, out.LowSignal)
}
