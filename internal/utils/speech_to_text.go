package utils

import "context"

// SpeechToText converts a short voice recording into text.
//
// This is intentionally small and swappable: voice is an input modality, not a domain model.
// Implementations should not persist raw audio long-term.
type SpeechToText interface {
	CanTranscribe() bool
	TranscribeAudio(ctx context.Context, inputFilePath string) (transcribedText string, confidence *float32, err error)
}

// NewSpeechToTextFromEnv returns the configured STT implementation (hybrid: API-first with CLI fallback).
// Use for UI mic endpoints. When OPENAI_API_KEY is set, prefers API; otherwise or on retryable failure uses Whisper CLI.
func NewSpeechToTextFromEnv() SpeechToText {
	return NewHybridSpeechToText()
}
