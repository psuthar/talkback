package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/newrelic/go-agent/v3/newrelic"
)

// Test_healthHandler_GET returns 200 and JSON status ok.
func Test_healthHandler_GET(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	healthHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("healthHandler GET: status = %d, want 200", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("healthHandler GET: Content-Type = %q, want application/json", ct)
	}
	var out healthResponse
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("healthHandler GET: decode response: %v", err)
	}
	if out.Status != "ok" {
		t.Errorf("healthHandler GET: status = %q, want ok", out.Status)
	}
}

// Test_healthHandler_nonGET returns 405.
func Test_healthHandler_nonGET(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/health", nil)
	w := httptest.NewRecorder()

	healthHandler(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("healthHandler POST: status = %d, want 405", w.Code)
	}
}

// Test_newrelic_WrapHandleFunc_nilApp verifies that wrapping a handler with
// newrelic.WrapHandleFunc(nil, pattern, handler) does not change behavior (nil app is no-op).
func Test_newrelic_WrapHandleFunc_nilApp(t *testing.T) {
	_, wrapped := newrelic.WrapHandleFunc(nil, "/health", healthHandler)
	if wrapped == nil {
		t.Fatal("WrapHandleFunc returned nil handler")
	}

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	wrapped(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("wrapped healthHandler: status = %d, want 200", w.Code)
	}
	var out healthResponse
	if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
		t.Fatalf("wrapped healthHandler: decode response: %v", err)
	}
	if out.Status != "ok" {
		t.Errorf("wrapped healthHandler: status = %q, want ok", out.Status)
	}
}
