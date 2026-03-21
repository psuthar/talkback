package browserurl

import (
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestParticipantSessionRedirectPath(t *testing.T) {
	id := uuid.MustParse("550e8400-e29b-41d4-a716-446655440000")
	got := ParticipantSessionRedirectPath(id)
	if !strings.HasPrefix(got, CanonicalSessionPrefix+"/") {
		t.Fatalf("expected prefix %q, got %q", CanonicalSessionPrefix+"/", got)
	}
	if !strings.Contains(got, "mode=view") {
		t.Fatalf("expected mode=view in %q", got)
	}
	abs := ParticipantSessionAbsoluteURL("https://app.example", id)
	if !strings.HasPrefix(abs, "https://app.example/app/sessions/") {
		t.Fatalf("absolute URL: %q", abs)
	}
}
