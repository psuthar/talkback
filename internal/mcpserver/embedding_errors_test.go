package mcpserver

import (
	"errors"
	"testing"
)

func TestMcpClassifyEmbeddingError(t *testing.T) {
	cases := []struct {
		err      error
		wantCode string
		wantHTTP int
	}{
		{errors.New("OPENAI_API_KEY environment variable is not set"), "openai_not_configured", 503},
		{errors.New("status 429: rate limit"), "openai_rate_limited", 429},
		{errors.New("insufficient_quota"), "openai_quota_exceeded", 429},
		{errors.New("something weird"), "embedding_failed", 503},
	}
	for _, tc := range cases {
		code, st, _ := mcpClassifyEmbeddingError(tc.err)
		if code != tc.wantCode || st != tc.wantHTTP {
			t.Fatalf("err=%v: got code=%s http=%d want %s %d", tc.err, code, st, tc.wantCode, tc.wantHTTP)
		}
	}
}

func TestMcpClassifyIndexError(t *testing.T) {
	code, msg := mcpClassifyIndexError(errors.New("context deadline exceeded"))
	if code != "index_timeout" || msg == "" {
		t.Fatalf("timeout: code=%q msg=%q", code, msg)
	}
	code2, _ := mcpClassifyIndexError(errors.New("s3 access denied"))
	if code2 != "index_unavailable" {
		t.Fatalf("got %s", code2)
	}
}
