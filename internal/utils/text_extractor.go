package utils

import (
	"fmt"
	"io"
	"os"
	"os/exec"
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
	case ".docx", ".xlsx", ".pptx":
		return DefaultOfficeExtractor.ExtractText(filePath)
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
	// Prefer pdftotext (poppler-utils) when available - it works reliably on Linux.
	// ledongthuc/pdf often returns raw PDF structure on Linux instead of extracted text.
	if text, err := extractPDFWithPdftotext(filePath); err == nil && strings.TrimSpace(text) != "" {
		if !looksLikeRawPDF(text) {
			return text, nil
		}
	}
	// Fall back to Go library (works on Windows; may fail on Linux)
	pages, err := ExtractPDFPages(filePath)
	if err != nil {
		return "", err
	}
	text := strings.Join(pages, "\n\n")
	if looksLikeRawPDF(text) {
		return "", fmt.Errorf("pdf text extraction produced raw PDF structure instead of text")
	}
	return text, nil
}

// looksLikeRawPDF returns true if the text looks like raw PDF structure (e.g. %PDF-1.4, obj/endobj).
func looksLikeRawPDF(text string) bool {
	t := strings.TrimSpace(text)
	if len(t) < 20 {
		return false
	}
	if strings.HasPrefix(t, "%PDF") {
		return true
	}
	if strings.Contains(t, " endobj ") || strings.Contains(t, " 0 obj ") {
		return true
	}
	return false
}

// extractPDFWithPdftotext runs pdftotext (poppler-utils) if available. Returns empty string on exec error.
func extractPDFWithPdftotext(filePath string) (string, error) {
	cmd := exec.Command("pdftotext", "-layout", "-enc", "UTF-8", filePath, "-")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}

// ExtractPDFPages returns text per page; out[i] is the text for page i+1 (1-based). Empty pages are "".
func ExtractPDFPages(filePath string) ([]string, error) {
	f, r, err := pdf.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open pdf: %w", err)
	}
	defer f.Close()

	numPage := r.NumPage()
	out := make([]string, numPage)
	hasAny := false
	for i := 1; i <= numPage; i++ {
		p := r.Page(i)
		if p.V.IsNull() {
			continue
		}
		text, err := p.GetPlainText(nil)
		if err != nil {
			continue
		}
		t := strings.TrimSpace(text)
		out[i-1] = t
		if t != "" {
			hasAny = true
		}
	}
	if !hasAny {
		return nil, fmt.Errorf("pdf text extraction produced no text")
	}
	return out, nil
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
