package invitations

import (
	"testing"
)

func TestNormalizeEmail(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"  Foo@Example.COM  ", "foo@example.com"},
		{"user@test.org", "user@test.org"},
		{"", ""},
		{"  ", ""},
		{"A@B.C", "a@b.c"},
	}
	for _, tt := range tests {
		got := NormalizeEmail(tt.in)
		if got != tt.want {
			t.Errorf("NormalizeEmail(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestExpiryHours(t *testing.T) {
	// Default when zero
	s := &Service{Config: Config{ExpiryHours: 0}}
	if got := s.expiryHours(); got != DefaultExpiryHours {
		t.Errorf("expiryHours() with 0 = %d, want %d", got, DefaultExpiryHours)
	}
	// Custom
	s.Config.ExpiryHours = 24
	if got := s.expiryHours(); got != 24 {
		t.Errorf("expiryHours() with 24 = %d, want 24", got)
	}
}
