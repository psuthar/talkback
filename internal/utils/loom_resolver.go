package utils

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"
)

// LoomMediaInfo contains resolved media information from a Loom URL
type LoomMediaInfo struct {
	VideoID         string // Extracted Loom video ID
	MediaURL        string // Direct download URL for MP4/audio
	EmbedURL        string // Embed URL for iframe
	DurationSeconds *int   // Duration if available
	RequiresAuth    bool   // Whether the video requires authentication
	IsPublic        bool   // Whether the video is publicly accessible
	Error           error  // Any error encountered
}

// LoomResolver handles resolution of Loom share URLs to downloadable media
type LoomResolver struct {
	client        *http.Client
	graphqlClient *LoomGraphQLClient
}

// NewLoomResolver creates a new Loom resolver
func NewLoomResolver() *LoomResolver {
	return &LoomResolver{
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		graphqlClient: NewLoomGraphQLClient(),
	}
}

// ResolveMedia attempts to resolve a Loom share URL to a downloadable media URL
// password is optional and used for password-protected videos
func (r *LoomResolver) ResolveMedia(ctx context.Context, loomURL string, password *string) (*LoomMediaInfo, error) {
	// Extract video ID from URL
	videoID := extractLoomVideoID(loomURL)
	if videoID == "" {
		return nil, fmt.Errorf("invalid Loom URL format: %s", loomURL)
	}

	info := &LoomMediaInfo{
		VideoID:  videoID,
		EmbedURL: fmt.Sprintf("https://www.loom.com/embed/%s", videoID),
	}

	// Approach 1: Try Loom GraphQL API first (best method)
	// Try MP4-only first (Whisper-compatible)
	if result, err := r.graphqlClient.ResolveVideoSource(ctx, videoID, password); err == nil {
		info.MediaURL = result.MediaURL
		info.RequiresAuth = result.RequiresAuth
		info.IsPublic = !result.RequiresAuth

		// Check if we got an HLS playlist - if so, try to get MP4 instead
		if strings.Contains(info.MediaURL, ".m3u8") || strings.Contains(info.MediaURL, "/hls/") || strings.Contains(info.MediaURL, "playlist") {
			log.Printf("GraphQL returned HLS playlist for video %s, trying MP4/WEBM-only resolution", videoID)
			// Try again requesting only MP4/WEBM (no streaming formats)
			if mp4Result, err := r.graphqlClient.ResolveVideoSourceWithFormats(ctx, videoID, password, []string{"MP4", "WEBM"}); err == nil && mp4Result != nil {
				mp4URL := mp4Result.MediaURL
				// Verify it's not an HLS playlist
				if mp4URL != "" && !strings.Contains(mp4URL, ".m3u8") && !strings.Contains(mp4URL, "/hls/") && !strings.Contains(mp4URL, "playlist") {
					log.Printf("Successfully resolved Loom video %s to MP4 via GraphQL (MP4-only request)", videoID)
					info.MediaURL = mp4URL
					info.RequiresAuth = mp4Result.RequiresAuth
					info.IsPublic = !mp4Result.RequiresAuth
					return info, nil
				}
				log.Printf("MP4/WEBM-only request still returned HLS playlist or empty URL for video %s: %s", videoID, mp4URL)
			} else {
				log.Printf("MP4/WEBM-only resolution failed for video %s: %v", videoID, err)
			}
			// If MP4-only failed, return error immediately since we can't transcribe HLS
			return info, fmt.Errorf("Loom video %s is only available as HLS playlist (.m3u8), which Whisper API cannot process. Please upload transcript manually", videoID)
		}
		log.Printf("Successfully resolved Loom video %s via GraphQL", videoID)
		return info, nil
	} else {
		log.Printf("GraphQL resolution failed for video %s: %v", videoID, err)
	}

	// Approach 2: Try Loom REST API if API key is available (legacy)
	if apiKey := os.Getenv("LOOM_API_KEY"); apiKey != "" {
		if mediaURL, err := r.resolveViaAPI(ctx, videoID, apiKey); err == nil && mediaURL != "" {
			info.MediaURL = mediaURL
			info.IsPublic = true
			return info, nil
		}
	}

	// Approach 3: Attempt to extract from Loom's embed page (fallback)
	if mediaURL, duration, err := r.resolveViaEmbedPage(ctx, info.EmbedURL); err == nil && mediaURL != "" {
		info.MediaURL = mediaURL
		if duration != nil {
			info.DurationSeconds = duration
		}
		info.IsPublic = true
		return info, nil
	}

	// Approach 4: Try direct CDN URL pattern (last resort, rarely works)
	if directURL := r.tryDirectCDNPattern(videoID); directURL != "" {
		if r.validateMediaURL(ctx, directURL) {
			info.MediaURL = directURL
			info.IsPublic = true
			return info, nil
		}
	}

	// If all approaches fail, provide detailed error message
	msg := "unable to resolve downloadable media URL"
	if password != nil && *password != "" {
		msg += " - video may require different password or be private"
	} else {
		msg += " - video may be private, password-protected, or use streaming (HLS/DASH)"
	}
	msg += ". Please upload transcript manually or provide password if available."

	return info, fmt.Errorf("%s", msg)
}

// resolveViaAPI attempts to get media URL using Loom API
func (r *LoomResolver) resolveViaAPI(ctx context.Context, videoID, apiKey string) (string, error) {
	apiURL := fmt.Sprintf("https://api.loom.com/v1/videos/%s", videoID)

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return "", err
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", apiKey))
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("Loom API returned status %d", resp.StatusCode)
	}

	// Parse response to extract media URL
	// Note: Actual structure depends on Loom API - adjust as needed
	var response struct {
		MediaURL string `json:"media_url"`
		// Alternative fields that might exist:
		// Video struct {
		//   MediaURL string `json:"media_url"`
		// } `json:"video"`
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	if err := json.Unmarshal(body, &response); err != nil {
		return "", fmt.Errorf("failed to parse API response: %w", err)
	}

	return response.MediaURL, nil
}

// resolveViaEmbedPage attempts to extract media URL from Loom's embed page HTML
func (r *LoomResolver) resolveViaEmbedPage(ctx context.Context, embedURL string) (string, *int, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", embedURL, nil)
	if err != nil {
		return "", nil, err
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; TalkBack/1.0)")

	resp, err := r.client.Do(req)
	if err != nil {
		return "", nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", nil, fmt.Errorf("failed to fetch embed page: status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", nil, err
	}

	html := string(body)

	// Try to extract media URL from various patterns in the HTML
	// Pattern 1: Look for CDN URLs in script tags or data attributes
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`"cdnUrl"\s*:\s*"([^"]+)"`),
		regexp.MustCompile(`"mediaUrl"\s*:\s*"([^"]+)"`),
		regexp.MustCompile(`cdn\.loom\.com/sessions/[^"'\s]+\.mp4`),
		regexp.MustCompile(`https://cdn\.loom\.com/sessions/[a-zA-Z0-9\-]+\.mp4`),
	}

	for _, pattern := range patterns {
		matches := pattern.FindStringSubmatch(html)
		if len(matches) > 0 {
			mediaURL := matches[len(matches)-1]
			// Unescape if needed
			if unescaped, err := url.QueryUnescape(mediaURL); err == nil {
				mediaURL = unescaped
			}
			if strings.HasPrefix(mediaURL, "http") {
				return mediaURL, nil, nil
			}
		}
	}

	return "", nil, fmt.Errorf("could not extract media URL from embed page")
}

// tryDirectCDNPattern attempts common CDN URL patterns
func (r *LoomResolver) tryDirectCDNPattern(videoID string) string {
	// Try different CDN patterns
	patterns := []string{
		fmt.Sprintf("https://cdn.loom.com/sessions/%s.mp4", videoID),
		fmt.Sprintf("https://cdn.loom.com/videos/%s.mp4", videoID),
	}

	for _, pattern := range patterns {
		if r.validateMediaURL(context.Background(), pattern) {
			return pattern
		}
	}

	return ""
}

// validateMediaURL checks if a media URL is accessible
func (r *LoomResolver) validateMediaURL(ctx context.Context, mediaURL string) bool {
	req, err := http.NewRequestWithContext(ctx, "HEAD", mediaURL, nil)
	if err != nil {
		return false
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	// Accept 200, 206 (partial content), or 301/302 redirects
	return resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusPartialContent ||
		resp.StatusCode == http.StatusMovedPermanently || resp.StatusCode == http.StatusFound
}

// GenerateJobKey creates an idempotency key for transcript jobs
// Format: hash(video_source_id + source_url)
func GenerateJobKey(videoSourceID, sourceURL string) string {
	data := fmt.Sprintf("%s:%s", videoSourceID, sourceURL)
	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:])
}
