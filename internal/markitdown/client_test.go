package markitdown

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// helper: build a Client wired to a test server with a known secret.
func clientFor(t *testing.T, server *httptest.Server) *Client {
	t.Helper()
	return &Client{
		BaseURL:      server.URL,
		Secret:       "test-secret",
		HTTP:         server.Client(),
		Retries:      1,
		ImageTimeout: 5 * time.Second,
		URLTimeout:   5 * time.Second,
	}
}

// ─── Enabled() / disabled-client behavior ─────────────────────────────────

func TestClient_DisabledWhenEnvMissing(t *testing.T) {
	t.Setenv("MARKITDOWN_SIDECAR_URL", "")
	t.Setenv("MARKITDOWN_SIDECAR_SECRET", "")
	c := NewClient()
	if c.Enabled() {
		t.Fatalf("client should be disabled when env unset")
	}
	_, err := c.ExtractURL(context.Background(), "https://example.com")
	if !errors.Is(err, ErrSidecarUnavailable) {
		t.Fatalf("expected ErrSidecarUnavailable, got %v", err)
	}
}

func TestClient_EnabledWhenEnvSet(t *testing.T) {
	t.Setenv("MARKITDOWN_SIDECAR_URL", "https://sidecar.example")
	t.Setenv("MARKITDOWN_SIDECAR_SECRET", "secret")
	c := NewClient()
	if !c.Enabled() {
		t.Fatalf("client should be enabled when env set")
	}
}

// ─── ExtractImage happy path + auth header ────────────────────────────────

func TestExtractImage_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/extract/image" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-secret" {
			t.Errorf("missing/wrong bearer header: %q", r.Header.Get("Authorization"))
		}
		// Verify multipart parses with a "file" field carrying the bytes.
		mediaType, params, _ := mime_parse(r.Header.Get("Content-Type"))
		if mediaType != "multipart/form-data" {
			t.Errorf("expected multipart/form-data, got %q", mediaType)
		}
		mr := multipart.NewReader(r.Body, params["boundary"])
		part, err := mr.NextPart()
		if err != nil {
			t.Fatalf("read multipart part: %v", err)
		}
		if part.FormName() != "file" {
			t.Errorf("expected form field 'file', got %q", part.FormName())
		}
		if got := part.Header.Get("Content-Type"); got != "image/png" {
			t.Errorf("expected part content-type image/png, got %q", got)
		}
		body, _ := io.ReadAll(part)
		if string(body) != "fake-png-bytes" {
			t.Errorf("unexpected body bytes: %q", body)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"text":"# slide title","model":"gpt-4o-mini","tokens_used":42}`)
	}))
	defer srv.Close()

	c := clientFor(t, srv)
	got, err := c.ExtractImage(context.Background(), []byte("fake-png-bytes"), "image/png")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got.Text != "# slide title" || got.Model != "gpt-4o-mini" || got.TokensUsed != 42 {
		t.Fatalf("unexpected result: %+v", got)
	}
}

// ─── ExtractURL happy path ────────────────────────────────────────────────

func TestExtractURL_HappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/extract/url" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer test-secret" {
			t.Errorf("missing bearer header")
		}
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"url":"https://example.com/article"`) {
			t.Errorf("expected url in body, got %s", body)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"text":"# Article","title":"Example","fetched_url":"https://example.com/article","status_code":200}`)
	}))
	defer srv.Close()

	c := clientFor(t, srv)
	got, err := c.ExtractURL(context.Background(), "https://example.com/article")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got.Text != "# Article" || got.Title != "Example" || got.StatusCode != 200 {
		t.Fatalf("unexpected result: %+v", got)
	}
}

// ─── Retry on 5xx-then-200 ────────────────────────────────────────────────

func TestExtractImage_RetriesOn5xxThenSucceeds(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			http.Error(w, `{"error":"transient"}`, http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"text":"ok","model":"m","tokens_used":0}`)
	}))
	defer srv.Close()

	c := clientFor(t, srv) // Retries: 1 → 2 attempts total
	got, err := c.ExtractImage(context.Background(), []byte("x"), "image/png")
	if err != nil {
		t.Fatalf("expected success on retry, got %v", err)
	}
	if got.Text != "ok" {
		t.Errorf("unexpected result: %+v", got)
	}
	if calls != 2 {
		t.Errorf("expected 2 attempts, got %d", calls)
	}
}

// ─── No retry on 400 ──────────────────────────────────────────────────────

func TestExtractImage_NoRetryOn400(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnsupportedMediaType)
		fmt.Fprint(w, `{"detail":{"error":"unsupported_media_type","content_type":"text/plain"}}`)
	}))
	defer srv.Close()

	c := clientFor(t, srv)
	_, err := c.ExtractImage(context.Background(), []byte("x"), "text/plain")
	if !errors.Is(err, ErrSidecarBadRequest) {
		t.Fatalf("expected ErrSidecarBadRequest, got %v", err)
	}
	var se *SidecarError
	if !errors.As(err, &se) {
		t.Fatalf("expected *SidecarError, got %T", err)
	}
	if se.Code != "unsupported_media_type" {
		t.Errorf("expected stable code unsupported_media_type, got %q", se.Code)
	}
	if calls != 1 {
		t.Errorf("expected no retry on 4xx, got %d attempts", calls)
	}
}

// ─── 401 → ErrSidecarUnauthorized ─────────────────────────────────────────

func TestExtractImage_401Unauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"detail":{"error":"missing_bearer_token"}}`)
	}))
	defer srv.Close()

	c := clientFor(t, srv)
	_, err := c.ExtractImage(context.Background(), []byte("x"), "image/png")
	if !errors.Is(err, ErrSidecarUnauthorized) {
		t.Fatalf("expected ErrSidecarUnauthorized, got %v", err)
	}
}

// ─── 5xx after retry exhausted → ErrSidecarUnavailable ───────────────────

func TestExtractImage_5xxExhaustsRetries(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		http.Error(w, `{"error":"down"}`, http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	c := clientFor(t, srv) // Retries: 1 → 2 attempts
	_, err := c.ExtractImage(context.Background(), []byte("x"), "image/png")
	if !errors.Is(err, ErrSidecarUnavailable) {
		t.Fatalf("expected ErrSidecarUnavailable, got %v", err)
	}
	if calls != 2 {
		t.Errorf("expected 2 attempts then surface error, got %d", calls)
	}
}

// ─── URL endpoint upstream 4xx pass-through ──────────────────────────────

func TestExtractURL_UpstreamHTTPErrorClassified(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"error":"upstream_http_error","status_code":404,"fetched_url":"https://example.com/missing","message":"upstream returned 404"}`)
	}))
	defer srv.Close()

	c := clientFor(t, srv)
	_, err := c.ExtractURL(context.Background(), "https://example.com/missing")
	if !errors.Is(err, ErrSidecarUpstreamHTTP) {
		t.Fatalf("expected ErrSidecarUpstreamHTTP for upstream 4xx, got %v", err)
	}
	var se *SidecarError
	if !errors.As(err, &se) || se.Code != "upstream_http_error" || se.FetchedURL != "https://example.com/missing" {
		t.Errorf("unexpected SidecarError: %+v", se)
	}
}

// ─── Empty URL → bad request without hitting the server ──────────────────

func TestExtractURL_EmptyURLReturnsBadRequest(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
	}))
	defer srv.Close()
	c := clientFor(t, srv)
	_, err := c.ExtractURL(context.Background(), "")
	if !errors.Is(err, ErrSidecarBadRequest) {
		t.Fatalf("expected ErrSidecarBadRequest for empty url, got %v", err)
	}
	if calls != 0 {
		t.Errorf("client should short-circuit before HTTP call, got %d", calls)
	}
}

// ─── Network error after retry → ErrSidecarUnavailable ───────────────────

func TestExtractImage_NetworkErrorReturnsUnavailable(t *testing.T) {
	c := &Client{
		BaseURL:      "http://127.0.0.1:1", // closed port
		Secret:       "test-secret",
		HTTP:         &http.Client{Timeout: 200 * time.Millisecond},
		Retries:      0,
		ImageTimeout: 200 * time.Millisecond,
	}
	_, err := c.ExtractImage(context.Background(), []byte("x"), "image/png")
	if !errors.Is(err, ErrSidecarUnavailable) {
		t.Fatalf("expected ErrSidecarUnavailable, got %v", err)
	}
}

// ─── parseStableError helper coverage (both response shapes) ─────────────

func TestParseStableError_FlatShape(t *testing.T) {
	code, msg, fu := parseStableError([]byte(`{"error":"timeout","message":"upstream timeout","fetched_url":"x"}`))
	if code != "timeout" || msg != "upstream timeout" || fu != "x" {
		t.Errorf("unexpected: code=%q msg=%q fu=%q", code, msg, fu)
	}
}

func TestParseStableError_NestedDetailShape(t *testing.T) {
	code, msg, _ := parseStableError([]byte(`{"detail":{"error":"missing_bearer_token","message":"nope"}}`))
	if code != "missing_bearer_token" || msg != "nope" {
		t.Errorf("unexpected: code=%q msg=%q", code, msg)
	}
}

func TestParseStableError_EmptyBody(t *testing.T) {
	code, _, _ := parseStableError(nil)
	if code != "" {
		t.Errorf("expected empty, got %q", code)
	}
}

// ─── env var helpers ─────────────────────────────────────────────────────

func TestReadIntEnv_Defaults(t *testing.T) {
	t.Setenv("MD_TEST_INT", "")
	if got := readIntEnv("MD_TEST_INT", 7); got != 7 {
		t.Errorf("expected fallback 7, got %d", got)
	}
	t.Setenv("MD_TEST_INT", "garbage")
	if got := readIntEnv("MD_TEST_INT", 7); got != 7 {
		t.Errorf("expected fallback on parse error, got %d", got)
	}
	t.Setenv("MD_TEST_INT", "3")
	if got := readIntEnv("MD_TEST_INT", 7); got != 3 {
		t.Errorf("expected parsed 3, got %d", got)
	}
}

func TestReadDurationEnv_Defaults(t *testing.T) {
	t.Setenv("MD_TEST_DUR", "")
	if got := readDurationEnv("MD_TEST_DUR", 5*time.Second); got != 5*time.Second {
		t.Errorf("expected fallback, got %v", got)
	}
	t.Setenv("MD_TEST_DUR", "2.5")
	if got := readDurationEnv("MD_TEST_DUR", 5*time.Second); got != 2500*time.Millisecond {
		t.Errorf("expected 2.5s, got %v", got)
	}
}

// mime_parse wraps mime.ParseMediaType to keep test imports tidy.
func mime_parse(s string) (string, map[string]string, error) {
	if i := strings.Index(s, ";"); i >= 0 {
		base := strings.TrimSpace(s[:i])
		params := map[string]string{}
		for _, kv := range strings.Split(s[i+1:], ";") {
			kv = strings.TrimSpace(kv)
			eq := strings.Index(kv, "=")
			if eq <= 0 {
				continue
			}
			k := strings.TrimSpace(kv[:eq])
			v := strings.Trim(strings.TrimSpace(kv[eq+1:]), `"`)
			params[k] = v
		}
		return base, params, nil
	}
	return strings.TrimSpace(s), map[string]string{}, nil
}
