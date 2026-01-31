package utils

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

// IsLoomShareURL checks if a URL is a Loom share URL
func IsLoomShareURL(url string) bool {
	urlLower := strings.ToLower(url)
	return strings.Contains(urlLower, "loom.com/share/") || 
		   strings.Contains(urlLower, "www.loom.com/share/")
}

// ProbeMediaURL performs a HEAD request to check if a URL is a direct media file
// Returns: isMedia (bool), contentType (string), error
func ProbeMediaURL(ctx context.Context, url string) (bool, string, error) {
	// Create request with context
	req, err := http.NewRequestWithContext(ctx, "HEAD", url, nil)
	if err != nil {
		return false, "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", "TalkBack/1.0")

	client := &http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// Follow redirects but limit to 5
			if len(via) >= 5 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}

	resp, err := client.Do(req)
	if err != nil {
		return false, "", fmt.Errorf("failed to probe URL: %w", err)
	}
	defer resp.Body.Close()

	// Check status code
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return false, "", fmt.Errorf("URL requires authentication (status %d)", resp.StatusCode)
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return false, "", fmt.Errorf("URL not accessible (status %d)", resp.StatusCode)
	}

	// Check Content-Type
	contentType := resp.Header.Get("Content-Type")
	if contentType == "" {
		// If no Content-Type, check URL extension
		if strings.HasSuffix(strings.ToLower(url), ".mp4") {
			return true, "video/mp4", nil
		}
		return false, "", fmt.Errorf("no Content-Type header and URL doesn't end with .mp4")
	}

	// Check if Content-Type is video/*
	if strings.HasPrefix(strings.ToLower(contentType), "video/") {
		return true, contentType, nil
	}

	// Also check URL extension as fallback
	if strings.HasSuffix(strings.ToLower(url), ".mp4") {
		return true, contentType, nil
	}

	return false, contentType, fmt.Errorf("Content-Type '%s' is not a video type", contentType)
}

// DownloadVideoToStorage downloads a video from a URL and stores it locally
// Returns the object key (storage path) and error
func DownloadVideoToStorage(ctx context.Context, url string, sessionID uuid.UUID, videoID uuid.UUID) (string, error) {
	// Create storage directory
	storageDir := filepath.Join("data", "uploads", sessionID.String(), "videos")
	if err := os.MkdirAll(storageDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create storage directory: %w", err)
	}

	// Determine file extension from URL or Content-Type
	ext := ".mp4"
	if strings.HasSuffix(strings.ToLower(url), ".webm") {
		ext = ".webm"
	} else if strings.HasSuffix(strings.ToLower(url), ".m4v") {
		ext = ".m4v"
	}

	// Create file path
	objectKey := filepath.Join("data", "uploads", sessionID.String(), "videos", videoID.String()+ext)
	filePath := objectKey

	// Create file
	out, err := os.Create(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to create file: %w", err)
	}
	defer out.Close()

	// Create request
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", "TalkBack/1.0")

	// Make request
	client := &http.Client{
		Timeout: 30 * time.Minute, // Allow time for large file downloads
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to download video: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		return "", fmt.Errorf("download failed with status %d", resp.StatusCode)
	}

	// Check content length for size limits
	maxSize := int64(500 * 1024 * 1024) // 500MB default
	if maxSizeEnv := os.Getenv("MAX_VIDEO_FILE_SIZE_MB"); maxSizeEnv != "" {
		if parsed, err := parseSizeMB(maxSizeEnv + "MB"); err == nil {
			maxSize = parsed
		}
	}

	if resp.ContentLength > 0 && resp.ContentLength > maxSize {
		return "", fmt.Errorf("file too large: %d bytes (max: %d bytes)", resp.ContentLength, maxSize)
	}

	// Stream download
	_, err = io.Copy(out, resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to write file: %w", err)
	}

	if err := out.Close(); err != nil {
		return "", fmt.Errorf("failed to close file: %w", err)
	}

	// Verify file exists and has content
	info, err := os.Stat(filePath)
	if err != nil || info.Size() == 0 {
		return "", fmt.Errorf("downloaded file is empty or missing")
	}

	return objectKey, nil
}

// parseSizeMB parses a size string like "500MB" or "1GB" to bytes
func parseSizeMB(sizeStr string) (int64, error) {
	sizeStr = strings.TrimSpace(sizeStr)
	var multiplier int64 = 1

	if strings.HasSuffix(sizeStr, "MB") || strings.HasSuffix(sizeStr, "mb") {
		multiplier = 1024 * 1024
		sizeStr = sizeStr[:len(sizeStr)-2]
	} else if strings.HasSuffix(sizeStr, "GB") || strings.HasSuffix(sizeStr, "gb") {
		multiplier = 1024 * 1024 * 1024
		sizeStr = sizeStr[:len(sizeStr)-2]
	} else if strings.HasSuffix(sizeStr, "KB") || strings.HasSuffix(sizeStr, "kb") {
		multiplier = 1024
		sizeStr = sizeStr[:len(sizeStr)-2]
	}

	var size int64
	if _, err := fmt.Sscanf(sizeStr, "%d", &size); err != nil {
		return 0, err
	}

	return size * multiplier, nil
}
