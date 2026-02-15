package utils

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"strings"
	"time"
)

// WhisperTranscriptionResult contains the result of a Whisper transcription
type WhisperTranscriptionResult struct {
	Text      string          `json:"text"`       // Full transcript text
	Language  string          `json:"language"`   // Detected language code
	Segments  []TranscriptSegment `json:"segments,omitempty"` // Timestamped segments (optional)
	Duration  float64         `json:"duration"`   // Duration in seconds
}

// TranscriptSegment represents a timestamped segment of the transcript
type TranscriptSegment struct {
	ID       int     `json:"id"`        // Segment ID
	Start    float64 `json:"start"`     // Start time in seconds
	End      float64 `json:"end"`       // End time in seconds
	Text     string  `json:"text"`      // Segment text
}

// WhisperTranscriber handles transcription via OpenAI Whisper API
type WhisperTranscriber struct {
	apiKey  string
	baseURL string
	model   string
	client  *http.Client
}

// NewWhisperTranscriber creates a new Whisper transcriber (for long-form; uses WHISPER_MODEL).
func NewWhisperTranscriber() *WhisperTranscriber {
	model := os.Getenv("WHISPER_MODEL")
	if model == "" {
		model = "whisper-1"
	}
	return NewWhisperTranscriberForSTT(model)
}

// NewWhisperTranscriberForSTT creates a Whisper API transcriber with the given model (for mic STT).
// Uses OPENAI_API_KEY and OPENAI_BASE_URL. Timeout is 10 min for file jobs; callers (e.g. hybrid) use context timeout for mic.
func NewWhisperTranscriberForSTT(model string) *WhisperTranscriber {
	apiKey := os.Getenv("OPENAI_API_KEY")
	baseURL := os.Getenv("OPENAI_BASE_URL")
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1/audio/transcriptions"
	}
	if model == "" {
		model = "whisper-1"
	}
	return &WhisperTranscriber{
		apiKey:  apiKey,
		baseURL: baseURL,
		model:   model,
		client: &http.Client{
			Timeout: 10 * time.Minute,
		},
	}
}

// TranscribeFile transcribes an audio/video file using Whisper API
// filePath: path to the media file to transcribe
// language: optional language code (empty string for auto-detect)
// responseFormat: optional format ("json", "text", "srt", "verbose_json", "vtt")
// timestampGranularities: optional array of granularities (["word", "segment"])
func (w *WhisperTranscriber) TranscribeFile(ctx context.Context, filePath string, language string, responseFormat string, timestampGranularities []string) (*WhisperTranscriptionResult, error) {
	if w.apiKey == "" {
		return nil, fmt.Errorf("OPENAI_API_KEY not set")
	}

	// Open the file
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	// Get file info for filename
	fileInfo, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("failed to get file info: %w", err)
	}

	// Create multipart form
	var requestBody bytes.Buffer
	writer := multipart.NewWriter(&requestBody)

	// Add file
	part, err := writer.CreateFormFile("file", fileInfo.Name())
	if err != nil {
		return nil, fmt.Errorf("failed to create form file: %w", err)
	}
	if _, err := io.Copy(part, file); err != nil {
		return nil, fmt.Errorf("failed to copy file to form: %w", err)
	}

	// Add model
	if err := writer.WriteField("model", w.model); err != nil {
		return nil, fmt.Errorf("failed to write model field: %w", err)
	}

	// Add language if specified
	if language != "" {
		if err := writer.WriteField("language", language); err != nil {
			return nil, fmt.Errorf("failed to write language field: %w", err)
		}
	}

	// Add response format (default to verbose_json for segments)
	if responseFormat == "" {
		responseFormat = "verbose_json"
	}
	if err := writer.WriteField("response_format", responseFormat); err != nil {
		return nil, fmt.Errorf("failed to write response_format field: %w", err)
	}

	// Add timestamp granularities if specified
	// For multipart form data, array fields must be sent as multiple fields with the same name
	if len(timestampGranularities) > 0 {
		for _, gran := range timestampGranularities {
			if err := writer.WriteField("timestamp_granularities[]", gran); err != nil {
				return nil, fmt.Errorf("failed to write timestamp_granularities field: %w", err)
			}
		}
	}

	// Close writer
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("failed to close multipart writer: %w", err)
	}

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, "POST", w.baseURL, &requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", w.apiKey))
	req.Header.Set("Content-Type", writer.FormDataContentType())

	// Make request
	resp, err := w.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to make request to Whisper API: %w", err)
	}
	defer resp.Body.Close()

	// Read response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var errorResp struct {
			Error struct {
				Message string `json:"message"`
				Type    string `json:"type"`
			} `json:"error"`
		}
		if err := json.Unmarshal(body, &errorResp); err == nil {
			return nil, fmt.Errorf("Whisper API error (%s): %s", errorResp.Error.Type, errorResp.Error.Message)
		}
		return nil, fmt.Errorf("Whisper API returned status %d: %s", resp.StatusCode, string(body))
	}

	// Parse response based on format
	if responseFormat == "verbose_json" || responseFormat == "json" {
		var result struct {
			Text     string    `json:"text"`
			Language string    `json:"language"`
			Duration float64   `json:"duration"`
			Segments []TranscriptSegment `json:"segments"`
		}
		
		if err := json.Unmarshal(body, &result); err != nil {
			return nil, fmt.Errorf("failed to parse Whisper response: %w", err)
		}

		// Map segments if they exist
		segments := make([]TranscriptSegment, len(result.Segments))
		for i, seg := range result.Segments {
			segments[i] = TranscriptSegment{
				ID:    seg.ID,
				Start: seg.Start,
				End:   seg.End,
				Text:  seg.Text,
			}
		}

		return &WhisperTranscriptionResult{
			Text:     result.Text,
			Language: result.Language,
			Duration: result.Duration,
			Segments: segments,
		}, nil
	}

	// For text format, just return the body as text
	return &WhisperTranscriptionResult{
		Text: string(body),
	}, nil
}

// CanTranscribe checks if Whisper transcription is available
func (w *WhisperTranscriber) CanTranscribe() bool {
	return w.apiKey != ""
}

// TranscribeAudio implements the SpeechToText interface for short mic recordings.
// Uses response_format "text" for minimal payload. Confidence is not provided by the API.
func (w *WhisperTranscriber) TranscribeAudio(ctx context.Context, inputFilePath string) (string, *float32, error) {
	res, err := w.TranscribeFile(ctx, inputFilePath, "", "text", nil)
	if err != nil {
		return "", nil, err
	}
	return strings.TrimSpace(res.Text), nil, nil
}
