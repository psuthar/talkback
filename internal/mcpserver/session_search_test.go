package mcpserver

import (
	"testing"
)

func TestTruncateSearchSnippet(t *testing.T) {
	t.Parallel()
	short := "hello"
	if got := truncateSearchSnippet(short); got != short {
		t.Fatalf("got %q", got)
	}
	long := string(make([]rune, maxSearchSnippetRunes+10))
	got := truncateSearchSnippet(long)
	if len([]rune(got)) != maxSearchSnippetRunes+len([]rune("…")) {
		t.Fatalf("len %d want %d", len([]rune(got)), maxSearchSnippetRunes+len([]rune("…")))
	}
}

func TestAnchorMs(t *testing.T) {
	t.Parallel()
	m := map[string]interface{}{"start_ms": float64(1200), "end_ms": float64(3400)}
	s := anchorMs(m, "start_ms")
	e := anchorMs(m, "end_ms")
	if s == nil || *s != 1200 || e == nil || *e != 3400 {
		t.Fatalf("start=%v end=%v", s, e)
	}
	if anchorMs(nil, "x") != nil {
		t.Fatal()
	}
}
