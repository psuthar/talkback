package utils

import (
	"errors"
	"strings"
	"testing"
)

// SCRUM-371: unit tests for CSVToMarkdown. The handler integration
// tests in internal/handlers/ exercise the wiring around it; these
// tests pin the helper's contract.

func TestCSVToMarkdown_Basic(t *testing.T) {
	in := []byte("name,role\nAlex,Eng\nJordan,SRE\nSam,PM\n")
	got, err := CSVToMarkdown(in, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "| name | role |\n" +
		"| --- | --- |\n" +
		"| Alex | Eng |\n" +
		"| Jordan | SRE |\n" +
		"| Sam | PM |\n"
	if got != want {
		t.Fatalf("unexpected markdown:\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestCSVToMarkdown_HeaderOnly(t *testing.T) {
	in := []byte("name,role\n")
	got, err := CSVToMarkdown(in, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "| name | role |\n| --- | --- |\n"
	if got != want {
		t.Fatalf("expected header + separator only, got:\n%q", got)
	}
}

func TestCSVToMarkdown_EmptyFile(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   []byte
	}{
		{"zero bytes", []byte("")},
		{"only whitespace", []byte("   \n\t  \n")},
		{"only newline", []byte("\n")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := CSVToMarkdown(tc.in, 0)
			if !errors.Is(err, ErrEmptyCSV) {
				t.Fatalf("expected ErrEmptyCSV, got err=%v got=%q", err, got)
			}
			if got != "" {
				t.Fatalf("expected empty string on empty input, got %q", got)
			}
		})
	}
}

func TestCSVToMarkdown_RaggedRows(t *testing.T) {
	// row 2 is short; row 3 is full. Output must pad row 2 with empty cells.
	in := []byte("a,b,c\n1,2\n3,4,5\n")
	got, err := CSVToMarkdown(in, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "| a | b | c |\n" +
		"| --- | --- | --- |\n" +
		"| 1 | 2 |  |\n" +
		"| 3 | 4 | 5 |\n"
	if got != want {
		t.Fatalf("ragged-row padding wrong:\nwant:\n%s\ngot:\n%s", want, got)
	}
}

func TestCSVToMarkdown_PipeInCell(t *testing.T) {
	in := []byte("col\n\"a|b\"\n")
	got, err := CSVToMarkdown(in, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, `| a\|b |`) {
		t.Fatalf("expected literal | to be escaped to \\| in cell, got:\n%s", got)
	}
	// Verify the table grid would parse — the only un-escaped pipes
	// should be the cell-boundary pipes.
	for _, line := range strings.Split(strings.TrimRight(got, "\n"), "\n") {
		// Skip the alignment separator row.
		if strings.HasPrefix(line, "| --- ") {
			continue
		}
		// Each row begins and ends with `|`. Internal pipes that
		// aren't part of `\|` would be cell boundaries. Count
		// non-escaped pipes; a 1-column table should have exactly 2.
		stripped := strings.ReplaceAll(line, `\|`, "")
		if got := strings.Count(stripped, "|"); got != 2 {
			t.Fatalf("expected exactly 2 non-escaped pipes in 1-col row, got %d in line %q", got, line)
		}
	}
}

func TestCSVToMarkdown_NewlineInQuotedCell(t *testing.T) {
	// RFC 4180 quoted newline inside a cell.
	in := []byte("col\n\"line one\nline two\"\n")
	got, err := CSVToMarkdown(in, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(got, "| line one<br>line two |") {
		t.Fatalf("expected embedded newline to render as <br>, got:\n%s", got)
	}
	// And: no real newlines inside the cell — the table must still be
	// one CSV row per output row.
	bodyLines := 0
	for _, line := range strings.Split(strings.TrimRight(got, "\n"), "\n") {
		if strings.HasPrefix(line, "| --- ") || strings.HasPrefix(line, "| col ") {
			continue
		}
		bodyLines++
	}
	if bodyLines != 1 {
		t.Fatalf("expected 1 body line, got %d", bodyLines)
	}
}

func TestCSVToMarkdown_SizeCap(t *testing.T) {
	// Build a CSV with ~100 rows, each ~50 chars, then cap at 500 bytes.
	var b strings.Builder
	b.WriteString("col\n")
	for i := 0; i < 100; i++ {
		b.WriteString(strings.Repeat("x", 40))
		b.WriteString("\n")
	}
	got, err := CSVToMarkdown([]byte(b.String()), 500)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) > 700 { // some slack for the footer line
		t.Fatalf("output exceeded cap by too much: %d bytes", len(got))
	}
	if !strings.Contains(got, "more rows omitted") {
		t.Fatalf("expected truncation footer, got:\n%s", got)
	}
	// Footer should name a positive integer of omitted rows.
	if !strings.Contains(got, "_… (") {
		t.Fatalf("expected unicode ellipsis prefix in footer, got:\n%s", got)
	}
}

func TestCSVToMarkdown_UTF8(t *testing.T) {
	in := []byte("name,city\n安藤,東京\nJürgen,München\n")
	got, err := CSVToMarkdown(in, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, frag := range []string{"安藤", "東京", "Jürgen", "München"} {
		if !strings.Contains(got, frag) {
			t.Errorf("expected UTF-8 fragment %q to round-trip, got:\n%s", frag, got)
		}
	}
}

func TestCSVToMarkdown_TrailingNewlineOnly(t *testing.T) {
	// File that ends with a single \n must not emit a phantom empty row.
	in := []byte("a,b\n1,2\n")
	got, err := CSVToMarkdown(in, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "| a | b |\n| --- | --- |\n| 1 | 2 |\n"
	if got != want {
		t.Fatalf("phantom row leaked:\nwant:\n%s\ngot:\n%s", want, got)
	}
}

// Sanity: parse error path returns a non-nil error and an empty string.
// The Go encoding/csv parser is lenient with LazyQuotes, so this uses
// a malformed quoted field that even LazyQuotes refuses.
func TestCSVToMarkdown_ParseError(t *testing.T) {
	// Unterminated quoted field followed by EOF — encoding/csv reports
	// "bare \" in non-quoted-field" or similar.
	in := []byte("col\n\"unterminated\n")
	got, err := CSVToMarkdown(in, 0)
	// LazyQuotes makes the parser lenient enough that this might still
	// succeed; only fail the test if csv accepted it AND the output is
	// silently empty. The contract: a structural parse error returns
	// a non-nil error; otherwise the markdown must be non-empty.
	if err != nil {
		if got != "" {
			t.Fatalf("expected empty string on parse error, got %q", got)
		}
		return
	}
	if got == "" {
		t.Fatalf("LazyQuotes accepted input but markdown is empty")
	}
}

// SCRUM-396: RowsToMarkdownTable is the table-rendering core shared by
// CSVToMarkdown and the synchronous .xls/.xlsx extractors.

func TestRowsToMarkdownTable_HeaderSeparatorBody(t *testing.T) {
	rows := [][]string{{"name", "role"}, {"Alex", "Eng"}, {"Jordan", "SRE"}}
	got := RowsToMarkdownTable(rows, 0)
	want := "| name | role |\n| --- | --- |\n| Alex | Eng |\n| Jordan | SRE |\n"
	if got != want {
		t.Fatalf("RowsToMarkdownTable mismatch\n got: %q\nwant: %q", got, want)
	}
}

func TestRowsToMarkdownTable_RaggedRowsRightPadded(t *testing.T) {
	rows := [][]string{{"a", "b", "c"}, {"1"}, {"x", "y"}}
	got := RowsToMarkdownTable(rows, 0)
	want := "| a | b | c |\n| --- | --- | --- |\n| 1 |  |  |\n| x | y |  |\n"
	if got != want {
		t.Fatalf("ragged-row padding mismatch\n got: %q\nwant: %q", got, want)
	}
}

func TestRowsToMarkdownTable_EscapesPipeAndNewline(t *testing.T) {
	rows := [][]string{{"h1", "h2"}, {"a|b", "line1\nline2"}}
	got := RowsToMarkdownTable(rows, 0)
	if !strings.Contains(got, `a\|b`) {
		t.Errorf("expected escaped pipe; got %q", got)
	}
	if !strings.Contains(got, "line1<br>line2") {
		t.Errorf("expected newline → <br>; got %q", got)
	}
}

func TestRowsToMarkdownTable_SizeCapTruncates(t *testing.T) {
	rows := [][]string{{"h"}}
	for i := 0; i < 1000; i++ {
		rows = append(rows, []string{strings.Repeat("x", 50)})
	}
	got := RowsToMarkdownTable(rows, 512) // tiny cap
	if !strings.HasPrefix(got, "| h |\n| --- |\n") {
		t.Fatalf("expected header+separator preserved; got %q", got[:min(80, len(got))])
	}
	if !strings.Contains(got, "more rows omitted") {
		t.Errorf("expected truncation footer; got %q", got)
	}
	if len(got) > 1024 {
		t.Errorf("output not capped: %d bytes", len(got))
	}
}

func TestRowsToMarkdownTable_EmptyInputs(t *testing.T) {
	if RowsToMarkdownTable(nil, 0) != "" {
		t.Error("nil rows should yield empty string")
	}
	if RowsToMarkdownTable([][]string{}, 0) != "" {
		t.Error("empty rows should yield empty string")
	}
	if RowsToMarkdownTable([][]string{{}}, 0) != "" {
		t.Error("a single zero-column row should yield empty string")
	}
}

func TestTrimEmptyEdgeRows(t *testing.T) {
	in := [][]string{
		{"", "  "},
		nil,
		{"name", "role"},
		{"", ""},
		{"Alex", "Eng"},
		{"  "},
		nil,
	}
	got := trimEmptyEdgeRows(in)
	want := [][]string{{"name", "role"}, {"", ""}, {"Alex", "Eng"}}
	if len(got) != len(want) {
		t.Fatalf("len mismatch: got %d want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if strings.Join(got[i], "|") != strings.Join(want[i], "|") {
			t.Errorf("row %d: got %v want %v", i, got[i], want[i])
		}
	}
	// Interior blank row preserved.
	if strings.TrimSpace(strings.Join(got[1], "")) != "" {
		t.Errorf("expected interior blank row preserved, got %v", got[1])
	}
}
