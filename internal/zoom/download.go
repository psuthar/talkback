package zoom

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

// DefaultDownloadTimeout is the timeout for the Zoom recording download request + body read.
var DefaultDownloadTimeout = 30*time.Minute + 30*time.Second

// DownloadRecordingFile downloads the Zoom recording file and returns the HTTP response.
// Caller must close resp.Body. Use an http.Client with timeouts; follows redirects.
// Validates status 200 and rejects text/html or JSON error payloads (logs body excerpt).
func DownloadRecordingFile(ctx context.Context, downloadURL, accessToken string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return nil, fmt.Errorf("zoom download: new request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)

	client := &http.Client{
		Timeout:   DefaultDownloadTimeout,
		Transport: http.DefaultTransport,
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("zoom download: %w", err)
	}
	finalURL := resp.Request.URL.String()
	if finalURL != downloadURL {
		log.Printf("ZOOM_DOWNLOAD redirect final_url=%s", finalURL)
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		_ = resp.Body.Close()
		ct := resp.Header.Get("Content-Type")
		log.Printf("ZOOM_DOWNLOAD status=%d content_type=%s body_excerpt=%s", resp.StatusCode, ct, string(body))
		return nil, fmt.Errorf("zoom download: status %d", resp.StatusCode)
	}

	ct := strings.TrimSpace(strings.Split(resp.Header.Get("Content-Type"), ";")[0])
	ctLower := strings.ToLower(ct)
	if ctLower == "text/html" || strings.HasPrefix(ctLower, "application/json") {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		_ = resp.Body.Close()
		log.Printf("ZOOM_DOWNLOAD content_type=%s (rejected) body_excerpt=%s", ct, string(body))
		return nil, fmt.Errorf("zoom download: unexpected content-type %s (possible error page)", ct)
	}

	log.Printf("ZOOM_DOWNLOAD status=%d content_type=%s content_length=%d final_url=%s",
		resp.StatusCode, resp.Header.Get("Content-Type"), resp.ContentLength, finalURL)
	return resp, nil
}
