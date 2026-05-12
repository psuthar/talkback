package utils

import (
	"archive/zip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestDefaultOfficeExtractor_UnsupportedExtension(t *testing.T) {
	tmp := t.TempDir()
	f := filepath.Join(tmp, "x.doc")
	if err := os.WriteFile(f, []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := DefaultOfficeExtractor.ExtractText(f)
	if err == nil {
		t.Fatal("expected error for .doc")
	}
	if !strings.Contains(err.Error(), "unsupported") {
		t.Errorf("error should mention unsupported: %v", err)
	}
}

func TestExtractDocx(t *testing.T) {
	tmp := t.TempDir()
	docxPath := filepath.Join(tmp, "minimal.docx")

	// Minimal .docx: zip with word/document.xml containing w:t text
	w, err := os.Create(docxPath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(w)
	// Word document.xml with namespace; our parser only checks Local name "t"
	docXML := `<?xml version="1.0"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:body>
    <w:p><w:r><w:t>Hello DOCX</w:t></w:r></w:p>
    <w:p><w:r><w:t>Second line</w:t></w:r></w:p>
  </w:body>
</w:document>`
	fw, err := zw.Create("word/document.xml")
	if err != nil {
		w.Close()
		t.Fatal(err)
	}
	if _, err := fw.Write([]byte(docXML)); err != nil {
		zw.Close()
		w.Close()
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		w.Close()
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	text, err := extractDocx(docxPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "Hello DOCX") {
		t.Errorf("expected Hello DOCX in output: %q", text)
	}
	if !strings.Contains(text, "Second line") {
		t.Errorf("expected Second line in output: %q", text)
	}
}

func TestExtractXlsx(t *testing.T) {
	tmp := t.TempDir()
	xlsxPath := filepath.Join(tmp, "minimal.xlsx")

	f := excelize.NewFile()
	if err := f.SetSheetRow("Sheet1", "A1", &[]any{"name", "role"}); err != nil {
		t.Fatal(err)
	}
	if err := f.SetSheetRow("Sheet1", "A2", &[]any{"Alex Chen", "Senior Staff Engineer"}); err != nil {
		t.Fatal(err)
	}
	if err := f.SaveAs(xlsxPath); err != nil {
		t.Fatal(err)
	}
	f.Close()

	text, err := extractXlsx(xlsxPath)
	if err != nil {
		t.Fatal(err)
	}
	// SCRUM-396: a single-sheet workbook renders as one markdown table
	// (no `## SheetName` heading) — header, separator, body.
	if !strings.HasPrefix(text, "| name | role |\n| --- | --- |\n") {
		t.Errorf("expected a markdown table header+separator; got %q", text)
	}
	if !strings.Contains(text, "| Alex Chen | Senior Staff Engineer |") {
		t.Errorf("expected a markdown table body row; got %q", text)
	}
	if strings.Contains(text, "## ") {
		t.Errorf("single-sheet workbook must not get a sheet heading; got %q", text)
	}
}

// SCRUM-396: a multi-sheet .xlsx renders one `## SheetName` heading + table
// per non-empty sheet (the shape SpreadsheetViewer styles).
func TestExtractXlsx_MultiSheet(t *testing.T) {
	tmp := t.TempDir()
	xlsxPath := filepath.Join(tmp, "multi.xlsx")

	f := excelize.NewFile() // creates "Sheet1"
	if err := f.SetSheetRow("Sheet1", "A1", &[]any{"name", "role"}); err != nil {
		t.Fatal(err)
	}
	if err := f.SetSheetRow("Sheet1", "A2", &[]any{"Alex", "Eng"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.NewSheet("Notes"); err != nil {
		t.Fatal(err)
	}
	if err := f.SetSheetRow("Notes", "A1", &[]any{"topic", "summary"}); err != nil {
		t.Fatal(err)
	}
	if err := f.SetSheetRow("Notes", "A2", &[]any{"scope", "platform"}); err != nil {
		t.Fatal(err)
	}
	if err := f.SaveAs(xlsxPath); err != nil {
		t.Fatal(err)
	}
	f.Close()

	text, err := extractXlsx(xlsxPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"## Sheet1", "## Notes",
		"| name | role |", "| --- | --- |", "| Alex | Eng |",
		"| topic | summary |", "| scope | platform |",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("multi-sheet xlsx output missing %q\n--- got ---\n%s", want, text)
		}
	}
	if strings.Index(text, "## Sheet1") > strings.Index(text, "## Notes") {
		t.Errorf("sheets out of order; got %q", text)
	}
}

func TestExtractPptx(t *testing.T) {
	tmp := t.TempDir()
	pptxPath := filepath.Join(tmp, "minimal.pptx")

	w, err := os.Create(pptxPath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(w)
	slideXML := `<?xml version="1.0"?>
<sld xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main">
  <cSld><spTree><sp><txBody><a:p><a:r><a:t>Hello PPTX</a:t></a:r></a:p></txBody></sp></spTree></cSld>
</sld>`
	fw, err := zw.Create("ppt/slides/slide1.xml")
	if err != nil {
		w.Close()
		t.Fatal(err)
	}
	if _, err := fw.Write([]byte(slideXML)); err != nil {
		zw.Close()
		w.Close()
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		w.Close()
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	text, err := extractPptx(pptxPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "Hello PPTX") {
		t.Errorf("expected Hello PPTX in output: %q", text)
	}
}

func TestDefaultOfficeExtractor_ExtractText_Docx(t *testing.T) {
	tmp := t.TempDir()
	docxPath := filepath.Join(tmp, "test.docx")
	w, err := os.Create(docxPath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(w)
	docXML := `<?xml version="1.0"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>Office text</w:t></w:r></w:p></w:body></w:document>`
	fw, _ := zw.Create("word/document.xml")
	fw.Write([]byte(docXML))
	zw.Close()
	w.Close()

	text, err := DefaultOfficeExtractor.ExtractText(docxPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "Office text") {
		t.Errorf("expected Office text: %q", text)
	}
}

// SCRUM-395: pure-Go legacy .xls (BIFF / OLE2) extraction. Fixture is the
// same 2-sheet workbook used by the e2e suite (web/tests/e2e/fixtures/test.xls),
// copied into testdata/ so the Go test doesn't reach outside the module.
// Sheets: "Roster" [name,role / Alex Chen,Senior Staff Engineer / Priya Raman,Hiring Manager]
//         "Notes"  [topic,summary / culture,... / scope,leads notification platform]
func TestExtractXls(t *testing.T) {
	text, err := extractXls(filepath.Join("testdata", "test.xls"))
	if err != nil {
		t.Fatalf("extractXls: %v", err)
	}
	for _, want := range []string{
		"name", "role", "Alex Chen", "Senior Staff Engineer", "Priya Raman", "Hiring Manager",
		"topic", "summary", "culture", "candidate values written communication",
		"scope", "leads notification platform",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("extracted .xls text missing %q\n--- got ---\n%s", want, text)
		}
	}
	// SCRUM-396: output is a GitHub-flavored markdown table (so SpreadsheetViewer
	// renders a real table, not a run-on paragraph) — header row, alignment
	// separator, then `| value | value |` body rows.
	if !strings.HasPrefix(text, "| name | role |\n| --- | --- |\n") {
		t.Errorf("expected a markdown table header+separator; got %q", text)
	}
	if !strings.Contains(text, "| Alex Chen | Senior Staff Engineer |") {
		t.Errorf("expected a markdown table body row; got %q", text)
	}
}

// A non-.xls payload with a .xls name must surface an error, not a panic or
// silent empty result — extractXls runs on arbitrary user uploads.
func TestExtractXls_NotARealXls(t *testing.T) {
	tmp := t.TempDir()
	f := filepath.Join(tmp, "fake.xls")
	if err := os.WriteFile(f, []byte("name,role\nAlex,Eng\n"), 0644); err != nil {
		t.Fatal(err)
	}
	text, err := extractXls(f)
	if err == nil {
		t.Fatalf("expected error for non-OLE2 bytes named .xls; got text=%q", text)
	}
	if text != "" {
		t.Errorf("expected empty text on error; got %q", text)
	}
}

// DefaultOfficeExtractor must route .xls to the BIFF reader.
func TestDefaultOfficeExtractor_Xls(t *testing.T) {
	text, err := DefaultOfficeExtractor.ExtractText(filepath.Join("testdata", "test.xls"))
	if err != nil {
		t.Fatalf("ExtractText(.xls): %v", err)
	}
	if !strings.Contains(text, "Alex Chen") || !strings.Contains(text, "leads notification platform") {
		t.Errorf("expected legacy .xls content via DefaultOfficeExtractor; got %q", text)
	}
}
