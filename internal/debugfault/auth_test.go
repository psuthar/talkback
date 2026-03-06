package debugfault

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestGuard_DisabledModeRejectsAccess(t *testing.T) {
	os.Unsetenv("OBS_TEST_MODE")
	defer os.Unsetenv("OBS_TEST_MODE")

	handler := Guard(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called when disabled")
	})
	req := httptest.NewRequest(http.MethodPost, "/debug/fault/error", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 when disabled, got %d", rec.Code)
	}
}

func TestGuard_TokenMismatchRejectsAccess(t *testing.T) {
	os.Setenv("OBS_TEST_MODE", "true")
	os.Setenv("OBS_TEST_TOKEN", "secret123")
	defer func() {
		os.Unsetenv("OBS_TEST_MODE")
		os.Unsetenv("OBS_TEST_TOKEN")
	}()

	handler := Guard(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called when token wrong")
	})
	req := httptest.NewRequest(http.MethodPost, "/debug/fault/error", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403 when token missing, got %d", rec.Code)
	}

	req2 := httptest.NewRequest(http.MethodPost, "/debug/fault/error", nil)
	req2.Header.Set(HeaderTestToken, "wrong-token")
	rec2 := httptest.NewRecorder()
	handler(rec2, req2)
	if rec2.Code != http.StatusForbidden {
		t.Errorf("expected 403 when token wrong, got %d", rec2.Code)
	}
}

func TestGuard_TokenMatchAllowsAccess(t *testing.T) {
	os.Setenv("OBS_TEST_MODE", "true")
	os.Setenv("OBS_TEST_TOKEN", "secret123")
	defer func() {
		os.Unsetenv("OBS_TEST_MODE")
		os.Unsetenv("OBS_TEST_TOKEN")
	}()

	called := false
	handler := Guard(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	req := httptest.NewRequest(http.MethodPost, "/debug/fault/error", nil)
	req.Header.Set(HeaderTestToken, "secret123")
	rec := httptest.NewRecorder()
	handler(rec, req)
	if !called {
		t.Error("handler should be called when token matches")
	}
}

func initTestMode(t *testing.T) {
	t.Helper()
	os.Setenv("OBS_TEST_MODE", "true")
	os.Unsetenv("OBS_TEST_TOKEN")
	t.Cleanup(func() {
		os.Unsetenv("OBS_TEST_MODE")
	})
}

func TestGuard_NoTokenRequiredWhenUnset(t *testing.T) {
	os.Setenv("OBS_TEST_MODE", "true")
	os.Unsetenv("OBS_TEST_TOKEN")
	defer os.Unsetenv("OBS_TEST_MODE")

	called := false
	handler := Guard(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	req := httptest.NewRequest(http.MethodPost, "/debug/fault/error", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)
	if !called {
		t.Error("handler should be called when OBS_TEST_TOKEN unset")
	}
}
