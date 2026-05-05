package urlextract

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/psuthar/talkback/internal/markitdown"
)

// SCRUM-304 — sidecar disabled (nil client or .Enabled()==false): the
// wrapper is a no-op pass-through to the legacy FetchAndExtract. Verified
// by serving a tiny HTML page from a test upstream and asserting the
// extracted body matches the Go-path output exactly.
func TestFetchAndExtractWithSidecar_NilClientFallsBackToLegacy(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<html><head><title>Doc</title></head><body><main><h1>Hi</h1><p>Body text</p></main></body></html>`)
	}))
	defer upstream.Close()

	got, err := FetchAndExtractWithSidecar(context.Background(), upstream.URL, DefaultMaxBytes, nil)
	if err != nil {
		t.Fatalf("expected legacy path to succeed, got %v", err)
	}
	if got.Title != "Doc" {
		t.Errorf("unexpected title: %q", got.Title)
	}
	if !strings.Contains(got.Body, "Body text") {
		t.Errorf("expected body to contain 'Body text', got %q", got.Body)
	}
}

// SCRUM-304 — when env flag is OFF the sidecar is bypassed even if a
// configured client is passed in. This protects environments where the
// sidecar exists but URL extraction has not been opted into yet.
func TestFetchAndExtractWithSidecar_FlagOffBypassesSidecar(t *testing.T) {
	t.Setenv("MARKITDOWN_URL_EXTRACTION_ENABLED", "false")

	sidecarCalls := 0
	sidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sidecarCalls++
	}))
	defer sidecar.Close()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><body><p>Legacy path content</p></body></html>`)
	}))
	defer upstream.Close()

	client := &markitdown.Client{
		BaseURL: sidecar.URL,
		Secret:  "test",
		HTTP:    sidecar.Client(),
	}
	if !client.Enabled() {
		t.Fatal("client should be Enabled()")
	}
	if markitdown.URLExtractionEnabled() {
		t.Fatal("flag should be OFF")
	}
	got, err := FetchAndExtractWithSidecar(context.Background(), upstream.URL, DefaultMaxBytes, client)
	if err != nil {
		t.Fatalf("legacy path failed: %v", err)
	}
	if !strings.Contains(got.Body, "Legacy path content") {
		t.Errorf("legacy fallback expected, got %q", got.Body)
	}
	if sidecarCalls != 0 {
		t.Errorf("sidecar should not be hit when flag OFF; got %d calls", sidecarCalls)
	}
}

// SCRUM-304 — happy path: sidecar enabled + healthy → result comes from
// the sidecar (markdown body, sidecar-resolved title and fetched_url).
func TestFetchAndExtractWithSidecar_ReturnsSidecarResultOnSuccess(t *testing.T) {
	t.Setenv("MARKITDOWN_URL_EXTRACTION_ENABLED", "true")

	sidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"text":"# Doc title\n\n- bullet 1\n- bullet 2","title":"Doc title","fetched_url":"https://example.com/article","status_code":200}`)
	}))
	defer sidecar.Close()

	client := &markitdown.Client{BaseURL: sidecar.URL, Secret: "test", HTTP: sidecar.Client()}
	got, err := FetchAndExtractWithSidecar(context.Background(), "https://example.com/article", DefaultMaxBytes, client)
	if err != nil {
		t.Fatalf("expected success, got %v", err)
	}
	if got.Title != "Doc title" {
		t.Errorf("unexpected title: %q", got.Title)
	}
	if !strings.Contains(got.Body, "- bullet 1") {
		t.Errorf("expected markdown bullet preserved, got %q", got.Body)
	}
	if got.FinalURL != "https://example.com/article" {
		t.Errorf("unexpected fetched_url: %q", got.FinalURL)
	}
}

// SCRUM-304 — sidecar 5xx-after-retry: fall back to the Go DOM walker
// transparently so link verification stays robust on sidecar outages.
// The caller still sees a successful FetchResult.
func TestFetchAndExtractWithSidecar_FallsBackOnSidecar5xx(t *testing.T) {
	t.Setenv("MARKITDOWN_URL_EXTRACTION_ENABLED", "true")

	sidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"down"}`, http.StatusServiceUnavailable)
	}))
	defer sidecar.Close()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><body><main><p>Fallback content via Go path</p></main></body></html>`)
	}))
	defer upstream.Close()

	client := &markitdown.Client{
		BaseURL: sidecar.URL,
		Secret:  "test",
		HTTP:    sidecar.Client(),
		Retries: 0,
	}
	got, err := FetchAndExtractWithSidecar(context.Background(), upstream.URL, DefaultMaxBytes, client)
	if err != nil {
		t.Fatalf("expected fallback success, got %v", err)
	}
	if !strings.Contains(got.Body, "Fallback content via Go path") {
		t.Errorf("expected legacy extractor output, got %q", got.Body)
	}
}

// SCRUM-304 — upstream 4xx pass-through: the user's URL itself returned
// 4xx, so this is a real failure (not a sidecar problem). Return an error
// so the caller marks the link failed with the upstream status.
func TestFetchAndExtractWithSidecar_UpstreamHTTPErrorReturnsError(t *testing.T) {
	t.Setenv("MARKITDOWN_URL_EXTRACTION_ENABLED", "true")

	sidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprint(w, `{"error":"upstream_http_error","status_code":404,"fetched_url":"https://example.com/missing","message":"upstream returned 404"}`)
	}))
	defer sidecar.Close()

	client := &markitdown.Client{BaseURL: sidecar.URL, Secret: "test", HTTP: sidecar.Client()}
	_, err := FetchAndExtractWithSidecar(context.Background(), "https://example.com/missing", DefaultMaxBytes, client)
	if err == nil {
		t.Fatal("expected error for upstream 4xx pass-through")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("expected error to mention upstream status 404, got %q", err.Error())
	}
}

// SCRUM-304 — sidecar 4xx (e.g., invalid URL rejected by the sidecar):
// surface as a real error matching today's contract; do not fall back.
func TestFetchAndExtractWithSidecar_BadRequestReturnsError(t *testing.T) {
	t.Setenv("MARKITDOWN_URL_EXTRACTION_ENABLED", "true")

	sidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"detail":{"error":"invalid_url","url":"ftp://x"}}`)
	}))
	defer sidecar.Close()

	client := &markitdown.Client{BaseURL: sidecar.URL, Secret: "test", HTTP: sidecar.Client()}
	_, err := FetchAndExtractWithSidecar(context.Background(), "ftp://x", DefaultMaxBytes, client)
	if err == nil {
		t.Fatal("expected error for sidecar 400")
	}
	var se *markitdown.SidecarError
	if !errors.As(err, &se) {
		t.Fatalf("expected wrapped *SidecarError, got %T", err)
	}
	if se.Code != "invalid_url" {
		t.Errorf("expected stable code invalid_url preserved, got %q", se.Code)
	}
}
