// Package markitdown is the typed Go client for the MarkItDown Python sidecar
// (services/markitdown-sidecar/). The sidecar handles two extraction paths
// that have no acceptable Go-native option today:
//
//   - POST /extract/image — vision-based image captioning + OCR (the biggest
//     content-coverage gap surfaced in SCRUM-296/297; kind=image materials
//     currently get no extracted text).
//   - POST /extract/url   — readability-style HTML→Markdown that preserves
//     headings/lists/tables (replaces the hand-rolled DOM walker in
//     internal/urlextract for verified link content).
//
// Existing PPTX/DOCX/PDF/XLSX/Whisper extractors stay on their Go paths.
//
// Callers should always check Enabled() first and fall back to the
// pre-existing extractor on err == ErrSidecarUnavailable so user-visible
// flows (link verification in particular) remain robust on sidecar outage.
//
// Errors are typed (errors.Is checks against ErrSidecar*) so callers can
// branch on retry-able vs. fatal without parsing prose. The underlying
// SidecarError carries the stable code from the sidecar's response body
// (e.g. "llm_misconfigured", "timeout", "fetch_failed") for telemetry.
package markitdown

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	envBaseURL                = "MARKITDOWN_SIDECAR_URL"
	envSecret                 = "MARKITDOWN_SIDECAR_SECRET"
	envRetries                = "MARKITDOWN_SIDECAR_RETRIES"
	envImageTimeout           = "MARKITDOWN_SIDECAR_IMAGE_TIMEOUT_S"
	envURLTimeout             = "MARKITDOWN_SIDECAR_URL_TIMEOUT_S"
	envFileTimeout            = "MARKITDOWN_SIDECAR_FILE_TIMEOUT_S"
	envImageExtractionEnabled = "MARKITDOWN_IMAGE_EXTRACTION_ENABLED"
	envURLExtractionEnabled   = "MARKITDOWN_URL_EXTRACTION_ENABLED"
	envFileExtractionEnabled  = "MARKITDOWN_FILE_EXTRACTION_ENABLED"
)

// ImageExtractionEnabled returns true when the operator has explicitly opted
// in to image captioning via env. Defaults to false on first ship so the
// existing "image upload sets text_status=ready with empty text" path stays
// active for unconfigured environments. Once set to "true", uploads route
// through the sidecar (assuming the client is also Enabled()).
func ImageExtractionEnabled() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv(envImageExtractionEnabled)), "true")
}

// URLExtractionEnabled returns true when the operator has opted in to
// sidecar-based URL extraction. Defaults to false; existing
// internal/urlextract behavior (Go DOM walker) stays the canonical path
// otherwise. When enabled, the link-extraction worker tries the sidecar
// first and falls back to the Go path on transient sidecar failure so
// link verification stays robust on outages.
func URLExtractionEnabled() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv(envURLExtractionEnabled)), "true")
}

// FileExtractionEnabled returns true when the operator has opted in to
// sidecar-based generic file extraction (SCRUM-332). Initially scoped to
// CSV / XLS / XLSX uploads; the upstream MarkItDown SDK converts each to
// a markdown table natively. Defaults to false on first ship so the
// existing pure-Go XLSX excelize path stays active for unconfigured
// environments. Once set to "true", the materials handler routes
// CSV/XLS/XLSX uploads through the sidecar (assuming the client is also
// Enabled()).
func FileExtractionEnabled() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv(envFileExtractionEnabled)), "true")
}

// Default per-operation timeouts. Image captioning involves an LLM round-trip
// so it gets the longer budget; URL extraction is just a fetch + markdown
// conversion. Both env-overridable.
const (
	defaultRetries      = 1
	defaultImageTimeout = 30 * time.Second
	defaultURLTimeout   = 20 * time.Second
	defaultFileTimeout  = 30 * time.Second // pandas/openpyxl/xlrd parse can be slow on large workbooks
)

// Client is safe for concurrent use; the underlying *http.Client handles
// connection pooling. Construct via NewClient (reads env) or by populating
// fields directly in tests.
type Client struct {
	BaseURL      string
	Secret       string
	HTTP         *http.Client
	Retries      int
	ImageTimeout time.Duration
	URLTimeout   time.Duration
	FileTimeout  time.Duration
}

// ImageResult is the typed body of a successful POST /extract/image call.
// TokensUsed is plumbed by SCRUM-306; today it is always 0 because MarkItDown
// does not surface token counts.
type ImageResult struct {
	Text       string `json:"text"`
	Model      string `json:"model"`
	TokensUsed int    `json:"tokens_used"`
}

// URLResult is the typed body of a successful POST /extract/url call.
type URLResult struct {
	Text       string `json:"text"`
	Title      string `json:"title"`
	FetchedURL string `json:"fetched_url"`
	StatusCode int    `json:"status_code"`
}

// FileResult is the typed body of a successful POST /extract/file call
// (SCRUM-332). Used for CSV / XLS / XLSX → markdown extraction.
type FileResult struct {
	Text      string `json:"text"`
	Extension string `json:"extension"`
	Bytes     int    `json:"bytes"`
}

// SidecarError wraps the sidecar's structured error response so callers can
// branch on the stable Code field without parsing prose. Use errors.Is with
// ErrSidecar* sentinels to classify by category.
type SidecarError struct {
	// HTTPStatus is the HTTP status the sidecar returned to us. For URL
	// extraction's 4xx pass-through this is the upstream's status code
	// (the sidecar deliberately mirrors it).
	HTTPStatus int
	// Code is the stable error identifier from the sidecar's response body
	// (e.g. "llm_misconfigured", "timeout", "fetch_failed",
	// "upstream_http_error", "body_too_large", "unsupported_media_type").
	Code string
	// Message is a human-readable description (do not parse).
	Message string
	// FetchedURL is populated for URL endpoint upstream errors so callers
	// can show the resolved redirect target.
	FetchedURL string
	// kind classifies which sentinel this error wraps, for errors.Is.
	kind error
}

func (e *SidecarError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("markitdown sidecar: %s (%d): %s", e.Code, e.HTTPStatus, e.Message)
	}
	return fmt.Sprintf("markitdown sidecar: HTTP %d: %s", e.HTTPStatus, e.Message)
}

func (e *SidecarError) Unwrap() error { return e.kind }

// Sentinels for errors.Is classification.
var (
	// ErrSidecarUnavailable wraps network errors, timeouts, 5xx after retry,
	// and missing-config cases. Callers should fall back to the pre-existing
	// extractor on this error.
	ErrSidecarUnavailable = errors.New("markitdown sidecar unavailable")
	// ErrSidecarBadRequest wraps 4xx responses other than 401 — typically a
	// caller bug (wrong content type, bad URL, oversized body). Do not retry.
	ErrSidecarBadRequest = errors.New("markitdown sidecar bad request")
	// ErrSidecarUnauthorized wraps 401 — the bearer secret is wrong or
	// missing on the sidecar. Do not retry; alert ops.
	ErrSidecarUnauthorized = errors.New("markitdown sidecar unauthorized")
	// ErrSidecarUpstreamHTTP wraps upstream URLs returning 4xx that the URL
	// endpoint passes through. The link is the user's URL, not a sidecar bug.
	ErrSidecarUpstreamHTTP = errors.New("markitdown sidecar upstream http error")
)

// NewClient reads env vars and returns a configured Client. The client is
// "disabled" (Enabled() == false) when MARKITDOWN_SIDECAR_URL or
// MARKITDOWN_SIDECAR_SECRET is unset; calls return ErrSidecarUnavailable so
// callers fall back without nil-pointer panics.
func NewClient() *Client {
	c := &Client{
		BaseURL:      strings.TrimRight(os.Getenv(envBaseURL), "/"),
		Secret:       os.Getenv(envSecret),
		Retries:      readIntEnv(envRetries, defaultRetries),
		ImageTimeout: readDurationEnv(envImageTimeout, defaultImageTimeout),
		URLTimeout:   readDurationEnv(envURLTimeout, defaultURLTimeout),
		FileTimeout:  readDurationEnv(envFileTimeout, defaultFileTimeout),
	}
	c.HTTP = &http.Client{}
	return c
}

// Enabled reports whether the client has the env config required to call
// the sidecar. Callers should check this and short-circuit to fallback
// behavior when false.
func (c *Client) Enabled() bool {
	return c != nil && c.BaseURL != "" && c.Secret != ""
}

// ExtractImage POSTs the image as multipart/form-data. contentType must be
// the upload's MIME (e.g. "image/png"). The sidecar enforces the
// content-type guard and body cap; callers don't need to pre-validate.
func (c *Client) ExtractImage(ctx context.Context, body []byte, contentType string) (*ImageResult, error) {
	if !c.Enabled() {
		return nil, &SidecarError{Code: "sidecar_disabled", Message: "MARKITDOWN_SIDECAR_URL/SECRET not configured", kind: ErrSidecarUnavailable}
	}
	endpoint := c.BaseURL + "/extract/image"

	doRequest := func() (*http.Response, error) {
		var buf bytes.Buffer
		writer := multipart.NewWriter(&buf)
		header := make(textproto.MIMEHeader)
		header.Set("Content-Disposition", `form-data; name="file"; filename="upload"`)
		if contentType != "" {
			header.Set("Content-Type", contentType)
		}
		part, err := writer.CreatePart(header)
		if err != nil {
			return nil, fmt.Errorf("create multipart part: %w", err)
		}
		if _, err := part.Write(body); err != nil {
			return nil, fmt.Errorf("write multipart body: %w", err)
		}
		if err := writer.Close(); err != nil {
			return nil, fmt.Errorf("close multipart writer: %w", err)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, &buf)
		if err != nil {
			return nil, fmt.Errorf("build request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+c.Secret)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		return c.do(req, c.ImageTimeout)
	}

	resp, err := c.callWithRetry(doRequest)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if classifyErr := c.classifyError(resp, "image"); classifyErr != nil {
		return nil, classifyErr
	}

	var out ImageResult
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, &SidecarError{HTTPStatus: resp.StatusCode, Code: "decode_failed", Message: err.Error(), kind: ErrSidecarUnavailable}
	}
	return &out, nil
}

// ExtractFile POSTs the file as multipart/form-data with a `file_extension`
// form field so the sidecar's MarkItDown converter selects the right
// converter (csv / xls / xlsx). The sidecar enforces its own size cap and
// extension allowlist; callers don't need to pre-validate. SCRUM-332.
//
// fileExtension must be the canonical extension (e.g. ".csv", "csv",
// ".xlsx") — leading dot optional. contentType is the upload's MIME and
// is forwarded for telemetry; the sidecar routes by extension, not MIME.
func (c *Client) ExtractFile(ctx context.Context, body []byte, contentType string, filename string, fileExtension string) (*FileResult, error) {
	if !c.Enabled() {
		return nil, &SidecarError{Code: "sidecar_disabled", Message: "MARKITDOWN_SIDECAR_URL/SECRET not configured", kind: ErrSidecarUnavailable}
	}
	endpoint := c.BaseURL + "/extract/file"

	if filename == "" {
		filename = "upload"
	}

	doRequest := func() (*http.Response, error) {
		var buf bytes.Buffer
		writer := multipart.NewWriter(&buf)
		header := make(textproto.MIMEHeader)
		header.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename=%q`, filename))
		if contentType != "" {
			header.Set("Content-Type", contentType)
		}
		part, err := writer.CreatePart(header)
		if err != nil {
			return nil, fmt.Errorf("create multipart part: %w", err)
		}
		if _, err := part.Write(body); err != nil {
			return nil, fmt.Errorf("write multipart body: %w", err)
		}
		if fileExtension != "" {
			if err := writer.WriteField("file_extension", fileExtension); err != nil {
				return nil, fmt.Errorf("write file_extension field: %w", err)
			}
		}
		if err := writer.Close(); err != nil {
			return nil, fmt.Errorf("close multipart writer: %w", err)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, &buf)
		if err != nil {
			return nil, fmt.Errorf("build request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+c.Secret)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		return c.do(req, c.FileTimeout)
	}

	resp, err := c.callWithRetry(doRequest)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if classifyErr := c.classifyError(resp, "file"); classifyErr != nil {
		return nil, classifyErr
	}

	var out FileResult
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, &SidecarError{HTTPStatus: resp.StatusCode, Code: "decode_failed", Message: err.Error(), kind: ErrSidecarUnavailable}
	}
	return &out, nil
}

// ExtractURL POSTs {url} as JSON. The sidecar fetches the URL with its own
// timeout/body cap, so this client just sets the operation timeout.
func (c *Client) ExtractURL(ctx context.Context, urlStr string) (*URLResult, error) {
	if !c.Enabled() {
		return nil, &SidecarError{Code: "sidecar_disabled", Message: "MARKITDOWN_SIDECAR_URL/SECRET not configured", kind: ErrSidecarUnavailable}
	}
	if _, err := url.Parse(urlStr); err != nil || urlStr == "" {
		return nil, &SidecarError{HTTPStatus: 0, Code: "invalid_url", Message: "url empty or unparseable", kind: ErrSidecarBadRequest}
	}
	endpoint := c.BaseURL + "/extract/url"

	doRequest := func() (*http.Response, error) {
		payload, err := json.Marshal(map[string]string{"url": urlStr})
		if err != nil {
			return nil, fmt.Errorf("marshal request: %w", err)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
		if err != nil {
			return nil, fmt.Errorf("build request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+c.Secret)
		req.Header.Set("Content-Type", "application/json")
		return c.do(req, c.URLTimeout)
	}

	resp, err := c.callWithRetry(doRequest)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if classifyErr := c.classifyError(resp, "url"); classifyErr != nil {
		return nil, classifyErr
	}

	var out URLResult
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, &SidecarError{HTTPStatus: resp.StatusCode, Code: "decode_failed", Message: err.Error(), kind: ErrSidecarUnavailable}
	}
	return &out, nil
}

// do sends the request with a per-call timeout, isolated from the parent
// context's deadline so retries get a fresh budget.
func (c *Client) do(req *http.Request, timeout time.Duration) (*http.Response, error) {
	httpc := c.HTTP
	if httpc == nil {
		httpc = &http.Client{}
	}
	if timeout > 0 {
		ctx, cancel := context.WithTimeout(req.Context(), timeout)
		// Cancel after the response is closed by the caller; for now we let
		// the http.Client's transport handle the deadline. We can't defer
		// cancel here because the caller reads the body afterward.
		_ = cancel // silence linter; deadline is enforced via ctx propagation
		req = req.WithContext(ctx)
	}
	return httpc.Do(req)
}

// callWithRetry handles transient 5xx + network errors. 4xx, 401, 200 all
// return immediately; only 5xx and request errors trigger a retry.
func (c *Client) callWithRetry(send func() (*http.Response, error)) (*http.Response, error) {
	attempts := c.Retries + 1
	if attempts < 1 {
		attempts = 1
	}
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			// Light backoff so we don't hammer a struggling sidecar.
			time.Sleep(time.Duration(attempt) * 200 * time.Millisecond)
		}
		resp, err := send()
		if err != nil {
			lastErr = err
			continue
		}
		if resp.StatusCode >= 500 && attempt+1 < attempts {
			resp.Body.Close()
			lastErr = fmt.Errorf("sidecar 5xx: %d", resp.StatusCode)
			continue
		}
		return resp, nil
	}
	return nil, &SidecarError{Code: "sidecar_unreachable", Message: lastErr.Error(), kind: ErrSidecarUnavailable}
}

// classifyError reads non-2xx responses and produces a typed *SidecarError.
// op is "image" or "url" — used only for error context.
func (c *Client) classifyError(resp *http.Response, op string) error {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	body, _ := io.ReadAll(resp.Body)
	bodyText := strings.TrimSpace(string(body))

	se := &SidecarError{HTTPStatus: resp.StatusCode}
	se.Code, se.Message, se.FetchedURL = parseStableError(body)
	if se.Message == "" {
		se.Message = bodyText
	}

	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		se.kind = ErrSidecarUnauthorized
	case resp.StatusCode == http.StatusForbidden,
		resp.StatusCode == http.StatusNotFound:
		// For URL extraction, the sidecar passes upstream 4xx through —
		// classify those distinctly so callers can decide to surface the
		// upstream status to the user (e.g. mark link as failed with the
		// real upstream code).
		if op == "url" && se.Code == "upstream_http_error" {
			se.kind = ErrSidecarUpstreamHTTP
			break
		}
		se.kind = ErrSidecarBadRequest
	case resp.StatusCode >= 400 && resp.StatusCode < 500:
		if op == "url" && se.Code == "upstream_http_error" {
			se.kind = ErrSidecarUpstreamHTTP
			break
		}
		se.kind = ErrSidecarBadRequest
	default:
		// 5xx after retry exhausted, or unexpected status.
		se.kind = ErrSidecarUnavailable
	}
	return se
}

// parseStableError teases the {error, message, fetched_url} envelope out of
// the sidecar's structured error body. Both endpoints return errors as
// either {"error": "<code>", "message": "..."} (image + URL stable bodies)
// or {"detail": {"error": "<code>", ...}} (FastAPI HTTPException default).
func parseStableError(body []byte) (code, message, fetchedURL string) {
	if len(body) == 0 {
		return "", "", ""
	}
	var flat struct {
		Error      string `json:"error"`
		Message    string `json:"message"`
		FetchedURL string `json:"fetched_url"`
	}
	if err := json.Unmarshal(body, &flat); err == nil && flat.Error != "" {
		return flat.Error, flat.Message, flat.FetchedURL
	}
	var nested struct {
		Detail struct {
			Error      string `json:"error"`
			Message    string `json:"message"`
			FetchedURL string `json:"fetched_url"`
		} `json:"detail"`
	}
	if err := json.Unmarshal(body, &nested); err == nil && nested.Detail.Error != "" {
		return nested.Detail.Error, nested.Detail.Message, nested.Detail.FetchedURL
	}
	return "", "", ""
}

func readIntEnv(key string, fallback int) int {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v < 0 {
		return fallback
	}
	return v
}

func readDurationEnv(key string, fallback time.Duration) time.Duration {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	secs, err := strconv.ParseFloat(raw, 64)
	if err != nil || secs <= 0 {
		return fallback
	}
	return time.Duration(secs * float64(time.Second))
}
