package utils

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"strings"
)

// SCRUM-371: synchronous CSV → markdown rendering for the materials upload
// path. Avoids the markitdown sidecar dependency that previously left CSV
// uploads stuck in `pending` when the sidecar was disabled.
//
// SCRUM-396: the table-rendering core was lifted out into RowsToMarkdownTable
// so the synchronous .xls / .xlsx extractors emit the same GitHub-flavored
// markdown table the SpreadsheetViewer expects, rather than tab-joined plain
// text (which react-markdown renders as one run-on paragraph).

// ErrEmptyCSV is returned by CSVToMarkdown when the input contains no
// parseable rows (zero bytes, only whitespace, or only a single trailing
// newline). Callers can distinguish this from a structural parse error.
var ErrEmptyCSV = errors.New("csv input is empty")

// DefaultCSVMarkdownMaxBytes caps the rendered markdown size so a 5 MiB
// CSV cannot expand into many tens of MiB of grid characters. Callers
// pass this to CSVToMarkdown; it is exported so handler tests can reuse
// the same number without copy-pasting.
const DefaultCSVMarkdownMaxBytes = 1 << 20 // 1 MiB

// CSVToMarkdown parses raw CSV bytes and renders them as a GitHub-flavored
// markdown table via RowsToMarkdownTable. The first row becomes the header.
//
// CSV-specific behavior:
//   - LazyQuotes: tolerates bare quote characters mid-field (real-world
//     CSVs are messy).
//   - FieldsPerRecord: -1: ragged rows are accepted (RowsToMarkdownTable
//     right-pads shorter rows to the widest row).
//
// Returns (markdown, nil) on success, ("", ErrEmptyCSV) on empty input,
// and ("", err) on a structural parse error. Truncation when the output
// would exceed maxBytes is graceful (a `… (N more rows omitted)` footer),
// not an error — see RowsToMarkdownTable.
func CSVToMarkdown(data []byte, maxBytes int) (string, error) {
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

	md := RowsToMarkdownTable(rows, maxBytes)
	if md == "" {
		return "", ErrEmptyCSV
	}
	return md, nil
}

// RowsToMarkdownTable renders [][]string rows as a GitHub-flavored markdown
// table: the first row is the header, followed by an alignment separator
// (`| --- | --- | …`) and the body rows. The table width is the widest row;
// shorter rows are right-padded with empty cells. Each cell is escaped so a
// stray `|` or newline can't corrupt the grid (see escapeCell). When the
// rendered output would exceed maxBytes, body rows stop being appended and a
// `_… (N more rows omitted)_` footer is added — truncation is graceful, not an
// error. Returns "" when rows is empty or has zero columns; the caller decides
// how to surface that (CSVToMarkdown maps it to ErrEmptyCSV; the office
// extractors treat it as "this sheet produced no text").
func RowsToMarkdownTable(rows [][]string, maxBytes int) string {
	if maxBytes <= 0 {
		maxBytes = DefaultCSVMarkdownMaxBytes
	}
	if len(rows) == 0 {
		return ""
	}

	maxCols := 0
	for _, r := range rows {
		if len(r) > maxCols {
			maxCols = len(r)
		}
	}
	if maxCols == 0 {
		return ""
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

	// Body rows — stop appending when the next row would push us over the cap;
	// emit a truncation footer that names the omitted count.
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
		rowEstimate++ // trailing "\n"

		// Always emit at least the header + separator (already in b). If even
		// the first body row would overflow, we still keep the footer instead
		// of producing an oversized output.
		footer := fmt.Sprintf("\n_… (%d more rows omitted)_\n", len(rows)-1-rendered)
		if b.Len()+rowEstimate+len(footer) > maxBytes && rendered > 0 {
			b.WriteString(fmt.Sprintf("\n_… (%d more rows omitted)_\n", len(rows)-1-i))
			return b.String()
		}
		writeRow(row)
		rendered++
	}
	return b.String()
}

// trimEmptyEdgeRows drops leading and trailing rows whose every cell is blank
// (after trimming whitespace). Spreadsheet readers commonly return such rows —
// excelize.GetRows includes empty rows before the first cell with data, and
// extrame/xls's ReadAllCells sizes its result by MaxRow and leaves nil rows
// for unused indices. Trimming them keeps the first real row as the table
// header rather than an all-empty header. Interior blank rows are preserved
// (they're a real empty row in the sheet).
func trimEmptyEdgeRows(rows [][]string) [][]string {
	rowBlank := func(r []string) bool {
		for _, c := range r {
			if strings.TrimSpace(c) != "" {
				return false
			}
		}
		return true
	}
	i := 0
	for i < len(rows) && rowBlank(rows[i]) {
		i++
	}
	j := len(rows)
	for j > i && rowBlank(rows[j-1]) {
		j--
	}
	return rows[i:j]
}

// escapeCell makes a cell value safe to drop into a GFM table cell. Only
// touches the three characters that would corrupt the grid; leaves backslash
// and other markdown metacharacters alone (the cell content is human-readable
// extracted text, not markdown the user authored).
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
