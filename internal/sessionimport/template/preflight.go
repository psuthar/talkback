package template

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// SCRUM-352: synchronous preflight URL/type validation for session-template
// elements. Runs after the schema validator (SCRUM-351) and before any DB or
// storage write. Network-bound; the schema validator is the pure pre-pass.
//
// SSRF protection is enforced by a custom DialContext that blocks RFC1918,
// loopback, link-local, and cloud-metadata IP ranges at TCP-connect time.
// Combined with the schema validator's https-only enforcement, this means a
// malicious template descriptor cannot make the server fetch internal
// resources or scan the internal network.

// Preflight reason codes (extend the schema validator's catalog with
// network-bound reasons). Schema-only codes live in template.go.
const (
	ReasonURLUnreachable     = "url_unreachable"
	ReasonContentTypeMismatch = "content_type_mismatch"
	ReasonBodyTooLarge       = "body_too_large"
	ReasonRedirectLoop       = "redirect_loop"
	ReasonAuthRequired       = "auth_required"
	ReasonPrivateIPBlocked   = "private_ip_blocked"
	ReasonHTTPStatusError    = "http_status_error"
	ReasonRequestTimeout     = "request_timeout"
)

// Per-element-kind body size limits enforced at preflight via Content-Length.
const (
	MaxMaterialBytes   = 100 * 1024 * 1024 // 100 MiB
	MaxLinkBytes       = 32 * 1024         // 32 KiB
	MaxTranscriptBytes = 1 * 1024 * 1024   // 1 MiB
	MaxVideoBytes      = -1                // unbounded — embed-mode only in v1
)

// Config wires preflight runtime knobs. Zero values pick safe defaults.
type Config struct {
	// HTTPClient is injectable for tests (e.g. an httptest.Server-backed
	// client). When nil, a default client with the SSRF-aware dialer is
	// constructed. Production callers should pass nil.
	HTTPClient *http.Client

	// PerFetchTimeout is the per-element timeout (default 30s).
	PerFetchTimeout time.Duration

	// MaxRedirects caps redirect chain length (default 5).
	MaxRedirects int

	// AllowPrivateIPs disables the SSRF-prevention dialer. Tests can flip
	// this to true to hit httptest servers on 127.0.0.1; production must
	// leave it false.
	AllowPrivateIPs bool

	// UserAgent override (default mirrors internal/urlextract Chrome UA per
	// the project's URL-fetcher feedback memory).
	UserAgent string
}

// ElementMeta is the result of one successful element fetch.
type ElementMeta struct {
	URL           string
	ResolvedURL   string // after redirects (final URL)
	ContentType   string
	ContentLength int64
	StatusCode    int
}

// PreflightResult is the union of per-element errors and per-element
// successful metadata. A non-empty Errors slice means the import must NOT
// proceed — zero DB or storage writes have occurred.
type PreflightResult struct {
	Errors   []ValidationError
	Elements map[string]ElementMeta
}

const defaultUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"

// Run executes preflight against every fetchable URL in the template. The
// schema validator (ParseAndValidate) MUST have been run first so this
// function can assume t is structurally valid.
//
// Network calls run sequentially per element (worst-case len(elements) *
// PerFetchTimeout); parallelism is a separate optimization (SCRUM-357).
//
// On any preflight error the import must abort: zero DB writes, zero storage
// writes, structured per-element error response, no session created.
func Run(ctx context.Context, t *Template, cfg Config) PreflightResult {
	if cfg.PerFetchTimeout <= 0 {
		cfg.PerFetchTimeout = 30 * time.Second
	}
	if cfg.MaxRedirects <= 0 {
		cfg.MaxRedirects = 5
	}
	if cfg.UserAgent == "" {
		cfg.UserAgent = defaultUserAgent
	}
	client := cfg.HTTPClient
	if client == nil {
		client = newDefaultClient(cfg)
	}

	res := PreflightResult{Elements: map[string]ElementMeta{}}

	for i := range t.Elements {
		el := &t.Elements[i]
		fetches := elementFetches(el)
		for _, f := range fetches {
			meta, errs := fetchOne(ctx, client, cfg, el.ID, f.url, f.maxBytes, f.declaredContentType)
			if len(errs) > 0 {
				res.Errors = append(res.Errors, errs...)
				continue
			}
			// We only record one ElementMeta per element ID (the first
			// successful fetch). Composite elements like transcript with
			// both text_url and segments_url validate both but the meta
			// record reflects the primary URL.
			if _, exists := res.Elements[el.ID]; !exists {
				res.Elements[el.ID] = meta
			}
		}
	}
	return res
}

// elementFetches returns the URLs that need preflight for one element, plus
// the per-fetch limits and declared content type. video_source URLs are
// preflight-checked for reachability only; v1 does not download MP4 bodies.
type elementFetch struct {
	url                 string
	maxBytes            int64
	declaredContentType string // empty when content-type matching is not required
}

func elementFetches(el *Element) []elementFetch {
	var out []elementFetch
	switch el.Kind {
	case KindMaterial:
		if el.URL != nil && *el.URL != "" {
			ct := ""
			if el.ContentType != nil {
				ct = *el.ContentType
			}
			out = append(out, elementFetch{url: *el.URL, maxBytes: MaxMaterialBytes, declaredContentType: ct})
		}
	case KindLink:
		if el.URL != nil && *el.URL != "" {
			out = append(out, elementFetch{url: *el.URL, maxBytes: MaxLinkBytes})
		}
	case KindVideoSource:
		// Reachability-only; no content-type constraint, no size cap.
		if el.EmbedURL != nil && *el.EmbedURL != "" {
			out = append(out, elementFetch{url: *el.EmbedURL, maxBytes: MaxVideoBytes})
		}
		if el.VideoURL != nil && *el.VideoURL != "" {
			out = append(out, elementFetch{url: *el.VideoURL, maxBytes: MaxVideoBytes})
		}
	case KindTranscript:
		if el.TextURL != nil && *el.TextURL != "" {
			out = append(out, elementFetch{url: *el.TextURL, maxBytes: MaxTranscriptBytes})
		}
		if el.SegmentsURL != nil && *el.SegmentsURL != "" {
			out = append(out, elementFetch{url: *el.SegmentsURL, maxBytes: MaxTranscriptBytes})
		}
	}
	return out
}

// fetchOne does a HEAD against the URL with SSRF-aware dialing, redirect
// tracking, and the configured timeout. Returns the populated ElementMeta or
// a non-empty error slice.
func fetchOne(ctx context.Context, client *http.Client, cfg Config, elementID, rawURL string, maxBytes int64, declaredCT string) (ElementMeta, []ValidationError) {
	reqCtx, cancel := context.WithTimeout(ctx, cfg.PerFetchTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodHead, rawURL, nil)
	if err != nil {
		return ElementMeta{}, []ValidationError{{
			ElementID:  elementID,
			ReasonCode: ReasonURLUnreachable,
			Message:    fmt.Sprintf("invalid URL: %v", err),
		}}
	}
	req.Header.Set("User-Agent", cfg.UserAgent)

	resp, err := client.Do(req)
	if err != nil {
		return ElementMeta{}, []ValidationError{translateFetchError(elementID, err)}
	}
	defer resp.Body.Close()

	var errs []ValidationError

	// HTTP status: any 4xx/5xx is an error. Some servers return 405
	// (method not allowed) for HEAD; we treat that as ok-with-no-metadata
	// rather than a hard failure (the worker will GET when actually
	// importing). 200-299 is the happy path.
	if resp.StatusCode >= 400 && resp.StatusCode != http.StatusMethodNotAllowed {
		errs = append(errs, ValidationError{
			ElementID:  elementID,
			ReasonCode: ReasonHTTPStatusError,
			Message:    fmt.Sprintf("HTTP %d on HEAD", resp.StatusCode),
		})
	}

	// Auth-required: any response that gates with WWW-Authenticate is
	// outside v1 scope (SCRUM-350 non-goal: no authenticated fetch).
	if v := resp.Header.Get("WWW-Authenticate"); v != "" {
		errs = append(errs, ValidationError{
			ElementID:  elementID,
			ReasonCode: ReasonAuthRequired,
			Message:    "URL requires authentication",
		})
	}

	// Content-Length size cap.
	var contentLength int64 = -1
	if cl := resp.Header.Get("Content-Length"); cl != "" {
		if n, parseErr := strconv.ParseInt(cl, 10, 64); parseErr == nil {
			contentLength = n
		}
	}
	if maxBytes > 0 && contentLength > 0 && contentLength > maxBytes {
		errs = append(errs, ValidationError{
			ElementID:  elementID,
			ReasonCode: ReasonBodyTooLarge,
			Message:    fmt.Sprintf("Content-Length %d exceeds %d", contentLength, maxBytes),
		})
	}

	// Content-Type matching for material elements (declaredCT non-empty).
	observedCT := strings.SplitN(resp.Header.Get("Content-Type"), ";", 2)[0]
	observedCT = strings.TrimSpace(observedCT)
	if declaredCT != "" && observedCT != "" && !strings.EqualFold(observedCT, declaredCT) {
		errs = append(errs, ValidationError{
			ElementID:    elementID,
			DeclaredType: declaredCT,
			ObservedType: observedCT,
			ReasonCode:   ReasonContentTypeMismatch,
			Message:      fmt.Sprintf("Content-Type %q does not match declared %q", observedCT, declaredCT),
		})
	}

	resolvedURL := rawURL
	if resp.Request != nil && resp.Request.URL != nil {
		resolvedURL = resp.Request.URL.String()
	}
	return ElementMeta{
		URL:           rawURL,
		ResolvedURL:   resolvedURL,
		ContentType:   observedCT,
		ContentLength: contentLength,
		StatusCode:    resp.StatusCode,
	}, errs
}

// translateFetchError converts net/http transport errors into preflight
// reason codes so callers see structured diagnostics.
func translateFetchError(elementID string, err error) ValidationError {
	msg := err.Error()
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return ValidationError{ElementID: elementID, ReasonCode: ReasonRequestTimeout, Message: "request timed out"}
	case strings.Contains(msg, "stopped after") && strings.Contains(msg, "redirects"):
		return ValidationError{ElementID: elementID, ReasonCode: ReasonRedirectLoop, Message: msg}
	case strings.Contains(msg, "dial blocked"):
		// Our SSRF dialer wraps the rejection with this prefix.
		return ValidationError{ElementID: elementID, ReasonCode: ReasonPrivateIPBlocked, Message: msg}
	default:
		return ValidationError{ElementID: elementID, ReasonCode: ReasonURLUnreachable, Message: msg}
	}
}

// newDefaultClient builds an http.Client with the SSRF-aware dialer and a
// redirect-counting CheckRedirect.
func newDefaultClient(cfg Config) *http.Client {
	dialer := &net.Dialer{Timeout: cfg.PerFetchTimeout}
	transport := &http.Transport{
		Proxy:               http.ProxyFromEnvironment,
		MaxIdleConns:        10,
		IdleConnTimeout:     30 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
		DialContext:         protectedDialContext(dialer, cfg.AllowPrivateIPs),
	}
	return &http.Client{
		Transport: transport,
		Timeout:   cfg.PerFetchTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= cfg.MaxRedirects {
				return fmt.Errorf("stopped after %d redirects", cfg.MaxRedirects)
			}
			return nil
		},
	}
}

// protectedDialContext returns a DialContext that resolves and validates the
// destination IP before connecting. RFC1918, loopback, link-local, ULA, and
// the AWS/GCP/Azure metadata addresses are all blocked.
func protectedDialContext(d *net.Dialer, allowPrivate bool) func(ctx context.Context, network, addr string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, err
		}
		if !allowPrivate {
			ips, lookupErr := net.DefaultResolver.LookupIPAddr(ctx, host)
			if lookupErr != nil {
				return nil, fmt.Errorf("dial blocked: dns resolution failed: %w", lookupErr)
			}
			for _, ip := range ips {
				if isBlockedIP(ip.IP) {
					return nil, fmt.Errorf("dial blocked: %s resolves to %s (private/loopback/link-local/metadata)", host, ip.IP)
				}
			}
		}
		return d.DialContext(ctx, network, net.JoinHostPort(host, port))
	}
}

// isBlockedIP reports whether ip falls into any range we refuse to dial.
// Cloud metadata addresses (169.254.169.254, fd00:ec2::254) are caught by
// the link-local check.
var (
	blockedV4Networks []*net.IPNet
	blockedV6Networks []*net.IPNet
	blockedNetsOnce   sync.Once
)

func loadBlockedNets() {
	for _, cidr := range []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"127.0.0.0/8",
		"169.254.0.0/16", // link-local + AWS metadata
		"100.64.0.0/10",  // CGNAT / shared address space
		"0.0.0.0/8",
	} {
		if _, n, err := net.ParseCIDR(cidr); err == nil {
			blockedV4Networks = append(blockedV4Networks, n)
		}
	}
	for _, cidr := range []string{
		"::1/128",
		"fc00::/7",   // ULA
		"fe80::/10",  // link-local
		"::ffff:0:0/96", // IPv4-mapped — fallback
	} {
		if _, n, err := net.ParseCIDR(cidr); err == nil {
			blockedV6Networks = append(blockedV6Networks, n)
		}
	}
}

func isBlockedIP(ip net.IP) bool {
	blockedNetsOnce.Do(loadBlockedNets)
	if ip4 := ip.To4(); ip4 != nil {
		for _, n := range blockedV4Networks {
			if n.Contains(ip4) {
				return true
			}
		}
		return false
	}
	for _, n := range blockedV6Networks {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// MustParseURL is a tiny convenience for tests that need to assert on URL
// shape. Not used in production code paths.
func MustParseURL(s string) *url.URL {
	u, err := url.Parse(s)
	if err != nil {
		panic(err)
	}
	return u
}
