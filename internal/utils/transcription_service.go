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
)

// TranscriptionService orchestrates the full transcription workflow
type TranscriptionService struct {
	resolver   *LoomResolver
	transcriber *WhisperTranscriber
	client     *http.Client
	tempDir    string
}

// NewTranscriptionService creates a new transcription service
func NewTranscriptionService() *TranscriptionService {
	tempDir := os.Getenv("TRANSCRIPT_TEMP_DIR")
	if tempDir == "" {
		tempDir = filepath.Join(os.TempDir(), "talkback_transcripts")
		os.MkdirAll(tempDir, 0755)
	}

	return &TranscriptionService{
		resolver:   NewLoomResolver(),
		transcriber: NewWhisperTranscriber(),
		client: &http.Client{
			Timeout: 30 * time.Minute, // Allow time for large file downloads
		},
		tempDir: tempDir,
	}
}

// DownloadMedia downloads media from a URL to a temporary file
// Returns the file path and cleanup function
// Note: HLS playlists (.m3u8) are not supported by Whisper API and will return an error
func (s *TranscriptionService) DownloadMedia(ctx context.Context, mediaURL string, jobID string) (string, func(), error) {
	// Check if URL is an HLS playlist - Whisper doesn't support these
	if strings.Contains(mediaURL, ".m3u8") || strings.Contains(mediaURL, "/hls/") || (strings.Contains(mediaURL, "playlist") && strings.Contains(mediaURL, ".m3u8")) {
		return "", nil, fmt.Errorf("HLS playlist (.m3u8) URLs are not supported by Whisper API. This Loom video only provides HLS streaming format. Solutions: 1) Upload transcript manually via the UI, 2) Use a different video that has direct MP4 access, 3) Convert HLS to MP4 using ffmpeg and upload the MP4 file")
	}

	// Determine file extension from URL or default to .mp4
	ext := ".mp4"
	if strings.HasSuffix(mediaURL, ".mp3") {
		ext = ".mp3"
	} else if strings.HasSuffix(mediaURL, ".wav") {
		ext = ".wav"
	} else if strings.HasSuffix(mediaURL, ".m4a") {
		ext = ".m4a"
	} else if strings.HasSuffix(mediaURL, ".webm") {
		ext = ".webm"
	} else if strings.Contains(mediaURL, ".m3u8") {
		// Already checked above, but double-check
		return "", nil, fmt.Errorf("HLS playlist (.m3u8) URLs are not supported by Whisper API")
	}

	// Create temporary file with appropriate extension
	tempFile := filepath.Join(s.tempDir, fmt.Sprintf("%s_%d%s", jobID, time.Now().Unix(), ext))
	
	// Create file
	out, err := os.Create(tempFile)
	if err != nil {
		return "", nil, fmt.Errorf("failed to create temp file: %w", err)
	}

	// Create cleanup function
	cleanup := func() {
		out.Close()
		os.Remove(tempFile)
	}

	// Create request with context
	req, err := http.NewRequestWithContext(ctx, "GET", mediaURL, nil)
	if err != nil {
		cleanup()
		return "", nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", "TalkBack/1.0")

	// Make request
	resp, err := s.client.Do(req)
	if err != nil {
		cleanup()
		return "", nil, fmt.Errorf("failed to download media: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusPartialContent {
		cleanup()
		return "", nil, fmt.Errorf("download failed with status %d", resp.StatusCode)
	}

	// Get content length for progress tracking (optional)
	contentLength := resp.ContentLength

	// Enforce size limit (500MB default)
	if contentLength > 0 {
		maxSize := int64(500 * 1024 * 1024) // 500MB
		if maxSizeEnv := os.Getenv("MAX_TRANSCRIPT_FILE_SIZE"); maxSizeEnv != "" {
			if parsed, err := parseSize(maxSizeEnv); err == nil {
				maxSize = parsed
			}
		}
		if contentLength > maxSize {
			cleanup()
			return "", nil, fmt.Errorf("file too large: %d bytes (max: %d bytes)", contentLength, maxSize)
		}
	}

	// Stream download
	_, err = io.Copy(out, resp.Body)
	if err != nil {
		cleanup()
		return "", nil, fmt.Errorf("failed to write file: %w", err)
	}

	if err := out.Close(); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("failed to close file: %w", err)
	}

	// Verify file exists and has content
	info, err := os.Stat(tempFile)
	if err != nil || info.Size() == 0 {
		cleanup()
		return "", nil, fmt.Errorf("downloaded file is empty or missing")
	}

	return tempFile, cleanup, nil
}

// TranscribeLoomVideo orchestrates the full transcription workflow for a Loom video
// password is optional and used for password-protected videos
func (s *TranscriptionService) TranscribeLoomVideo(ctx context.Context, loomURL string, password *string) (*WhisperTranscriptionResult, error) {
	// Step 1: Resolve Loom URL to media URL
	info, err := s.resolver.ResolveMedia(ctx, loomURL, password)
	if err != nil {
		// Return the error from resolver (it already includes helpful context)
		return nil, fmt.Errorf("failed to resolve Loom URL: %w", err)
	}

	if info.MediaURL == "" {
		return nil, fmt.Errorf("unable to resolve downloadable media URL - video may be private or use streaming. Please upload transcript manually or configure Loom API key if available")
	}

	if info.RequiresAuth {
		return nil, fmt.Errorf("video requires authentication - cannot download. Please upload transcript manually or configure Loom API authentication")
	}

	// Step 2: Download media (temporary file)
	// Use a temporary ID for the job
	jobID := fmt.Sprintf("transcript_%d", time.Now().Unix())
	tempFile, cleanup, err := s.DownloadMedia(ctx, info.MediaURL, jobID)
	if err != nil {
		return nil, fmt.Errorf("failed to download media: %w", err)
	}
	defer cleanup()

	// Step 3: Transcribe using Whisper
	result, err := s.transcriber.TranscribeFile(ctx, tempFile, "", "verbose_json", []string{"segment"})
	if err != nil {
		return nil, fmt.Errorf("failed to transcribe: %w", err)
	}

	// Add duration if available
	if info.DurationSeconds != nil {
		result.Duration = float64(*info.DurationSeconds)
	}

	return result, nil
}

// parseSize parses a size string like "500MB" or "1GB" to bytes
func parseSize(sizeStr string) (int64, error) {
	// Simple implementation - can be enhanced
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
