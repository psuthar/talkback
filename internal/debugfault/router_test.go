package debugfault

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestFaultRouter_DisabledReturns404(t *testing.T) {
	os.Unsetenv("OBS_TEST_MODE")
	defer os.Unsetenv("OBS_TEST_MODE")

	req := httptest.NewRequest(http.MethodPost, "/debug/fault/error", nil)
	rec := httptest.NewRecorder()
	FaultRouter(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 when disabled, got %d", rec.Code)
	}
}

func TestFaultRouter_UnknownPathReturns404(t *testing.T) {
	initTestMode(t)
	req := httptest.NewRequest(http.MethodPost, "/debug/fault/unknown", nil)
	rec := httptest.NewRecorder()
	FaultRouter(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 for unknown path, got %d", rec.Code)
	}
}

func TestFaultRouter_GetReturns405(t *testing.T) {
	initTestMode(t)
	req := httptest.NewRequest(http.MethodGet, "/debug/fault/error", nil)
	rec := httptest.NewRecorder()
	FaultRouter(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected 405 for GET, got %d", rec.Code)
	}
}
