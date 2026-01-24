package utils

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// FetchLoomTranscript attempts to fetch a transcript from Loom for a given video URL
// Returns the transcript text if successful, or an error if it fails
// This requires LOOM_API_KEY to be set in the environment
func FetchLoomTranscript(ctx context.Context, videoURL string) (string, error) {
	apiKey := os.Getenv("LOOM_API_KEY")
	if apiKey == "" {
		return "", fmt.Errorf("LOOM_API_KEY not set - cannot fetch transcript from Loom API")
	}

	// Extract video ID from Loom URL
	// Format: https://www.loom.com/share/{video-id} or https://www.loom.com/embed/{video-id}
	videoID := extractLoomVideoID(videoURL)
	if videoID == "" {
		return "", fmt.Errorf("invalid Loom video URL format: %s", videoURL)
	}

	// Loom API endpoint for getting video details (including transcript)
	// NOTE: This endpoint structure is a placeholder and needs to be verified against Loom's actual API documentation
	// The actual endpoint may be different. Common possibilities:
	// - https://api.loom.com/v1/videos/{id}/transcript
	// - https://api.loom.com/v1/videos/{id} (with transcript in response)
	// - https://api.loom.com/v2/videos/{id}/transcript
	// Please check Loom's official API documentation and update this endpoint accordingly
	apiURL := fmt.Sprintf("https://api.loom.com/v1/videos/%s/transcript", videoID)

	// Create HTTP request with timeout
	reqCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, "GET", apiURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	// Add API key to headers
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", apiKey))
	req.Header.Set("Content-Type", "application/json")

	// Make request
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch transcript from Loom: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("Loom API returned status %d: %s", resp.StatusCode, string(body))
	}

	// Parse response
	// Note: Response structure depends on Loom API - adjust as needed
	var response struct {
		Transcript string `json:"transcript"`
		// Alternative structure might be:
		// Data struct {
		//   Transcript string `json:"transcript"`
		// } `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return "", fmt.Errorf("failed to parse Loom API response: %w", err)
	}

	if response.Transcript == "" {
		return "", fmt.Errorf("transcript not available for this video (may not be transcribed yet)")
	}

	return response.Transcript, nil
}

// extractLoomVideoID extracts the video ID from a Loom URL
// Supports formats:
// - https://www.loom.com/share/{video-id}
// - https://www.loom.com/embed/{video-id}
// - http://www.loom.com/share/{video-id}
func extractLoomVideoID(url string) string {
	if url == "" {
		return ""
	}

	// Remove protocol if present
	url = strings.TrimPrefix(url, "https://")
	url = strings.TrimPrefix(url, "http://")
	url = strings.TrimPrefix(url, "www.")

	// Extract video ID from /share/ or /embed/ path
	parts := strings.Split(url, "/")
	for i, part := range parts {
		if (part == "share" || part == "embed") && i+1 < len(parts) {
			videoID := parts[i+1]
			// Remove query parameters if present
			if idx := strings.Index(videoID, "?"); idx >= 0 {
				videoID = videoID[:idx]
			}
			return videoID
		}
	}

	return ""
}

// CanFetchLoomTranscript checks if Loom transcript fetching is available
// (i.e., if LOOM_API_KEY is set)
func CanFetchLoomTranscript() bool {
	return os.Getenv("LOOM_API_KEY") != ""
}
