package markitdown

import (
	"context"
	"errors"
	"log"
	"time"
)

// Outcome is a stable tag for telemetry aggregation. Every Instrument*
// call emits exactly one log line with one of these tags so ops can
// grep latency / error rate by outcome without parsing prose. SCRUM-306.
type Outcome string

const (
	OutcomeSuccess           Outcome = "success"
	OutcomeUnavailable       Outcome = "unavailable"        // network / 5xx-after-retry / config missing
	OutcomeBadRequest        Outcome = "bad_request"        // 4xx other than 401 / upstream
	OutcomeUnauthorized      Outcome = "unauthorized"       // 401
	OutcomeUpstreamHTTP      Outcome = "upstream_http"      // URL endpoint upstream-4xx pass-through
	OutcomeUnclassified      Outcome = "unclassified_error" // anything that doesn't match the typed sentinels
)

// classifyOutcome maps a typed client error to a stable outcome tag.
// Mirrors the errors.Is dispatch used by callers so log tags and caller
// branches agree.
func classifyOutcome(err error) Outcome {
	if err == nil {
		return OutcomeSuccess
	}
	switch {
	case errors.Is(err, ErrSidecarUnavailable):
		return OutcomeUnavailable
	case errors.Is(err, ErrSidecarUnauthorized):
		return OutcomeUnauthorized
	case errors.Is(err, ErrSidecarUpstreamHTTP):
		return OutcomeUpstreamHTTP
	case errors.Is(err, ErrSidecarBadRequest):
		return OutcomeBadRequest
	default:
		return OutcomeUnclassified
	}
}

// InstrumentedExtractImage wraps Client.ExtractImage with one structured
// log line per call. Format mirrors the existing job_processor logs:
//
//	markitdown.image: outcome=<tag> duration_ms=<n> [code=<stable>] [tokens=<n>] [err=<short>]
//
// The intent is downstream-greppable lines, not a metrics framework — see
// SCRUM-306 ticket and docs/markitdown-sidecar-runbook.md.
func InstrumentedExtractImage(ctx context.Context, c *Client, body []byte, contentType string) (*ImageResult, error) {
	if c == nil {
		// Match the disabled-client behavior of the underlying call.
		return (&Client{}).ExtractImage(ctx, body, contentType)
	}
	start := time.Now()
	res, err := c.ExtractImage(ctx, body, contentType)
	logCallOutcome("markitdown.image", start, err, imageTokens(res))
	return res, err
}

// InstrumentedExtractURL wraps Client.ExtractURL with the same per-call
// structured log line as InstrumentedExtractImage.
func InstrumentedExtractURL(ctx context.Context, c *Client, urlStr string) (*URLResult, error) {
	if c == nil {
		return (&Client{}).ExtractURL(ctx, urlStr)
	}
	start := time.Now()
	res, err := c.ExtractURL(ctx, urlStr)
	logCallOutcome("markitdown.url", start, err, 0)
	return res, err
}

// InstrumentedExtractFile wraps Client.ExtractFile with the same per-call
// structured log line. SCRUM-332. Spreadsheets don't consume tokens (no
// LLM round-trip on the sidecar side), so the tokens field is always 0.
func InstrumentedExtractFile(ctx context.Context, c *Client, body []byte, contentType, filename, fileExtension string) (*FileResult, error) {
	if c == nil {
		return (&Client{}).ExtractFile(ctx, body, contentType, filename, fileExtension)
	}
	start := time.Now()
	res, err := c.ExtractFile(ctx, body, contentType, filename, fileExtension)
	logCallOutcome("markitdown.file", start, err, 0)
	return res, err
}

func logCallOutcome(op string, start time.Time, err error, tokens int) {
	outcome := classifyOutcome(err)
	durMs := time.Since(start).Milliseconds()
	if err == nil {
		if tokens > 0 {
			log.Printf("%s: outcome=%s duration_ms=%d tokens=%d", op, outcome, durMs, tokens)
		} else {
			log.Printf("%s: outcome=%s duration_ms=%d", op, outcome, durMs)
		}
		return
	}
	var se *SidecarError
	if errors.As(err, &se) {
		log.Printf("%s: outcome=%s duration_ms=%d code=%s err=%q", op, outcome, durMs, se.Code, se.Message)
		return
	}
	log.Printf("%s: outcome=%s duration_ms=%d err=%q", op, outcome, durMs, err.Error())
}

func imageTokens(res *ImageResult) int {
	if res == nil {
		return 0
	}
	return res.TokensUsed
}
