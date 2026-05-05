package urlextract

import (
	"context"
	"errors"
	"fmt"
	"log"

	"github.com/psuthar/talkback/internal/markitdown"
)

// FetchAndExtractWithSidecar wraps FetchAndExtract with an optional first
// hop through the markitdown sidecar (SCRUM-304). When the sidecar is
// configured + the URL-extraction feature flag is on, the sidecar's
// MarkItDown HTML→Markdown path produces richer content than the
// hand-rolled Go DOM walker (preserves headings, lists, tables).
//
// Failure semantics — the link-extraction job stays robust on sidecar
// problems:
//   - Sidecar unreachable / 5xx-after-retry / unauthorized: log at INFO
//     and fall back to the Go DOM walker. Caller sees identical
//     FetchResult shape; downstream chunking is unaffected.
//   - Sidecar 400 (bad request to the sidecar — e.g. invalid URL): same
//     contract as today; return an error so the caller marks the link
//     failed with a sensible message.
//   - Upstream 4xx (the user's URL itself returned 4xx): return an error
//     mirroring the upstream status. Caller marks the link failed with
//     the real upstream code in error_message.
//
// When the sidecar is disabled or the client is nil, this function is
// equivalent to FetchAndExtract — no behavior change for unconfigured
// environments.
func FetchAndExtractWithSidecar(ctx context.Context, rawURL string, maxBytes int64, client *markitdown.Client) (*FetchResult, error) {
	useSidecar := client != nil && client.Enabled() && markitdown.URLExtractionEnabled()
	if !useSidecar {
		return FetchAndExtract(ctx, rawURL, maxBytes)
	}

	result, err := client.ExtractURL(ctx, rawURL)
	if err == nil {
		return &FetchResult{
			Title:    result.Title,
			Body:     result.Text,
			FinalURL: result.FetchedURL,
		}, nil
	}

	// Upstream 4xx pass-through: the user's URL itself is bad. Return an
	// error so the caller marks the link failed with the upstream status.
	if errors.Is(err, markitdown.ErrSidecarUpstreamHTTP) {
		var se *markitdown.SidecarError
		if errors.As(err, &se) && se.HTTPStatus > 0 {
			return nil, fmt.Errorf("url returned status %d", se.HTTPStatus)
		}
		return nil, err
	}

	// Bad request to the sidecar (e.g., the URL didn't parse on the
	// sidecar side). Surface as a real error matching today's contract.
	if errors.Is(err, markitdown.ErrSidecarBadRequest) {
		return nil, fmt.Errorf("url extraction rejected: %w", err)
	}

	// Sidecar unavailable, unauthorized, or any other failure mode →
	// fall back to the Go path. Log once at INFO so we can spot patterns.
	log.Printf("urlextract: sidecar fallback markitdown_sidecar_fallback url=%s err=%v", rawURL, err)
	return FetchAndExtract(ctx, rawURL, maxBytes)
}
