package processing

import (
	"testing"
	"time"
)

func TestBackoffTransient(t *testing.T) {
	tests := []struct {
		attempt  int
		expected time.Duration
	}{
		{0, 1 * time.Minute},
		{1, 1 * time.Minute},
		{2, 3 * time.Minute},
		{3, 10 * time.Minute},
		{4, 30 * time.Minute},
		{5, 2 * time.Hour},
		{10, 2 * time.Hour},
	}
	for _, tt := range tests {
		got := BackoffTransient(tt.attempt)
		if got != tt.expected {
			t.Errorf("BackoffTransient(%d) = %v, want %v", tt.attempt, got, tt.expected)
		}
	}
}

func TestBackoffWaiting(t *testing.T) {
	tests := []struct {
		attempt  int
		expected time.Duration
	}{
		{0, 10 * time.Minute},
		{1, 10 * time.Minute},
		{2, 30 * time.Minute},
		{3, 2 * time.Hour},
		{5, 2 * time.Hour},
	}
	for _, tt := range tests {
		got := BackoffWaiting(tt.attempt)
		if got != tt.expected {
			t.Errorf("BackoffWaiting(%d) = %v, want %v", tt.attempt, got, tt.expected)
		}
	}
}
