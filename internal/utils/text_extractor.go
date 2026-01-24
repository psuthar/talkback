package utils

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	pdf "github.com/ledongthuc/pdf"
)

func ExtractTextFromFile(filePath string) (string, error) {
	ext := strings.ToLower(filepath.Ext(filePath))

	switch ext {
	case ".txt", ".md":
		return extractTextFile(filePath)
	case ".pdf":
		return extractPDF(filePath)
	default:
		// For now, try to read as text
		return extractTextFile(filePath)
	}
}

func extractTextFile(filePath string) (string, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to read file: %w", err)
	}
	return string(data), nil
}

func extractPDF(filePath string) (string, error) {
	f, r, err := pdf.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to open pdf: %w", err)
	}
	defer f.Close()

	reader, err := r.GetPlainText()
	if err != nil {
		return "", fmt.Errorf("failed to extract pdf text: %w", err)
	}

	b, err := io.ReadAll(reader)
	if err != nil {
		return "", fmt.Errorf("failed to read extracted pdf text: %w", err)
	}

	text := strings.TrimSpace(string(b))
	if text == "" {
		return "", fmt.Errorf("pdf text extraction produced empty text")
	}

	return text, nil
}

func ChunkText(text string, chunkSize int, overlap int) []string {
	if chunkSize <= 0 {
		chunkSize = 1000 // default chunk size
	}
	if overlap < 0 {
		overlap = 200 // default overlap
	}

	if len(text) <= chunkSize {
		return []string{text}
	}

	var chunks []string
	start := 0

	for start < len(text) {
		end := start + chunkSize
		if end > len(text) {
			end = len(text)
		} else {
			// Try to break at sentence boundary
			lastPeriod := strings.LastIndex(text[start:end], ".")
			lastNewline := strings.LastIndex(text[start:end], "\n")

			if lastPeriod > chunkSize/2 {
				end = start + lastPeriod + 1
			} else if lastNewline > chunkSize/2 {
				end = start + lastNewline + 1
			}
		}

		chunks = append(chunks, strings.TrimSpace(text[start:end]))
		start = end - overlap
		if start >= len(text) {
			break
		}
	}

	return chunks
}

func SaveFile(content io.Reader, destPath string) error {
	// Create directory if it doesn't exist
	dir := filepath.Dir(destPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Create file
	file, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("failed to create file: %w", err)
	}
	defer file.Close()

	// Copy content
	_, err = io.Copy(file, content)
	if err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}
