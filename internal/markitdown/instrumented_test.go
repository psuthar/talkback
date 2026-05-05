package markitdown

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// captureLog redirects log output to a buffer for the duration of fn so
// tests can assert on the structured log lines emitted by the
// instrumented helpers.
func captureLog(t *testing.T, fn func()) string {
	t.Helper()
	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(prev)
	fn()
	return buf.String()
}

func clientForInstrumented(srv *httptest.Server) *Client {
	return &Client{
		BaseURL:      srv.URL,
		Secret:       "test-secret",
		HTTP:         srv.Client(),
		Retries:      0,
		ImageTimeout: 5 * time.Second,
		URLTimeout:   5 * time.Second,
	}
}

// SCRUM-306 — happy path emits "outcome=success" with duration; tokens
// included when present in the result body.
func TestInstrumentedExtractImage_LogsSuccessWithTokens(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"text":"caption","model":"gpt-4o-mini","tokens_used":150}`)
	}))
	defer srv.Close()
	c := clientForInstrumented(srv)

	out := captureLog(t, func() {
		res, err := InstrumentedExtractImage(context.Background(), c, []byte("x"), "image/png")
		if err != nil {
			t.Fatalf("expected success, got %v", err)
		}
		if res.TokensUsed != 150 {
			t.Errorf("tokens not propagated: %d", res.TokensUsed)
		}
	})
	if !strings.Contains(out, "markitdown.image: outcome=success") {
		t.Errorf("expected success log line, got %q", out)
	}
	if !strings.Contains(out, "tokens=150") {
		t.Errorf("expected tokens=150 in log, got %q", out)
	}
	if !strings.Contains(out, "duration_ms=") {
		t.Errorf("expected duration_ms in log, got %q", out)
	}
}

// SCRUM-306 — 4xx error emits outcome=bad_request with the stable code
// preserved. SidecarError fields are unwrapped so ops can see what
// actually went wrong without parsing prose.
func TestInstrumentedExtractImage_LogsBadRequestWithCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnsupportedMediaType)
		fmt.Fprint(w, `{"detail":{"error":"unsupported_media_type"}}`)
	}))
	defer srv.Close()
	c := clientForInstrumented(srv)

	out := captureLog(t, func() {
		_, err := InstrumentedExtractImage(context.Background(), c, []byte("x"), "text/plain")
		if !errors.Is(err, ErrSidecarBadRequest) {
			t.Errorf("expected ErrSidecarBadRequest, got %v", err)
		}
	})
	if !strings.Contains(out, "outcome=bad_request") {
		t.Errorf("expected bad_request outcome tag, got %q", out)
	}
	if !strings.Contains(out, "code=unsupported_media_type") {
		t.Errorf("expected stable code in log, got %q", out)
	}
}

// SCRUM-306 — 5xx-after-retry-exhaustion emits outcome=unavailable so
// ops can grep for sidecar-degradation periods.
func TestInstrumentedExtractImage_LogsUnavailableOn5xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"down"}`, http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	c := clientForInstrumented(srv)

	out := captureLog(t, func() {
		_, err := InstrumentedExtractImage(context.Background(), c, []byte("x"), "image/png")
		if !errors.Is(err, ErrSidecarUnavailable) {
			t.Errorf("expected ErrSidecarUnavailable, got %v", err)
		}
	})
	if !strings.Contains(out, "outcome=unavailable") {
		t.Errorf("expected unavailable outcome tag, got %q", out)
	}
}

// SCRUM-306 — 401 emits outcome=unauthorized. Distinct tag so ops can
// alert on shared-secret drift specifically.
func TestInstrumentedExtractImage_LogsUnauthorizedOn401(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"detail":{"error":"missing_bearer_token"}}`)
	}))
	defer srv.Close()
	c := clientForInstrumented(srv)

	out := captureLog(t, func() {
		_, err := InstrumentedExtractImage(context.Background(), c, []byte("x"), "image/png")
		if !errors.Is(err, ErrSidecarUnauthorized) {
			t.Errorf("expected ErrSidecarUnauthorized, got %v", err)
		}
	})
	if !strings.Contains(out, "outcome=unauthorized") {
		t.Errorf("expected unauthorized outcome tag, got %q", out)
	}
}

// SCRUM-306 — URL endpoint happy path uses the markitdown.url op label.
func TestInstrumentedExtractURL_LogsSuccessWithUrlLabel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"text":"# t","title":"Title","fetched_url":"https://example.com","status_code":200}`)
	}))
	defer srv.Close()
	c := clientForInstrumented(srv)

	out := captureLog(t, func() {
		_, err := InstrumentedExtractURL(context.Background(), c, "https://example.com")
		if err != nil {
			t.Fatalf("expected success, got %v", err)
		}
	})
	if !strings.Contains(out, "markitdown.url: outcome=success") {
		t.Errorf("expected url success log, got %q", out)
	}
}

// SCRUM-306 — URL upstream 4xx pass-through emits outcome=upstream_http
// distinct from sidecar bad_request, so ops can separate "the user's URL
// returned 4xx" from "the sidecar rejected the request".
func TestInstrumentedExtractURL_LogsUpstreamHTTPOnUpstream4xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"error":"upstream_http_error","status_code":404,"fetched_url":"https://example.com/missing","message":"upstream returned 404"}`)
	}))
	defer srv.Close()
	c := clientForInstrumented(srv)

	out := captureLog(t, func() {
		_, err := InstrumentedExtractURL(context.Background(), c, "https://example.com/missing")
		if !errors.Is(err, ErrSidecarUpstreamHTTP) {
			t.Errorf("expected ErrSidecarUpstreamHTTP, got %v", err)
		}
	})
	if !strings.Contains(out, "outcome=upstream_http") {
		t.Errorf("expected upstream_http outcome tag, got %q", out)
	}
}

// SCRUM-306 — calling Instrumented* with a nil client mirrors the
// underlying disabled-client behavior (returns ErrSidecarUnavailable
// without crashing). No log line is required for the nil case.
func TestInstrumented_NilClientReturnsUnavailable(t *testing.T) {
	_, err := InstrumentedExtractImage(context.Background(), nil, []byte("x"), "image/png")
	if !errors.Is(err, ErrSidecarUnavailable) {
		t.Errorf("expected ErrSidecarUnavailable from nil client, got %v", err)
	}
	_, err = InstrumentedExtractURL(context.Background(), nil, "https://example.com")
	if !errors.Is(err, ErrSidecarUnavailable) {
		t.Errorf("expected ErrSidecarUnavailable from nil client, got %v", err)
	}
}
