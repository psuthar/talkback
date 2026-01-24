package utils

import "context"

// SpeechToText converts a short voice recording into text.
//
// This is intentionally small and swappable: voice is an input modality, not a domain model.
// Implementations should not persist raw audio long-term.
type SpeechToText interface {
	TranscribeAudio(ctx context.Context, inputFilePath string) (transcribedText string, confidence *float32, err error)
}

