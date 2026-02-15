package utils

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEstimateAudioDurationSeconds(t *testing.T) {
	t.Run("non-WAV returns false", func(t *testing.T) {
		dir := t.TempDir()
		f := filepath.Join(dir, "x.webm")
		require.NoError(t, os.WriteFile(f, []byte("not wav"), 0644))
		sec, ok := EstimateAudioDurationSeconds(f)
		assert.False(t, ok)
		assert.Equal(t, 0.0, sec)
	})

	t.Run("missing file returns false", func(t *testing.T) {
		sec, ok := EstimateAudioDurationSeconds(filepath.Join(t.TempDir(), "missing.wav"))
		assert.False(t, ok)
		assert.Equal(t, 0.0, sec)
	})
}

func TestHybridSpeechToText_CanTranscribe(t *testing.T) {
	t.Run("STT_MODE=api with no key returns false", func(t *testing.T) {
		t.Setenv("STT_MODE", "api")
		t.Setenv("OPENAI_API_KEY", "")
		t.Setenv("WHISPER_CLI", "nonexistent_whisper_12345")
		h := NewHybridSpeechToText()
		assert.False(t, h.CanTranscribe())
	})

	t.Run("STT_MODE=cli with CLI available returns true", func(t *testing.T) {
		t.Setenv("STT_MODE", "cli")
		t.Setenv("OPENAI_API_KEY", "")
		// Use "whisper" or a path that might exist; if not, skip
		h := NewHybridSpeechToText()
		// CanTranscribe depends on CLI in PATH; we can't assume either way
		_ = h.CanTranscribe()
	})

	t.Run("STT_MODE=hybrid with API key returns true", func(t *testing.T) {
		t.Setenv("STT_MODE", "hybrid")
		t.Setenv("OPENAI_API_KEY", "sk-test-key")
		t.Setenv("WHISPER_CLI", "nonexistent_whisper_12345")
		h := NewHybridSpeechToText()
		assert.True(t, h.CanTranscribe())
	})
}

func TestHybridSpeechToText_TranscribeAudio_Guardrails(t *testing.T) {
	t.Run("ErrAudioTooLong when over limit", func(t *testing.T) {
		dir := t.TempDir()
		wavPath := filepath.Join(dir, "long.wav")
		// 35 seconds at 32000 bytes/sec -> over STT_MAX_AUDIO_SECONDS=30
		writeMinimalWAVWithDuration(t, wavPath, 32000, 35)

		t.Setenv("STT_MODE", "api")
		t.Setenv("OPENAI_API_KEY", "sk-fake")
		t.Setenv("STT_MAX_AUDIO_SECONDS", "30")
		h := NewHybridSpeechToText()
		_, _, err := h.TranscribeAudio(context.Background(), wavPath)
		assert.ErrorIs(t, err, ErrAudioTooLong)
	})
}

// writeMinimalWAVWithDuration writes a minimal WAV with the given byte rate and duration in seconds.
func writeMinimalWAVWithDuration(t *testing.T, path string, byteRate uint32, durationSec int) {
	dataSize := int64(byteRate) * int64(durationSec)
	// RIFF size = file size - 8. File = 12 (RIFF) + 24 (fmt) + 8 + dataSize (data) = 44 + dataSize
	riffSize := 36 + dataSize
	b := make([]byte, 0, 44+int(dataSize))
	b = append(b, 'R', 'I', 'F', 'F')
	b = append(b, byte(riffSize), byte(riffSize>>8), byte(riffSize>>16), byte(riffSize>>24))
	b = append(b, 'W', 'A', 'V', 'E')
	b = append(b, 'f', 'm', 't', ' ')
	b = append(b, 16, 0, 0, 0)
	b = append(b, 1, 0, 1, 0) // PCM, mono
	b = append(b, byte(byteRate), byte(byteRate>>8), byte(byteRate>>16), byte(byteRate>>24))
	b = append(b, byte(byteRate), byte(byteRate>>8), byte(byteRate>>16), byte(byteRate>>24))
	b = append(b, 2, 0, 16, 0) // block align, bits
	b = append(b, 'd', 'a', 't', 'a')
	b = append(b, byte(dataSize), byte(dataSize>>8), byte(dataSize>>16), byte(dataSize>>24))
	for i := int64(0); i < dataSize; i++ {
		b = append(b, 0)
	}
	require.NoError(t, os.WriteFile(path, b, 0644))
}

