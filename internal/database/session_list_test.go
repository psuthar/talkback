package database

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/psuthar/talkback/internal/models"
	"github.com/stretchr/testify/require"
)

func TestApplySessionListPagination(t *testing.T) {
	t.Parallel()
	mk := func(n int) []SessionListRow {
		var rows []SessionListRow
		for i := 0; i < n; i++ {
			rows = append(rows, SessionListRow{
				Session: &models.Session{ID: uuid.New(), Title: "s", UpdatedAt: time.Unix(int64(i), 0)},
				MyRole:  "creator",
			})
		}
		return rows
	}
	t.Run("no limit", func(t *testing.T) {
		t.Parallel()
		in := mk(3)
		got := ApplySessionListPagination(in, 0, 0)
		require.Len(t, got, 3)
	})
	t.Run("limit slice", func(t *testing.T) {
		t.Parallel()
		in := mk(5)
		got := ApplySessionListPagination(in, 2, 1)
		require.Len(t, got, 2)
	})
	t.Run("offset past end", func(t *testing.T) {
		t.Parallel()
		in := mk(2)
		got := ApplySessionListPagination(in, 5, 10)
		require.Empty(t, got)
	})
}
