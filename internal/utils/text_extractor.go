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
	case ".docx", ".xlsx", ".xls", ".pptx":
		return DefaultOfficeExtractor.ExtractText(filePath)
	default:
		// For now, try to read as text
		return extractTextFile(filePath)
	}
}

// ExtractTextFromFileWithMeta extracts based on original filename/content-type,
// not the temporary downloaded file path extension.
//
// This is important for R2 presigned downloads where the temp filename may not
// end with the original extension (leading to "read binary as text").
func ExtractTextFromFileWithMeta(filePath string, originalFilename string, originalContentType string) (string, error) {
	ext := strings.ToLower(filepath.Ext(originalFilename))
	ct := strings.ToLower(strings.TrimSpace(originalContentType))

	// Prefer extension for deterministic routing.
	switch ext {
	case ".txt", ".md":
		return extractTextFile(filePath)
	case ".pdf":
		return extractPDF(filePath)
	case ".docx", ".xlsx", ".xls", ".pptx":
		// Do not delegate to DefaultOfficeExtractor here: that implementation
		// routes by filePath extension, which may be wrong for temp downloads.
		switch ext {
		case ".docx":
			return extractDocx(filePath)
		case ".xlsx":
			return extractXlsx(filePath)
		case ".xls":
			return extractXls(filePath)
		case ".pptx":
			return extractPptx(filePath)
		}
	}

	// Fallback to content-type heuristics when ext is missing/odd.
	if ct == "text/plain" || strings.HasPrefix(ct, "text/") {
		return extractTextFile(filePath)
	}
	if strings.HasPrefix(ct, "application/pdf") || strings.Contains(ct, "pdf") {
		return extractPDF(filePath)
	}
	if strings.Contains(ct, "wordprocessingml") || ext == ".doc" || ext == ".docx" {
		return extractDocx(filePath)
	}
	if ext == ".xls" || strings.Contains(ct, "vnd.ms-excel") || ct == "application/msexcel" {
		// Legacy binary Excel — distinct format from .xlsx; excelize can't
		// read it. Pure-Go BIFF reader.
		return extractXls(filePath)
	}
	if strings.Contains(ct, "spreadsheetml") || ext == ".xlsx" {
		return extractXlsx(filePath)
	}
	if strings.Contains(ct, "presentationml") || strings.Contains(ct, "powerpoint") || ext == ".ppt" || ext == ".pptx" {
		// Pure-Go parser supports .pptx content extraction.
		return extractPptx(filePath)
	}

	// Final fallback: try as plain text.
	return extractTextFile(filePath)
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
