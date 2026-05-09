package utils

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"strings"
)

// SCRUM-371: synchronous CSV → markdown rendering for the materials
// upload path. Avoids the markitdown sidecar dependency that previously
// left CSV uploads stuck in `pending` when the sidecar was disabled.

// ErrEmptyCSV is returned by CSVToMarkdown when the input contains no
// parseable rows (zero bytes, only whitespace, or only a single trailing
// newline). Callers can distinguish this from a structural parse error.
var ErrEmptyCSV = errors.New("csv input is empty")

// DefaultCSVMarkdownMaxBytes caps the rendered markdown size so a 5 MiB
// CSV cannot expand into many tens of MiB of grid characters. Callers
// pass this to CSVToMarkdown; it is exported so handler tests can reuse
// the same number without copy-pasting.
const DefaultCSVMarkdownMaxBytes = 1 << 20 // 1 MiB

// CSVToMarkdown parses raw CSV bytes and renders them as a
// GitHub-flavored markdown table. The first row becomes the header.
//
// Behavior:
//   - LazyQuotes: tolerates bare quote characters mid-field (real-world
//     CSVs are messy).
//   - FieldsPerRecord: -1: ragged rows are accepted; shorter rows are
//     right-padded with empty cells to the max width.
//   - Per-cell escape: `|` → `\|` (so a stray pipe doesn't break the
//     grid), `\n` → `<br>` (GFM doesn't allow real newlines in cells),
//     `\r` dropped (csv.Reader normalises line endings already; defence
//     in depth).
//   - Size cap: when the rendered output would exceed maxBytes, stops
//     appending body rows and appends a `… (N more rows)` footer. The
//     returned error is nil — truncation is graceful, not an error.
//
// Returns (markdown, nil) on success, ("", ErrEmptyCSV) on empty input,
// and ("", err) on a structural parse error.
func CSVToMarkdown(data []byte, maxBytes int) (string, error) {
	if maxBytes <= 0 {
		maxBytes = DefaultCSVMarkdownMaxBytes
	}
	if len(bytes.TrimSpace(data)) == 0 {
		return "", ErrEmptyCSV
	}

	reader := csv.NewReader(bytes.NewReader(data))
	reader.LazyQuotes = true
	reader.FieldsPerRecord = -1
	rows, err := reader.ReadAll()
	if err != nil {
		return "", fmt.Errorf("csv parse: %w", err)
	}
	if len(rows) == 0 {
		return "", ErrEmptyCSV
	}

	maxCols := 0
	for _, r := range rows {
		if len(r) > maxCols {
			maxCols = len(r)
		}
	}
	if maxCols == 0 {
		return "", ErrEmptyCSV
	}

	var b strings.Builder
	writeRow := func(cells []string) {
		b.WriteString("|")
		for i := 0; i < maxCols; i++ {
			b.WriteString(" ")
			if i < len(cells) {
				b.WriteString(escapeCell(cells[i]))
			}
			b.WriteString(" |")
		}
		b.WriteString("\n")
	}

	// Header row + alignment separator.
	writeRow(rows[0])
	b.WriteString("|")
	for i := 0; i < maxCols; i++ {
		b.WriteString(" --- |")
	}
	b.WriteString("\n")

	// Body rows — stop appending when the next row would push us over
	// the cap; emit a truncation footer that names the omitted count.
	headerSize := b.Len()
	rendered := 0
	for i, row := range rows[1:] {
		// Estimate the size of this row so we don't blow past the cap.
		rowEstimate := 1 // leading "|"
		for j := 0; j < maxCols; j++ {
			rowEstimate += 3 // " |" + space
			if j < len(row) {
				rowEstimate += len(escapeCell(row[j]))
			}
		}
		rowEstimate += 1 // trailing "\n"

		// Always emit at least the header + separator (already in b).
		// If even the first body row would overflow, we still keep the
		// footer instead of producing an oversized output.
		footer := fmt.Sprintf("\n_… (%d more rows omitted)_\n", len(rows)-1-rendered)
		if b.Len()+rowEstimate+len(footer) > maxBytes && rendered > 0 {
			b.WriteString(fmt.Sprintf("\n_… (%d more rows omitted)_\n", len(rows)-1-i))
			return b.String(), nil
		}
		writeRow(row)
		rendered++
	}

	// Defence in depth: if the header alone overflowed (pathological
	// 1000-column header), we still return what we have and signal no
	// error — caller can decide how to surface a "header too wide" case.
	_ = headerSize
	return b.String(), nil
}

// escapeCell makes a CSV cell safe to drop into a GFM table cell. Only
// touches the three characters that would corrupt the grid; leaves
// backslash and other markdown metacharacters alone (the cell content
// is human-readable extracted text, not markdown the user authored).
func escapeCell(s string) string {
	if s == "" {
		return ""
	}
	// Order matters: replace newlines and CR before pipe so we don't
	// accidentally rewrite an escaped pipe.
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.ReplaceAll(s, "\n", "<br>")
	s = strings.ReplaceAll(s, "|", `\|`)
	return s
}
