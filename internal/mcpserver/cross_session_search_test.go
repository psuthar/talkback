package mcpserver

import (
	"testing"
)

func TestCrossSessionChunkLoadCap_default(t *testing.T) {
	t.Setenv("TALKBACK_MCP_MAX_CROSS_SESSION_CHUNKS", "")
	if got := crossSessionChunkLoadCap(); got != DefaultMaxCrossSessionChunks {
		t.Fatalf("got %d want %d", got, DefaultMaxCrossSessionChunks)
	}
}

func TestCrossSessionChunkLoadCap_explicit(t *testing.T) {
	t.Setenv("TALKBACK_MCP_MAX_CROSS_SESSION_CHUNKS", "12000")
	if got := crossSessionChunkLoadCap(); got != 12000 {
		t.Fatalf("got %d want 12000", got)
	}
}

func TestCrossSessionChunkLoadCap_invalidFallsBackToDefault(t *testing.T) {
	t.Setenv("TALKBACK_MCP_MAX_CROSS_SESSION_CHUNKS", "not-a-number")
	if got := crossSessionChunkLoadCap(); got != DefaultMaxCrossSessionChunks {
		t.Fatalf("got %d want default", got)
	}
}

func TestCrossSessionChunkLoadCap_belowMinFallsBack(t *testing.T) {
	t.Setenv("TALKBACK_MCP_MAX_CROSS_SESSION_CHUNKS", "500")
	if got := crossSessionChunkLoadCap(); got != DefaultMaxCrossSessionChunks {
		t.Fatalf("got %d want default", got)
	}
}

func TestCrossSessionChunkLoadCap_clampsMax(t *testing.T) {
	t.Setenv("TALKBACK_MCP_MAX_CROSS_SESSION_CHUNKS", "999999999")
	if got := crossSessionChunkLoadCap(); got != maxMaxCrossSessionChunks {
		t.Fatalf("got %d want %d", got, maxMaxCrossSessionChunks)
	}
}
