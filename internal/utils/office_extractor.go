package utils

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/extrame/xls"
	"github.com/xuri/excelize/v2"
)

// OfficeExtractor extracts plain text from Office documents (docx, xlsx, pptx).
// Implementations can be swapped (e.g. pure-Go vs LibreOffice-based) without changing callers.
type OfficeExtractor interface {
	ExtractText(filePath string) (string, error)
}

// DefaultOfficeExtractor is the default pure-Go implementation.
var DefaultOfficeExtractor OfficeExtractor = defaultOfficeExtractor{}

type defaultOfficeExtractor struct{}

func (defaultOfficeExtractor) ExtractText(filePath string) (string, error) {
	ext := strings.ToLower(filepath.Ext(filePath))
	switch ext {
	case ".docx":
		return extractDocx(filePath)
	case ".xlsx":
		return extractXlsx(filePath)
	case ".xls":
		return extractXls(filePath)
	case ".pptx":
		return extractPptx(filePath)
	default:
		return "", fmt.Errorf("unsupported office extension: %s", ext)
	}
}

// extractDocx reads a .docx (ZIP) and extracts text from word/document.xml,
// grouping by paragraph (w:p) so output is readable: paragraphs separated by "\n\n",
// text runs within a paragraph joined with no separator (preserves in-document spacing).
func extractDocx(filePath string) (string, error) {
	r, err := zip.OpenReader(filePath)
	if err != nil {
		return "", fmt.Errorf("open docx: %w", err)
	}
	defer r.Close()

	var docReader io.ReadCloser
	for _, f := range r.File {
		if f.Name == "word/document.xml" {
			docReader, err = f.Open()
			if err != nil {
				return "", fmt.Errorf("open document.xml: %w", err)
			}
			break
		}
	}
	if docReader == nil {
		return "", fmt.Errorf("word/document.xml not found in docx")
	}
	defer docReader.Close()

	var paragraphs []string
	var inParagraph bool
	var currentRuns []string

	dec := xml.NewDecoder(docReader)
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", fmt.Errorf("decode document.xml: %w", err)
		}
		switch v := tok.(type) {
		case xml.StartElement:
			if v.Name.Local == "p" {
				inParagraph = true
				currentRuns = nil
			} else if v.Name.Local == "t" && inParagraph {
				next, err := dec.Token()
				if err != nil {
					continue
				}
				if cd, ok := next.(xml.CharData); ok {
					currentRuns = append(currentRuns, string(cd))
				}
			}
		case xml.EndElement:
			if v.Name.Local == "p" {
				if inParagraph && len(currentRuns) > 0 {
					para := strings.TrimSpace(strings.Join(currentRuns, ""))
					if para != "" {
						paragraphs = append(paragraphs, para)
					}
				}
				inParagraph = false
			}
		}
	}

	if len(paragraphs) == 0 {
		return "", fmt.Errorf("docx text extraction produced no text")
	}
	return strings.Join(paragraphs, "\n\n"), nil
}

// extractXlsx reads a .xlsx and renders each sheet as a GitHub-flavored
// markdown table (SCRUM-396), so the SpreadsheetViewer displays a real table
// instead of a run-on paragraph. A workbook with a single non-empty sheet
// yields just the table; with two or more, each table is preceded by a
// `## SheetName` heading (the shape SpreadsheetViewer already styles for
// multi-sheet workbooks). The first row of each sheet is treated as the
// header. Sheets with no data are skipped.
func extractXlsx(filePath string) (string, error) {
	f, err := excelize.OpenFile(filePath)
	if err != nil {
		return "", fmt.Errorf("open xlsx: %w", err)
	}
	defer f.Close()

	type sheetTable struct {
		name  string
		table string
	}
	var tables []sheetTable
	for _, name := range f.GetSheetList() {
		rows, err := f.GetRows(name)
		if err != nil {
			continue
		}
		table := RowsToMarkdownTable(trimEmptyEdgeRows(rows), DefaultCSVMarkdownMaxBytes)
		if strings.TrimSpace(table) == "" {
			continue
		}
		tables = append(tables, sheetTable{name: name, table: table})
	}
	if len(tables) == 0 {
		return "", fmt.Errorf("xlsx text extraction produced no text")
	}

	if len(tables) == 1 {
		return tables[0].table, nil
	}
	var sections []string
	for _, t := range tables {
		sections = append(sections, "## "+t.name+"\n\n"+t.table)
	}
	return strings.Join(sections, "\n\n"), nil
}

// maxXlsRows caps how many rows extractXls pulls into memory across all
// sheets — a guard against a maliciously huge legacy workbook. (.xls is
// a 16-bit format, so a single sheet tops out at 65 536 rows; this still
// caps multi-sheet workbooks.)
const maxXlsRows = 200_000

// extractXls reads a legacy binary .xls workbook (BIFF / OLE2 compound
// document, MIME application/vnd.ms-excel) and renders it as a GitHub-flavored
// markdown table (SCRUM-396) — the same shape extractXlsx and CSVToMarkdown
// produce, so the SpreadsheetViewer shows a table. Uses github.com/extrame/xls
// (pure Go — no markitdown sidecar dependency, mirroring the .csv/.xlsx
// synchronous paths). The first row is treated as the header.
//
// Multi-sheet limitation: extrame/xls's safe bulk read (WorkBook.ReadAllCells)
// flattens every sheet into one row stream — there is no panic-safe per-sheet
// row API (WorkSheet.Row(i) dereferences a nil entry for unused indices). So a
// multi-sheet .xls renders as a single table with later sheets appended below
// the first (their header rows appear as body rows). Multi-sheet .xls is rare;
// proper `## SheetName` sectioning for it is a possible follow-up.
//
// Wrapped in a recover because the third-party BIFF parser can panic on
// malformed input and this runs on arbitrary user uploads.
func extractXls(filePath string) (text string, err error) {
	defer func() {
		if r := recover(); r != nil {
			text = ""
			err = fmt.Errorf("xls parse panicked: %v", r)
		}
	}()

	wb, openErr := xls.Open(filePath, "utf-8")
	if openErr != nil {
		return "", fmt.Errorf("open xls: %w", openErr)
	}
	if wb == nil {
		// xls.OpenReader returns (nil, nil) when the OLE container has no
		// Workbook/Book stream — i.e. the bytes aren't a real .xls.
		return "", fmt.Errorf("open xls: not a valid .xls workbook")
	}

	rows := trimEmptyEdgeRows(wb.ReadAllCells(maxXlsRows))
	md := RowsToMarkdownTable(rows, DefaultCSVMarkdownMaxBytes)
	if strings.TrimSpace(md) == "" {
		return "", fmt.Errorf("xls text extraction produced no text")
	}
	return md, nil
}

// ExtractPptxTextPerSlide returns text for each slide in order (slide 1, 2, 3, ...).
// Used by RAG to create per-slide chunks so citations can navigate to the correct slide.
func ExtractPptxTextPerSlide(filePath string) ([]string, error) {
	r, err := zip.OpenReader(filePath)
	if err != nil {
		return nil, fmt.Errorf("open pptx: %w", err)
	}
	defer r.Close()

	slidePrefix := "ppt/slides/slide"
	type slideEntry struct {
		num  int
		file *zip.File
	}
	var entries []slideEntry
	for _, f := range r.File {
		if !strings.HasPrefix(f.Name, slidePrefix) || !strings.HasSuffix(f.Name, ".xml") {
			continue
		}
		base := strings.TrimPrefix(f.Name, slidePrefix)
		base = strings.TrimSuffix(base, ".xml")
		n, _ := strconv.Atoi(base)
		if n < 1 {
			continue
		}
		entries = append(entries, slideEntry{num: n, file: f})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].num < entries[j].num })

	var out []string
	for _, e := range entries {
		rc, err := e.file.Open()
		if err != nil {
			continue
		}
		parts := extractPptxSlideText(rc)
		rc.Close()
		var slideText []string
		for _, p := range parts {
			if strings.TrimSpace(p) != "" {
				slideText = append(slideText, strings.TrimSpace(p))
			}
		}
		out = append(out, strings.Join(slideText, " "))
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("pptx text extraction produced no text")
	}
	return out, nil
}

// extractPptx reads a .pptx (ZIP) and extracts text from ppt/slides/slideN.xml (a:t elements).
func extractPptx(filePath string) (string, error) {
	slides, err := ExtractPptxTextPerSlide(filePath)
	if err != nil {
		return "", err
	}
	return strings.Join(slides, "\n"), nil
}

func extractPptxSlideText(r io.Reader) []string {
	var parts []string
	dec := xml.NewDecoder(r)
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			break
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		// DrawingML text: a:t (often in namespace ending with drawingml)
		if se.Name.Local != "t" {
			continue
		}
		next, err := dec.Token()
		if err != nil {
			continue
		}
		if cd, ok := next.(xml.CharData); ok {
			parts = append(parts, string(cd))
		}
	}
	return parts
}
