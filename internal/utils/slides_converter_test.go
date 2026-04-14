package utils

import (
	"os/exec"
	"testing"
)

// TestSofficeAvailable verifies that the LibreOffice soffice binary is available in the environment
// where tests are running. If soffice is not on PATH (e.g. local dev on Windows without LibreOffice),
// the test is skipped rather than failed.
func TestSofficeAvailable(t *testing.T) {
	if _, err := exec.LookPath("soffice"); err != nil {
		t.Skipf("soffice not found on PATH: %v", err)
	}

	cmd := exec.Command("soffice", "--version")
	if err := cmd.Run(); err != nil {
		t.Fatalf("soffice --version failed: %v", err)
	}
}

// TestWarmLibreOffice verifies that WarmLibreOffice completes without panic and logs success.
// Skipped when soffice is not on PATH (same guard as LibreOfficeHealthcheck).
func TestWarmLibreOffice(t *testing.T) {
	if _, err := exec.LookPath("soffice"); err != nil {
		t.Skipf("soffice not found on PATH, skipping warm-up test: %v", err)
	}
	// WarmLibreOffice must complete without panicking and without returning an error.
	// Since it only logs (no return value), a panic-free run is the acceptance bar.
	WarmLibreOffice()
}

