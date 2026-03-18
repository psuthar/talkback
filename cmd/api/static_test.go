package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestNewSPAFallbackHandler_BackendPrefixesNeverFallThrough(t *testing.T) {
	indexHTML := []byte("<html><body>app shell</body></html>")
	fsys := fstest.MapFS{
		"index.html":    {Data: indexHTML},
		"assets/app.js": {Data: []byte("console.log('app')")},
	}

	backendPrefixes := []string{"/api", "/ws", "/auth", "/zoom", "/admin", "/health", "/sessions", "/artifacts", "/debug"}
	h := newSPAFallbackHandler(fsys, indexHTML, backendPrefixes)

	cases := []string{
		"/api/does-not-exist",
		"/ws/session",
		"/auth/login",
		"/zoom/transcript-status",
		"/admin/unknown",
		"/health/something",
		"/sessions/unknown",
		"/artifacts/whatever",
		"/debug/fault/whatever",
	}

	for _, path := range cases {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusNotFound {
				t.Fatalf("expected 404 for %s, got %d", path, rec.Code)
			}
			if strings.Contains(rec.Body.String(), "app shell") {
				t.Fatalf("expected index.html not to be served for %s", path)
			}
		})
	}
}

func TestNewSPAFallbackHandler_ServesStaticAndSPA(t *testing.T) {
	indexHTML := []byte("<html><body>app shell</body></html>")
	fsys := fstest.MapFS{
		"index.html":    {Data: indexHTML},
		"assets/app.js": {Data: []byte("console.log('app')")},
		"favicon.ico":   {Data: []byte("fake")},
		"robots.txt":    {Data: []byte("robots")},
		"assets/readme": {Data: []byte("nope")},
	}

	backendPrefixes := []string{"/api", "/ws"}
	h := newSPAFallbackHandler(fsys, indexHTML, backendPrefixes)

	t.Run("serves index at root", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "app shell") {
			t.Fatalf("expected index html body")
		}
	})

	t.Run("serves existing static asset", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/assets/app.js", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		if strings.TrimSpace(rec.Body.String()) != "console.log('app')" {
			t.Fatalf("unexpected asset body: %q", rec.Body.String())
		}
	})

	t.Run("unknown frontend path falls back to index", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/some/deep/link", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d", rec.Code)
		}
		if !strings.Contains(rec.Body.String(), "app shell") {
			t.Fatalf("expected SPA index fallback")
		}
	})

	t.Run("missing file-looking path returns 404", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/missing/file.js", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404, got %d", rec.Code)
		}
	})
}
