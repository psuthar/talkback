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
	if err := f.SetCellValue("Sheet1", "A1", "Hello XLSX"); err != nil {
		t.Fatal(err)
	}
	if err := f.SetCellValue("Sheet1", "B1", "Cell B"); err != nil {
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
	if !strings.Contains(text, "Hello XLSX") {
		t.Errorf("expected Hello XLSX in output: %q", text)
	}
	if !strings.Contains(text, "Cell B") {
		t.Errorf("expected Cell B in output: %q", text)
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
