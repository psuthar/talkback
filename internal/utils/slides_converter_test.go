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

// TestConvertSlides_FallsBackToSofficeWhenUnoconvFails asserts the doc'd
// "unoconv … falling back to soffice --headless" promise on
// ConvertSlidesToPNGsWithLibreOffice. Prior to the fix, an unoconv that
// exited 0 without writing the expected PDF (the warm-up failure mode
// observed in production logs) bubbled up an error that aborted the whole
// pipeline. The fix logs a [WARN] and re-tries via soffice.
//
// We can't easily mock soffice's full PDF rendering in a unit test, so we
// verify the fallback by tracking which binary got invoked: the fake soffice
// records its argv to a sentinel file; the assertion is that the file exists
// after ConvertSlidesToPNGsWithLibreOffice returns. The slides goroutine
// will still error because the fake soffice doesn't produce a real PDF, but
// THAT error mentions soffice (proving fallback ran) rather than unoconv.
func TestConvertSlides_FallsBackToSofficeWhenUnoconvFails(t *testing.T) {
	scriptDir := t.TempDir()
	unoconvPath := filepath.Join(scriptDir, "fake-unoconv-fail")
	sofficePath := filepath.Join(scriptDir, "fake-soffice")
	sofficeMarker := filepath.Join(scriptDir, "soffice-was-invoked.txt")

	// Fake unoconv: exits 0, writes nothing — triggers "did not produce
	// expected PDF" inside convertWithUnoconv.
	unoconvScript := "#!/bin/sh\nexit 0\n"
	if err := os.WriteFile(unoconvPath, []byte(unoconvScript), 0755); err != nil {
		t.Fatalf("write fake unoconv: %v", err)
	}

	// Fake soffice: records that it was invoked, then exits 0 without
	// producing a PDF (the function will then fail at the "soffice did not
	// produce expected PDF" check, but the fact that we reached this binary
	// at all is what we're asserting).
	sofficeScript := "#!/bin/sh\necho \"$@\" > " + sofficeMarker + "\nexit 0\n"
	if err := os.WriteFile(sofficePath, []byte(sofficeScript), 0755); err != nil {
		t.Fatalf("write fake soffice: %v", err)
	}

	// Important: TALKBACK_SOFFICE_CMD must be empty so we use the local
	// (non-Docker) soffice path, but sofficeCmd() resolves via PATH.
	// We swap PATH to point at our fake binaries directory.
	t.Setenv("TALKBACK_UNOCONV_CMD", unoconvPath)
	t.Setenv("TALKBACK_USE_UNOCONV", "")
	t.Setenv("TALKBACK_SOFFICE_CMD", "")
	// Build a PATH that contains only our fake binaries dir + the system
	// /usr/bin so PATH lookup resolves "soffice" to our fake script. We rename
	// the fake to "soffice" via a symlink so exec.LookPath("soffice") finds it.
	pathDir := t.TempDir()
	if err := os.Symlink(sofficePath, filepath.Join(pathDir, "soffice")); err != nil {
		t.Fatalf("symlink fake soffice: %v", err)
	}
	t.Setenv("PATH", pathDir+":/usr/bin:/bin")

	srcFile := filepath.Join(scriptDir, "deck.pptx")
	if err := os.WriteFile(srcFile, []byte("dummy"), 0644); err != nil {
		t.Fatalf("write src file: %v", err)
	}

	_, err := ConvertSlidesToPNGsWithLibreOffice(srcFile)
	// Expect an error from the soffice step (fake doesn't write a PDF),
	// NOT from unoconv. The unoconv-error path used to bail without
	// touching soffice; now it should reach soffice.
	if err == nil {
		t.Fatal("expected an error (fake soffice doesn't produce a real PDF), got nil")
	}
	if !strings.Contains(err.Error(), "soffice") {
		t.Errorf("error should reference soffice (proving fallback ran), got: %v", err)
	}

	// The marker file is the strongest evidence the fallback ran.
	if _, statErr := os.Stat(sofficeMarker); statErr != nil {
		t.Errorf("fake soffice was never invoked (no marker at %s) — unoconv fallback did NOT trigger: %v", sofficeMarker, statErr)
	}
}
