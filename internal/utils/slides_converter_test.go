package utils

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestPdftoppmCmd_HonorsEnvOverride asserts pdftoppmCmd() returns the
// TALKBACK_PDFTOPPM_CMD value verbatim when set, without consulting PATH.
// SCRUM-287: this is the new override knob that lets ops point at a
// non-PATH poppler-utils install (e.g. /opt/homebrew/bin/pdftoppm) without
// recompiling.
func TestPdftoppmCmd_HonorsEnvOverride(t *testing.T) {
	t.Setenv("TALKBACK_PDFTOPPM_CMD", "/opt/custom/bin/pdftoppm")
	got, err := pdftoppmCmd()
	if err != nil {
		t.Fatalf("pdftoppmCmd: %v", err)
	}
	if got != "/opt/custom/bin/pdftoppm" {
		t.Fatalf("expected env override; got %q", got)
	}
}

// TestPdftoppmCmd_StructuredErrorWhenMissing asserts pdftoppmCmd() returns a
// structured error when the env override is empty and PATH lookup fails.
// The error message must mention TALKBACK_PDFTOPPM_CMD so an operator
// reading the log knows how to fix it.
func TestPdftoppmCmd_StructuredErrorWhenMissing(t *testing.T) {
	t.Setenv("TALKBACK_PDFTOPPM_CMD", "")
	t.Setenv("PATH", "/nonexistent")
	_, err := pdftoppmCmd()
	if err == nil {
		t.Fatal("expected error when pdftoppm is missing; got nil")
	}
	if !strings.Contains(err.Error(), "TALKBACK_PDFTOPPM_CMD") {
		t.Fatalf("error message must mention TALKBACK_PDFTOPPM_CMD; got %q", err.Error())
	}
}

// TestSofficeAvailable verifies that the LibreOffice soffice binary is available in the environment
// where tests are running. If soffice is not on PATH (e.g. local dev on Windows without LibreOffice),
// the test is skipped rather than failed.
func TestSofficeAvailable(t *testing.T) {
	if _, err := exec.LookPath("soffice"); err != nil {
		t.Skipf("soffice not found on PATH: %v", err)
	}

	cmd := exec.Command("soffice", "--version")
	if err := cmd.Run(); err != nil {
		t.Skipf("soffice on PATH but not usable (install/repair LibreOffice or remove broken shim): %v", err)
	}
}

// TestWarmLibreOffice verifies that WarmLibreOffice completes without panic and logs success.
// Skipped when soffice is not on PATH (same guard as LibreOfficeHealthcheck).
// Skipped under -short (can take up to ~30s when LibreOffice runs a dummy conversion).
func TestWarmLibreOffice(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping LibreOffice warm-up in short mode")
	}
	if _, err := exec.LookPath("soffice"); err != nil {
		t.Skipf("soffice not found on PATH, skipping warm-up test: %v", err)
	}
	// WarmLibreOffice must complete without panicking and without returning an error.
	// Since it only logs (no return value), a panic-free run is the acceptance bar.
	WarmLibreOffice()
}

// TestUseUnoconv_FalseWhenAbsent verifies that useUnoconv returns false when no unoconv binary
// is available and no override env var is set.
func TestUseUnoconv_FalseWhenAbsent(t *testing.T) {
	if _, err := exec.LookPath("unoconv"); err == nil {
		t.Skip("unoconv is present on PATH; this test only covers the absent case")
	}
	t.Setenv("TALKBACK_UNOCONV_CMD", "")
	t.Setenv("TALKBACK_USE_UNOCONV", "")
	t.Setenv("TALKBACK_SOFFICE_CMD", "")
	if useUnoconv() {
		t.Error("useUnoconv() should return false when unoconv is not on PATH")
	}
}

// TestUseUnoconv_FalseWhenSofficeDockerMode verifies that useUnoconv returns false when
// TALKBACK_SOFFICE_CMD is set (Docker wrapper mode), even if unoconv would otherwise be found.
func TestUseUnoconv_FalseWhenSofficeDockerMode(t *testing.T) {
	t.Setenv("TALKBACK_SOFFICE_CMD", "/fake/docker-soffice-wrapper")
	if useUnoconv() {
		t.Error("useUnoconv() should return false when TALKBACK_SOFFICE_CMD is set")
	}
}

// TestUseUnoconv_FalseWhenExplicitlyDisabled verifies TALKBACK_USE_UNOCONV=false opt-out.
func TestUseUnoconv_FalseWhenExplicitlyDisabled(t *testing.T) {
	t.Setenv("TALKBACK_SOFFICE_CMD", "")
	t.Setenv("TALKBACK_USE_UNOCONV", "false")
	if useUnoconv() {
		t.Error("useUnoconv() should return false when TALKBACK_USE_UNOCONV=false")
	}
}

// TestConvertWithUnoconv_CorrectArgs verifies that convertWithUnoconv passes the expected
// arguments to the unoconv binary. Uses a fake shell script pointed to by TALKBACK_UNOCONV_CMD
// that records its argv and creates a stub PDF output file.
func TestConvertWithUnoconv_CorrectArgs(t *testing.T) {
	scriptDir := t.TempDir()
	scriptPath := filepath.Join(scriptDir, "fake-unoconv")
	argsFile := filepath.Join(scriptDir, "args.txt")

	// Script records all args, then creates a stub PDF at <outdir>/<basename>.pdf
	script := "#!/bin/sh\n" +
		"echo \"$@\" > " + argsFile + "\n" +
		"outdir=\"\"\nprev=\"\"\n" +
		"for arg in \"$@\"; do\n" +
		"  if [ \"$prev\" = \"-o\" ]; then outdir=\"$arg\"; fi\n" +
		"  prev=\"$arg\"\n" +
		"  srcfile=\"$arg\"\n" +
		"done\n" +
		"base=$(basename \"$srcfile\")\n" +
		"base=\"${base%.*}\"\n" +
		"touch \"${outdir}/${base}.pdf\"\n"

	if err := os.WriteFile(scriptPath, []byte(script), 0755); err != nil {
		t.Fatalf("write fake script: %v", err)
	}

	t.Setenv("TALKBACK_UNOCONV_CMD", scriptPath)
	t.Setenv("TALKBACK_SOFFICE_CMD", "")

	srcFile := filepath.Join(scriptDir, "test.pptx")
	if err := os.WriteFile(srcFile, []byte("dummy"), 0644); err != nil {
		t.Fatalf("write src file: %v", err)
	}
	outDir := t.TempDir()

	pdfPath, err := convertWithUnoconv(context.Background(), srcFile, outDir)
	if err != nil {
		t.Fatalf("convertWithUnoconv returned error: %v", err)
	}
	if !strings.HasSuffix(pdfPath, ".pdf") {
		t.Errorf("expected PDF path, got %s", pdfPath)
	}

	argsBytes, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	args := string(argsBytes)
	if !strings.Contains(args, "-f pdf") {
		t.Errorf("expected -f pdf in args, got: %s", args)
	}
	if !strings.Contains(args, "-o") {
		t.Errorf("expected -o in args, got: %s", args)
	}
	if !strings.Contains(args, srcFile) {
		t.Errorf("expected src file path in args, got: %s", args)
	}
}
