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

