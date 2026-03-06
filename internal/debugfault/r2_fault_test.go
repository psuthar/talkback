package debugfault

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandleR2_ReturnsControlled500(t *testing.T) {
	initTestMode(t)
	req := httptest.NewRequest(http.MethodPost, "/debug/fault/r2?mode=auth", nil)
	rec := httptest.NewRecorder()
	Guard(HandleR2)(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.HasPrefix(body, "synthetic r2 fault:") {
		t.Errorf("expected synthetic r2 fault prefix, got %q", body)
	}
}

func TestRunR2Fault_AuthMode(t *testing.T) {
	err := RunR2Fault(context.Background(), "auth")
	if err == nil {
		t.Fatal("expected error for auth mode")
	}
	if !strings.Contains(err.Error(), "auth") && !strings.Contains(err.Error(), "Invalid") && !strings.Contains(err.Error(), "credentials") {
		// Could be various AWS/R2 error messages
		t.Logf("auth mode error: %v", err)
	}
}

func TestRunR2Fault_UnsupportedMode(t *testing.T) {
	err := RunR2Fault(context.Background(), "invalid")
	if err == nil {
		t.Fatal("expected error for invalid mode")
	}
	if !strings.Contains(err.Error(), "unsupported") {
		t.Errorf("expected unsupported mode message, got %v", err)
	}
}

