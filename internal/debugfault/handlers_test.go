package debugfault

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestHandleError_Responds500(t *testing.T) {
	initTestMode(t)
	req := httptest.NewRequest(http.MethodPost, "/debug/fault/error", nil)
	rec := httptest.NewRecorder()
	Guard(HandleError)(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.HasPrefix(body, "synthetic") {
		t.Errorf("expected synthetic prefix in body, got %q", body)
	}
}

func TestHandleLatency_Responds200AndRespectsCap(t *testing.T) {
	initTestMode(t)

	// Normal case
	req := httptest.NewRequest(http.MethodPost, "/debug/fault/latency?ms=10", nil)
	rec := httptest.NewRecorder()
	Guard(HandleLatency)(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "10 ms") {
		t.Errorf("expected body to contain 10 ms, got %q", body)
	}

	// Cap: request 99999, should cap at 10000
	req2 := httptest.NewRequest(http.MethodPost, "/debug/fault/latency?ms=99999", nil)
	rec2 := httptest.NewRecorder()
	Guard(HandleLatency)(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Errorf("expected 200 for capped latency, got %d", rec2.Code)
	}
	if !strings.Contains(rec2.Body.String(), "10000 ms") {
		t.Errorf("expected cap at 10000 ms, got %q", rec2.Body.String())
	}

	// Negative rejected
	req3 := httptest.NewRequest(http.MethodPost, "/debug/fault/latency?ms=-1", nil)
	rec3 := httptest.NewRecorder()
	Guard(HandleLatency)(rec3, req3)
	if rec3.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for negative ms, got %d", rec3.Code)
	}
}

func TestHandleLatency_DefaultMs(t *testing.T) {
	initTestMode(t)
	req := httptest.NewRequest(http.MethodPost, "/debug/fault/latency", nil)
	rec := httptest.NewRecorder()
	Guard(HandleLatency)(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), strconv.Itoa(defaultLatencyMs)) {
		t.Errorf("expected default %d ms in body", defaultLatencyMs)
	}
}
