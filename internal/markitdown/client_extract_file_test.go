package markitdown

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// fileClientFor mirrors clientFor but with FileTimeout populated so
// ExtractFile uses a non-zero per-call deadline.
func fileClientFor(t *testing.T, server *httptest.Server) *Client {
	t.Helper()
	return &Client{
		BaseURL:      server.URL,
		Secret:       "test-secret",
		HTTP:         server.Client(),
		Retries:      1,
		ImageTimeout: 5 * time.Second,
		URLTimeout:   5 * time.Second,
		FileTimeout:  5 * time.Second,
	}
}

// ─── FileExtractionEnabled() ────────────────────────────────────────────

func TestFileExtractionEnabled_DefaultsFalse(t *testing.T) {
	t.Setenv("MARKITDOWN_FILE_EXTRACTION_ENABLED", "")
	if FileExtractionEnabled() {
		t.Fatal("expected default false")
	}
}

func TestFileExtractionEnabled_TrueOnlyWhenEnvIsTrue(t *testing.T) {
	cases := map[string]bool{
		"true":  true,
		"True":  true,
		"TRUE":  true,
		"false": false,
		"yes":   false,
		"1":     false, // explicit string match — only "true" (case-insensitive) flips on
	}
	for value, want := range cases {
		t.Run(value, func(t *testing.T) {
			t.Setenv("MARKITDOWN_FILE_EXTRACTION_ENABLED", value)
			if got := FileExtractionEnabled(); got != want {
				t.Fatalf("env=%q: want %v, got %v", value, want, got)
			}
		})
	}
}

// ─── ExtractFile happy path + multipart shape + auth header ─────────────

func TestExtractFile_HappyPath_CSV(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/extract/file" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-secret" {
			t.Errorf("missing/wrong bearer header: %q", got)
		}
		mediaType, params, _ := mime_parse(r.Header.Get("Content-Type"))
		if mediaType != "multipart/form-data" {
			t.Errorf("expected multipart/form-data, got %q", mediaType)
		}
		mr := multipart.NewReader(r.Body, params["boundary"])
		// First part: file
		filePart, err := mr.NextPart()
		if err != nil {
			t.Fatalf("read first part: %v", err)
		}
		if filePart.FormName() != "file" {
			t.Errorf("expected first form field 'file', got %q", filePart.FormName())
		}
		if got := filePart.Header.Get("Content-Type"); got != "text/csv" {
			t.Errorf("expected part content-type text/csv, got %q", got)
		}
		body, _ := io.ReadAll(filePart)
		if string(body) != "name,role\nAlex,Eng\n" {
			t.Errorf("unexpected body bytes: %q", body)
		}
		// Second part: file_extension form field
		extPart, err := mr.NextPart()
		if err != nil {
			t.Fatalf("read second part: %v", err)
		}
		if extPart.FormName() != "file_extension" {
			t.Errorf("expected second form field 'file_extension', got %q", extPart.FormName())
		}
		extVal, _ := io.ReadAll(extPart)
		if string(extVal) != ".csv" {
			t.Errorf("expected file_extension value '.csv', got %q", extVal)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"text":"| name | role |\n| --- | --- |\n| Alex | Eng |","extension":".csv","bytes":21}`)
	}))
	defer srv.Close()

	c := fileClientFor(t, srv)
	got, err := c.ExtractFile(context.Background(), []byte("name,role\nAlex,Eng\n"), "text/csv", "customers.csv", ".csv")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !strings.Contains(got.Text, "| name | role |") {
		t.Fatalf("expected markdown table in result, got %q", got.Text)
	}
	if got.Extension != ".csv" {
		t.Fatalf("expected extension .csv, got %q", got.Extension)
	}
	if got.Bytes != 21 {
		t.Fatalf("expected bytes=21, got %d", got.Bytes)
	}
}

// XLSX returns sheet headings — confirms the multi-sheet shape is preserved
// end-to-end through the client.
func TestExtractFile_HappyPath_XLSX_MultiSheet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"text":"## Sheet1\n\n| a | b |\n\n## Sheet2\n\n| x | y |","extension":".xlsx","bytes":1024}`)
	}))
	defer srv.Close()

	c := fileClientFor(t, srv)
	got, err := c.ExtractFile(context.Background(), []byte("PK\x03\x04stub-xlsx"), "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", "report.xlsx", ".xlsx")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !strings.Contains(got.Text, "## Sheet1") || !strings.Contains(got.Text, "## Sheet2") {
		t.Fatalf("expected two sheet headings, got %q", got.Text)
	}
}

// ─── ExtractFile error paths ───────────────────────────────────────────

func TestExtractFile_DisabledClient_ReturnsUnavailable(t *testing.T) {
	c := &Client{} // BaseURL/Secret empty -> Enabled()==false
	_, err := c.ExtractFile(context.Background(), []byte("a,b\n1,2"), "text/csv", "x.csv", ".csv")
	if !errors.Is(err, ErrSidecarUnavailable) {
		t.Fatalf("expected ErrSidecarUnavailable, got %v", err)
	}
}

func TestExtractFile_Unauthorized_Returns401Sentinel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"detail":{"error":"missing_bearer_token"}}`)
	}))
	defer srv.Close()
	c := fileClientFor(t, srv)
	_, err := c.ExtractFile(context.Background(), []byte("x"), "text/csv", "x.csv", ".csv")
	if !errors.Is(err, ErrSidecarUnauthorized) {
		t.Fatalf("expected ErrSidecarUnauthorized, got %v", err)
	}
	var se *SidecarError
	if !errors.As(err, &se) {
		t.Fatalf("expected SidecarError, got %T", err)
	}
	if se.HTTPStatus != http.StatusUnauthorized {
		t.Fatalf("expected HTTPStatus 401, got %d", se.HTTPStatus)
	}
}

func TestExtractFile_TooLarge_Returns413BadRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusRequestEntityTooLarge)
		fmt.Fprint(w, `{"detail":{"error":"spreadsheet_too_large","max_bytes":5242880}}`)
	}))
	defer srv.Close()
	c := fileClientFor(t, srv)
	_, err := c.ExtractFile(context.Background(), []byte("big"), "text/csv", "big.csv", ".csv")
	if !errors.Is(err, ErrSidecarBadRequest) {
		t.Fatalf("expected ErrSidecarBadRequest for 413, got %v", err)
	}
	var se *SidecarError
	errors.As(err, &se)
	if se.Code != "spreadsheet_too_large" {
		t.Fatalf("expected code spreadsheet_too_large, got %q", se.Code)
	}
}

func TestExtractFile_UnsupportedExtension_Returns415BadRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnsupportedMediaType)
		fmt.Fprint(w, `{"detail":{"error":"unsupported_extension","extension":".pdf"}}`)
	}))
	defer srv.Close()
	c := fileClientFor(t, srv)
	_, err := c.ExtractFile(context.Background(), []byte("%PDF"), "application/pdf", "doc.pdf", ".pdf")
	if !errors.Is(err, ErrSidecarBadRequest) {
		t.Fatalf("expected ErrSidecarBadRequest for 415, got %v", err)
	}
	var se *SidecarError
	errors.As(err, &se)
	if se.Code != "unsupported_extension" {
		t.Fatalf("expected code unsupported_extension, got %q", se.Code)
	}
}

func TestExtractFile_MalformedInput_Returns422BadRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		fmt.Fprint(w, `{"error":"extraction_failed","message":"corrupt zip"}`)
	}))
	defer srv.Close()
	c := fileClientFor(t, srv)
	_, err := c.ExtractFile(context.Background(), []byte("not-a-zip"), "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", "bad.xlsx", ".xlsx")
	if !errors.Is(err, ErrSidecarBadRequest) {
		t.Fatalf("expected ErrSidecarBadRequest for 422, got %v", err)
	}
	var se *SidecarError
	errors.As(err, &se)
	if se.Code != "extraction_failed" {
		t.Fatalf("expected code extraction_failed, got %q", se.Code)
	}
}

func TestExtractFile_5xxAfterRetry_ReturnsUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"down"}`, http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	c := fileClientFor(t, srv)
	c.Retries = 0
	_, err := c.ExtractFile(context.Background(), []byte("x"), "text/csv", "x.csv", ".csv")
	if !errors.Is(err, ErrSidecarUnavailable) {
		t.Fatalf("expected ErrSidecarUnavailable for 5xx, got %v", err)
	}
}
