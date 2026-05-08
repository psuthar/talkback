package template

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// SCRUM-352: preflight URL/type validation tests.
//
// Tests use httptest.Server (binds to 127.0.0.1) and therefore set
// Config.AllowPrivateIPs = true. SSRF protection itself is unit-tested via
// isBlockedIP() against the ranges in loadBlockedNets — no network calls.

func TestPreflight_HappyPath_AllElementKinds(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/agenda.pdf":
			w.Header().Set("Content-Type", "application/pdf")
			w.Header().Set("Content-Length", "1024")
			w.WriteHeader(http.StatusOK)
		case "/policy":
			w.Header().Set("Content-Type", "text/html")
			w.Header().Set("Content-Length", "2048")
			w.WriteHeader(http.StatusOK)
		case "/embed/abc":
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(http.StatusOK)
		case "/transcript.txt":
			w.Header().Set("Content-Type", "text/plain")
			w.Header().Set("Content-Length", "512")
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	tmpl := &Template{
		Version: 1,
		Title:   "x",
		Elements: []Element{
			{Kind: KindMaterial, ID: "agenda", Title: strPtr("A"), URL: strPtr(srv.URL + "/agenda.pdf"), ContentType: strPtr("application/pdf")},
			{Kind: KindLink, ID: "policy", URL: strPtr(srv.URL + "/policy")},
			{Kind: KindVideoSource, ID: "rec", EmbedURL: strPtr(srv.URL + "/embed/abc")},
			{Kind: KindTranscript, ID: "tx", Source: strPtr("whisper"), TextURL: strPtr(srv.URL + "/transcript.txt")},
		},
	}
	res := Run(context.Background(), tmpl, Config{AllowPrivateIPs: true, HTTPClient: srv.Client()})
	assert.Empty(t, res.Errors, "happy path must have no preflight errors")
	assert.Len(t, res.Elements, 4, "every element must produce one ElementMeta")
}

func TestPreflight_HTTPStatusError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	tmpl := &Template{Version: 1, Title: "x", Elements: []Element{
		{Kind: KindLink, ID: "l", URL: strPtr(srv.URL)},
	}}
	res := Run(context.Background(), tmpl, Config{AllowPrivateIPs: true, HTTPClient: srv.Client()})
	require.NotEmpty(t, res.Errors)
	assert.True(t, hasPreflightReason(res.Errors, ReasonHTTPStatusError))
}

func TestPreflight_AuthRequired(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("WWW-Authenticate", `Basic realm="example"`)
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	tmpl := &Template{Version: 1, Title: "x", Elements: []Element{
		{Kind: KindLink, ID: "l", URL: strPtr(srv.URL)},
	}}
	res := Run(context.Background(), tmpl, Config{AllowPrivateIPs: true, HTTPClient: srv.Client()})
	require.NotEmpty(t, res.Errors)
	assert.True(t, hasPreflightReason(res.Errors, ReasonAuthRequired), "auth_required must surface")
	assert.True(t, hasPreflightReason(res.Errors, ReasonHTTPStatusError), "401 status also surfaces")
}

func TestPreflight_ContentTypeMismatch(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html") // declared application/pdf
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	tmpl := &Template{Version: 1, Title: "x", Elements: []Element{
		{Kind: KindMaterial, ID: "m", Title: strPtr("M"), URL: strPtr(srv.URL), ContentType: strPtr("application/pdf")},
	}}
	res := Run(context.Background(), tmpl, Config{AllowPrivateIPs: true, HTTPClient: srv.Client()})
	require.NotEmpty(t, res.Errors)
	got := false
	for _, e := range res.Errors {
		if e.ReasonCode == ReasonContentTypeMismatch {
			got = true
			assert.Equal(t, "application/pdf", e.DeclaredType)
			assert.Equal(t, "text/html", e.ObservedType)
		}
	}
	assert.True(t, got, "must surface ReasonContentTypeMismatch with declared+observed")
}

func TestPreflight_ContentTypeMismatch_StripParams(t *testing.T) {
	t.Parallel()
	// Server sends "text/csv; charset=utf-8" — we should match against "text/csv".
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	tmpl := &Template{Version: 1, Title: "x", Elements: []Element{
		{Kind: KindMaterial, ID: "m", Title: strPtr("M"), URL: strPtr(srv.URL), ContentType: strPtr("text/csv")},
	}}
	res := Run(context.Background(), tmpl, Config{AllowPrivateIPs: true, HTTPClient: srv.Client()})
	assert.Empty(t, res.Errors, "media-type charset suffix must not trip the comparator")
}

func TestPreflight_BodyTooLarge_Material(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Length", strconv.FormatInt(MaxMaterialBytes+1, 10))
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	tmpl := &Template{Version: 1, Title: "x", Elements: []Element{
		{Kind: KindMaterial, ID: "m", Title: strPtr("M"), URL: strPtr(srv.URL), ContentType: strPtr("application/pdf")},
	}}
	res := Run(context.Background(), tmpl, Config{AllowPrivateIPs: true, HTTPClient: srv.Client()})
	require.NotEmpty(t, res.Errors)
	assert.True(t, hasPreflightReason(res.Errors, ReasonBodyTooLarge))
}

func TestPreflight_BodyTooLarge_Link(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Header().Set("Content-Length", strconv.FormatInt(MaxLinkBytes+1, 10))
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	tmpl := &Template{Version: 1, Title: "x", Elements: []Element{
		{Kind: KindLink, ID: "l", URL: strPtr(srv.URL)},
	}}
	res := Run(context.Background(), tmpl, Config{AllowPrivateIPs: true, HTTPClient: srv.Client()})
	require.NotEmpty(t, res.Errors)
	assert.True(t, hasPreflightReason(res.Errors, ReasonBodyTooLarge))
}

func TestPreflight_RedirectLoop(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, r.URL.Path+"a", http.StatusFound)
	}))
	defer srv.Close()
	tmpl := &Template{Version: 1, Title: "x", Elements: []Element{
		{Kind: KindLink, ID: "l", URL: strPtr(srv.URL + "/start")},
	}}
	// MaxRedirects: 3 → after 3 hops the dialer aborts.
	res := Run(context.Background(), tmpl, Config{
		AllowPrivateIPs: true,
		HTTPClient:      newDefaultClient(Config{AllowPrivateIPs: true, PerFetchTimeout: 5 * time.Second, MaxRedirects: 3}),
	})
	require.NotEmpty(t, res.Errors)
	assert.True(t, hasPreflightReason(res.Errors, ReasonRedirectLoop))
}

func TestPreflight_RequestTimeout(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	tmpl := &Template{Version: 1, Title: "x", Elements: []Element{
		{Kind: KindLink, ID: "l", URL: strPtr(srv.URL)},
	}}
	res := Run(context.Background(), tmpl, Config{
		AllowPrivateIPs: true,
		PerFetchTimeout: 200 * time.Millisecond,
		HTTPClient:      newDefaultClient(Config{AllowPrivateIPs: true, PerFetchTimeout: 200 * time.Millisecond}),
	})
	require.NotEmpty(t, res.Errors)
	// Timeout surfaces as either ReasonRequestTimeout or ReasonURLUnreachable
	// depending on which client layer caught it. Accept either.
	gotTimeout := hasPreflightReason(res.Errors, ReasonRequestTimeout) || strings.Contains(res.Errors[0].Message, "deadline")
	assert.True(t, gotTimeout, "timeout must surface as a structured error: %+v", res.Errors)
}

// TestPreflight_VideoSource_Reachability_Only verifies that video_source
// URLs are not subject to content-type or size constraints — they pass
// preflight as long as they're reachable.
func TestPreflight_VideoSource_Reachability_Only(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html") // mismatch with mp4 declared? — no declaration for video_source
		w.Header().Set("Content-Length", "999999999")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	tmpl := &Template{Version: 1, Title: "x", Elements: []Element{
		{Kind: KindVideoSource, ID: "v", EmbedURL: strPtr(srv.URL)},
	}}
	res := Run(context.Background(), tmpl, Config{AllowPrivateIPs: true, HTTPClient: srv.Client()})
	assert.Empty(t, res.Errors, "video_source URLs are reachability-only in v1")
}

// TestPreflight_SSRF_BlockedByDefault confirms that without
// AllowPrivateIPs=true, the protected dialer blocks 127.0.0.1. We hit a
// 127.0.0.1 httptest server through a NEW default client (no explicit
// AllowPrivateIPs) and expect the SSRF dialer to refuse the connect.
func TestPreflight_SSRF_BlockedByDefault(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	tmpl := &Template{Version: 1, Title: "x", Elements: []Element{
		{Kind: KindLink, ID: "l", URL: strPtr(srv.URL)},
	}}
	// AllowPrivateIPs defaults to false → the dialer must block the connect.
	res := Run(context.Background(), tmpl, Config{PerFetchTimeout: 5 * time.Second})
	require.NotEmpty(t, res.Errors)
	assert.True(t, hasPreflightReason(res.Errors, ReasonPrivateIPBlocked), "must reject 127.0.0.1 with private_ip_blocked: %+v", res.Errors)
}

// TestIsBlockedIP_Ranges unit-tests the SSRF allowlist directly so we don't
// need network access.
func TestIsBlockedIP_Ranges(t *testing.T) {
	t.Parallel()
	cases := []struct {
		ip      string
		blocked bool
	}{
		// Public — must NOT be blocked.
		{"8.8.8.8", false},
		{"1.1.1.1", false},
		{"2001:4860:4860::8888", false},
		// Private / loopback / link-local / metadata — must be blocked.
		{"127.0.0.1", true},
		{"10.0.0.1", true},
		{"172.16.0.1", true},
		{"172.31.255.255", true},
		{"192.168.1.1", true},
		{"169.254.169.254", true}, // AWS metadata
		{"100.64.0.1", true},      // CGNAT
		{"::1", true},
		{"fe80::1", true},
		{"fd00::1", true},
	}
	for _, c := range cases {
		got := isBlockedIP(parseIP(t, c.ip))
		assert.Equal(t, c.blocked, got, "%s blocked? want=%v got=%v", c.ip, c.blocked, got)
	}
}

// --- helpers ---

func strPtr(s string) *string { return &s }

func parseIP(t *testing.T, s string) net.IP {
	t.Helper()
	ip := net.ParseIP(s)
	require.NotNil(t, ip, "parse IP %q", s)
	return ip
}

func hasPreflightReason(errs []ValidationError, code string) bool {
	for _, e := range errs {
		if e.ReasonCode == code {
			return true
		}
	}
	return false
}
