package utils

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWhisperTranscriber_TranscribeFile(t *testing.T) {
	// Skip test if OPENAI_API_KEY is not set
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		t.Skip("OPENAI_API_KEY not set, skipping integration test")
	}

	// Create a temporary test file
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "test.mp3")
	
	// Create a minimal MP3 file (empty file for testing)
	file, err := os.Create(testFile)
	require.NoError(t, err)
	file.Close()

	transcriber := NewWhisperTranscriber()
	if !transcriber.CanTranscribe() {
		t.Skip("Whisper transcriber not configured")
	}

	t.Run("transcribes file without timestamp granularities", func(t *testing.T) {
		ctx := context.Background()
		result, err := transcriber.TranscribeFile(ctx, testFile, "", "verbose_json", nil)
		
		// This may fail with the test file, but we're testing the request format
		// If it fails, it should fail with a meaningful error, not a format error
		if err != nil {
			// If error is about file format or API, that's expected with test file
			assert.NotContains(t, err.Error(), "timestamp_granularities")
			assert.NotContains(t, err.Error(), "enum")
		} else {
			// If successful, result should be valid
			assert.NotNil(t, result)
		}
	})

	t.Run("transcribes file with segment granularity", func(t *testing.T) {
		ctx := context.Background()
		result, err := transcriber.TranscribeFile(ctx, testFile, "", "verbose_json", []string{"segment"})
		
		// Should not fail with timestamp_granularities format error
		if err != nil {
			// Should not be a format error about granularities
			assert.NotContains(t, err.Error(), "timestamp_granularities")
			assert.NotContains(t, err.Error(), "enum")
			assert.NotContains(t, err.Error(), "Input should be")
		} else {
			assert.NotNil(t, result)
		}
	})

	t.Run("transcribes file with word granularity", func(t *testing.T) {
		ctx := context.Background()
		result, err := transcriber.TranscribeFile(ctx, testFile, "", "verbose_json", []string{"word"})
		
		if err != nil {
			assert.NotContains(t, err.Error(), "timestamp_granularities")
			assert.NotContains(t, err.Error(), "enum")
		} else {
			assert.NotNil(t, result)
		}
	})

	t.Run("transcribes file with multiple granularities", func(t *testing.T) {
		ctx := context.Background()
		result, err := transcriber.TranscribeFile(ctx, testFile, "", "verbose_json", []string{"segment", "word"})
		
		if err != nil {
			assert.NotContains(t, err.Error(), "timestamp_granularities")
			assert.NotContains(t, err.Error(), "enum")
		} else {
			assert.NotNil(t, result)
		}
	})
}

func TestWhisperTranscriber_TranscribeFile_RequestFormat(t *testing.T) {
	// Test that the request format is correct (multiple form fields, not JSON string)
	apiKey := "test-api-key"
	
	// Create a mock server to capture the request
	var capturedFormValues map[string][]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Parse multipart form
		err := r.ParseMultipartForm(10 << 20) // 10MB
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		
		// Capture form values
		capturedFormValues = r.MultipartForm.Value
		
		// Return a mock response
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"text":     "Test transcript",
			"language": "en",
			"duration": 10.5,
			"segments": []map[string]interface{}{},
		})
	}))
	defer server.Close()

	// Set API key
	originalKey := os.Getenv("OPENAI_API_KEY")
	os.Setenv("OPENAI_API_KEY", apiKey)
	defer func() {
		if originalKey != "" {
			os.Setenv("OPENAI_API_KEY", originalKey)
		} else {
			os.Unsetenv("OPENAI_API_KEY")
		}
	}()

	transcriber := &WhisperTranscriber{
		apiKey:  apiKey,
		baseURL: server.URL,
		model:   "whisper-1",
		client:  server.Client(),
	}

	// Create a temporary test file
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "test.mp3")
	file, err := os.Create(testFile)
	require.NoError(t, err)
	file.WriteString("fake audio data")
	file.Close()

	t.Run("sends timestamp_granularities as form fields not JSON string", func(t *testing.T) {
		capturedFormValues = nil
		ctx := context.Background()
		_, err := transcriber.TranscribeFile(ctx, testFile, "", "verbose_json", []string{"segment"})
		
		require.NoError(t, err)
		require.NotNil(t, capturedFormValues)

		// Check that timestamp_granularities is sent as form values, not as a JSON string
		granularities := capturedFormValues["timestamp_granularities[]"]
		
		require.NotNil(t, granularities)
		require.Len(t, granularities, 1)
		
		// Should be the string "segment", not JSON ["segment"]
		assert.Equal(t, "segment", granularities[0])
		
		// Should NOT be a JSON array string
		assert.False(t, strings.HasPrefix(granularities[0], "["))
		assert.False(t, strings.HasPrefix(granularities[0], "\""))
		
		// Should not contain JSON array syntax
		assert.NotContains(t, granularities[0], "[")
		assert.NotContains(t, granularities[0], "]")
	})

	t.Run("sends multiple granularities as separate form fields", func(t *testing.T) {
		capturedFormValues = nil
		ctx := context.Background()
		_, err := transcriber.TranscribeFile(ctx, testFile, "", "verbose_json", []string{"segment", "word"})
		
		require.NoError(t, err)
		require.NotNil(t, capturedFormValues)

		// Check that both granularities are sent as separate form values
		granularities := capturedFormValues["timestamp_granularities[]"]
		
		require.NotNil(t, granularities)
		require.Len(t, granularities, 2)
		
		// Should be individual strings, not JSON array
		assert.Contains(t, granularities, "segment")
		assert.Contains(t, granularities, "word")
		
		// None should be JSON strings
		for _, gran := range granularities {
			assert.False(t, strings.HasPrefix(gran, "["))
			assert.False(t, strings.HasPrefix(gran, "\""))
		}
	})
}

func TestNewWhisperTranscriber(t *testing.T) {
	t.Run("creates transcriber with API key", func(t *testing.T) {
		originalKey := os.Getenv("OPENAI_API_KEY")
		os.Setenv("OPENAI_API_KEY", "test-key")
		defer func() {
			if originalKey != "" {
				os.Setenv("OPENAI_API_KEY", originalKey)
			} else {
				os.Unsetenv("OPENAI_API_KEY")
			}
		}()

		transcriber := NewWhisperTranscriber()
		assert.NotNil(t, transcriber)
		assert.True(t, transcriber.CanTranscribe())
		assert.Equal(t, "whisper-1", transcriber.model)
	})

	t.Run("creates transcriber without API key", func(t *testing.T) {
		originalKey := os.Getenv("OPENAI_API_KEY")
		os.Unsetenv("OPENAI_API_KEY")
		defer func() {
			if originalKey != "" {
				os.Setenv("OPENAI_API_KEY", originalKey)
			}
		}()

		transcriber := NewWhisperTranscriber()
		assert.NotNil(t, transcriber)
		assert.False(t, transcriber.CanTranscribe())
	})

	t.Run("uses custom model from env", func(t *testing.T) {
		originalKey := os.Getenv("OPENAI_API_KEY")
		originalModel := os.Getenv("WHISPER_MODEL")
		os.Setenv("OPENAI_API_KEY", "test-key")
		os.Setenv("WHISPER_MODEL", "whisper-2")
		defer func() {
			if originalKey != "" {
				os.Setenv("OPENAI_API_KEY", originalKey)
			} else {
				os.Unsetenv("OPENAI_API_KEY")
			}
			if originalModel != "" {
				os.Setenv("WHISPER_MODEL", originalModel)
			} else {
				os.Unsetenv("WHISPER_MODEL")
			}
		}()

		transcriber := NewWhisperTranscriber()
		assert.NotNil(t, transcriber)
		assert.Equal(t, "whisper-2", transcriber.model)
	})
}

func TestWhisperTranscriber_CanTranscribe(t *testing.T) {
	t.Run("returns true when API key is set", func(t *testing.T) {
		originalKey := os.Getenv("OPENAI_API_KEY")
		os.Setenv("OPENAI_API_KEY", "test-key")
		defer func() {
			if originalKey != "" {
				os.Setenv("OPENAI_API_KEY", originalKey)
			} else {
				os.Unsetenv("OPENAI_API_KEY")
			}
		}()

		transcriber := NewWhisperTranscriber()
		assert.True(t, transcriber.CanTranscribe())
	})

	t.Run("returns false when API key is not set", func(t *testing.T) {
		originalKey := os.Getenv("OPENAI_API_KEY")
		os.Unsetenv("OPENAI_API_KEY")
		defer func() {
			if originalKey != "" {
				os.Setenv("OPENAI_API_KEY", originalKey)
			}
		}()

		transcriber := NewWhisperTranscriber()
		assert.False(t, transcriber.CanTranscribe())
	})
}
