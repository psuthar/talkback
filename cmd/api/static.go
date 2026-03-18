package main

import (
	"embed"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"strings"
	"time"
)

// distFS contains the compiled React app (web/dist) embedded into the Go binary.
// In production Docker builds, web/dist is copied into cmd/api/dist before go build.
//
// We keep a tiny placeholder index.html checked in so local `go build` never fails.
//
//go:embed dist
var distFS embed.FS

func registerEmbeddedSPA() {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		log.Printf("SPA: failed to init embedded dist FS: %v", err)
		return
	}

	// Load index.html once (fallback target).
	indexBytes, err := fs.ReadFile(sub, "index.html")
	if err != nil {
		log.Printf("SPA: index.html missing in embedded dist: %v", err)
		return
	}

	// Backend-owned prefixes must never fall through to SPA index.html.
	backendPrefixes := []string{
		"/api",
		"/auth",
		"/zoom",
		"/ws",
		"/health",
		"/admin",
		"/sessions",
		"/artifacts",
		"/debug",
	}

	handler := newSPAFallbackHandler(sub, indexBytes, backendPrefixes)
	http.Handle("/", handler)

	log.Printf("SPA: embedded static assets enabled (prefix-safe index.html fallback)")
}

func newSPAFallbackHandler(sub fs.FS, indexBytes []byte, backendPrefixes []string) http.Handler {
	// Serve real static assets (assets/*, favicon, etc.) when they exist.
	fileServer := http.FileServer(http.FS(sub))

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Protect backend-owned routes from SPA fallback.
		for _, p := range backendPrefixes {
			if strings.HasPrefix(r.URL.Path, p) {
				http.NotFound(w, r)
				return
			}
		}

		// Normalize and prevent path traversal weirdness.
		// r.URL.Path is always absolute and begins with "/".
		clean := pathClean(r.URL.Path)

		// Serve index.html at "/", or any deep link that isn't a backend-owned prefix.
		if clean == "/" {
			setHTMLHeaders(w)
			_, _ = w.Write(indexBytes)
			return
		}

		rel := strings.TrimPrefix(clean, "/")

		// If a matching static file exists, serve it.
		if existsInFS(sub, rel) {
			fileServer.ServeHTTP(w, r)
			return
		}

		// If this looks like a request for a file (has an extension), return 404
		// instead of serving index.html (helps avoid confusing asset debugging).
		if strings.Contains(rel, ".") {
			http.NotFound(w, r)
			return
		}

		// SPA fallback.
		setHTMLHeaders(w)
		_, _ = w.Write(indexBytes)
	})
}

func setHTMLHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// Keep caching modest; hashed assets should be cached by default via filename.
	w.Header().Set("Cache-Control", "no-cache")
}

func existsInFS(fsys fs.FS, name string) bool {
	if name == "" {
		return false
	}
	_, err := fs.Stat(fsys, name)
	return err == nil
}

// pathClean is like path.Clean but for URLs (always POSIX).
func pathClean(p string) string {
	if p == "" {
		return "/"
	}
	// Ensure leading slash.
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	// Minimal normalization; avoid pulling in path/filepath packages.
	// We handle // and .. safely enough for embedded lookup by using fs.Stat on trimmed names.
	parts := strings.Split(p, "/")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" || part == "." {
			continue
		}
		if part == ".." {
			if len(out) > 0 {
				out = out[:len(out)-1]
			}
			continue
		}
		out = append(out, part)
	}
	return "/" + strings.Join(out, "/")
}

// Make an embed build-time placeholder error clearer (only used in tests / debug).
func mustEmbedDebug(msg string, err error) {
	if err != nil {
		log.Printf("SPA embed error: %s: %v", msg, err)
	}
}

// Ensure fmt and time are not optimized away when used only in future tweaks.
// (This function is never called; it simply keeps the imports stable.)
func _spaNoopCompile() {
	_ = fmt.Sprintf
	_ = time.Now
}
