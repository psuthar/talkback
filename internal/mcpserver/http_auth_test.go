package mcpserver

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHTTPBearerAuthMiddleware_noHeader(t *testing.T) {
	t.Parallel()
	a := Auth{acceptedKeys: []string{"secret"}, RequireClientKey: true}
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true })
	h := a.HTTPBearerAuthMiddleware(next)

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.Contains(t, rec.Body.String(), "unauthorized")
	require.False(t, called, "handler must not be called without a token")
}

func TestHTTPBearerAuthMiddleware_wrongToken(t *testing.T) {
	t.Parallel()
	a := Auth{acceptedKeys: []string{"secret"}, RequireClientKey: true}
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true })
	h := a.HTTPBearerAuthMiddleware(next)

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.False(t, called)
}

func TestHTTPBearerAuthMiddleware_validToken(t *testing.T) {
	t.Parallel()
	a := Auth{acceptedKeys: []string{"secret"}, RequireClientKey: true}
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	h := a.HTTPBearerAuthMiddleware(next)

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.True(t, called, "handler must be called with a valid token")
}

func TestHTTPBearerAuthMiddleware_rotationKey(t *testing.T) {
	t.Parallel()
	a := Auth{acceptedKeys: []string{"alpha", "beta"}, RequireClientKey: true}
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	h := a.HTTPBearerAuthMiddleware(next)

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	req.Header.Set("Authorization", "Bearer beta")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.True(t, called)
}

func TestHTTPBearerAuthMiddleware_queryParamKey_valid(t *testing.T) {
	t.Parallel()
	a := Auth{acceptedKeys: []string{"secret"}, RequireClientKey: true}
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	h := a.HTTPBearerAuthMiddleware(next)

	req := httptest.NewRequest(http.MethodPost, "/?key=secret", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.True(t, called, "handler must be called with a valid ?key= param")
}

func TestHTTPBearerAuthMiddleware_queryParamKey_wrong(t *testing.T) {
	t.Parallel()
	a := Auth{acceptedKeys: []string{"secret"}, RequireClientKey: true}
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true })
	h := a.HTTPBearerAuthMiddleware(next)

	req := httptest.NewRequest(http.MethodPost, "/?key=wrong", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.False(t, called)
}

func TestHTTPBearerAuthMiddleware_headerTakesPrecedenceOverQueryParam(t *testing.T) {
	t.Parallel()
	a := Auth{acceptedKeys: []string{"header-key"}, RequireClientKey: true}
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	h := a.HTTPBearerAuthMiddleware(next)

	// Valid header key + wrong query param — header wins
	req := httptest.NewRequest(http.MethodPost, "/?key=wrong", nil)
	req.Header.Set("Authorization", "Bearer header-key")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.True(t, called)
}

// HTTPBearerAuthMiddleware always enforces the key even when RequireClientKey=false,
// because the HTTP transport is network-exposed (unlike stdio).
func TestHTTPBearerAuthMiddleware_alwaysEnforced_evenWhenRequireClientKeyFalse(t *testing.T) {
	t.Parallel()
	a := Auth{acceptedKeys: []string{"secret"}, RequireClientKey: false}
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true })
	h := a.HTTPBearerAuthMiddleware(next)

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	// No Authorization header
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.False(t, called)
}
