package utils

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ConvertedSlide holds the result of converting a slide deck to PNG images.
type ConvertedSlide struct {
	Index int
	Name  string
	Data  []byte
}

// SlideManifest describes the derived slide assets stored in object storage.
type SlideManifest struct {
	Slides []SlideManifestEntry `json:"slides"`
}

// SlideManifestEntry describes a single derived slide asset.
type SlideManifestEntry struct {
	Index      int    `json:"index"`
	StorageKey string `json:"storage_key"`
}

// sofficeCmd returns the executable to use for slide conversion: TALKBACK_SOFFICE_CMD if set, otherwise "soffice".
func sofficeCmd() (string, error) {
	if cmd := os.Getenv("TALKBACK_SOFFICE_CMD"); cmd != "" {
		return cmd, nil
	}
	path, err := exec.LookPath("soffice")
	if err != nil {
		return "", fmt.Errorf("soffice not found on PATH (install LibreOffice or set TALKBACK_SOFFICE_CMD to a Docker wrapper): %w", err)
	}
	return path, nil
}

// LibreOfficeHealthcheck runs soffice --version at startup and returns a one-line log message.
// Used locally and on Render to confirm PPT/slide conversion is available. Does not block startup.
// When TALKBACK_SOFFICE_CMD is set (e.g. Docker wrapper), we skip running it—wrapper exit code/output
// often isn't captured correctly from Go on Windows, and conversion will be tested on first PPT upload.
func LibreOfficeHealthcheck() string {
	if os.Getenv("TALKBACK_SOFFICE_CMD") != "" {
		return "LibreOffice healthcheck: ok (TALKBACK_SOFFICE_CMD configured; will use for PPT conversion)"
	}
	cmdPath, err := sofficeCmd()
	if err != nil {
		return "LibreOffice healthcheck: unavailable (" + err.Error() + ")"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, cmdPath, "--version")
	out, runErr := cmd.CombinedOutput()
	version := strings.TrimSpace(string(out))
	if idx := strings.Index(version, "\n"); idx > 0 {
		version = strings.TrimSpace(version[:idx])
	}
	if runErr != nil && (version == "" || (!strings.Contains(version, "LibreOffice") && !strings.Contains(version, "Build"))) {
		return fmt.Sprintf("LibreOffice healthcheck: unavailable (soffice --version failed: %v)", runErr)
	}
	if version == "" {
		version = "ok"
	}
	return "LibreOffice healthcheck: " + version
}

// uploadRoot returns the base directory for uploads (TALKBACK_UPLOAD_ROOT or cwd).
func uploadRoot() string {
	if root := os.Getenv("TALKBACK_UPLOAD_ROOT"); root != "" {
		return filepath.Clean(root)
	}
	wd, _ := os.Getwd()
	return wd
}

// uploadRootForTemp returns a directory under which to create temp dirs when using an external soffice (e.g. Docker).
// So that one volume mount can see both input and output, temp must be under the same root as uploads.
func uploadRootForTemp() string {
	return filepath.Join(uploadRoot(), ".tmp")
}

// slideDPI returns the resolution (DPI) for PDF→PNG conversion. Lower = faster and smaller files.
// TALKBACK_SLIDE_DPI env (default 100); clamped to 72–300. 100 is a good balance for in-browser preview.
func slideDPI() int {
	const defaultDPI = 100
	const minDPI, maxDPI = 72, 300
	if s := os.Getenv("TALKBACK_SLIDE_DPI"); s != "" {
		var n int
		if _, err := fmt.Sscanf(s, "%d", &n); err == nil && n >= minDPI && n <= maxDPI {
			return n
		}
	}
	return defaultDPI
}

// runPdfToPpm runs pdftoppm -png to produce one PNG per page. When TALKBACK_SOFFICE_CMD is set, runs pdftoppm in Docker.
// Uses slideDPI() for resolution; lower DPI speeds up conversion and reduces file size.
func runPdfToPpm(pdfPath, outPrefix string) error {
	dpi := slideDPI()
	dpiStr := strconv.Itoa(dpi)
	root := uploadRoot()
	if os.Getenv("TALKBACK_SOFFICE_CMD") != "" {
		rel, err := filepath.Rel(root, pdfPath)
		if err != nil {
			return fmt.Errorf("pdf path not under upload root: %w", err)
		}
		relDir := filepath.Dir(rel)
		containerPDF := "/data/" + filepath.ToSlash(rel)
		containerPrefix := "/data/" + filepath.ToSlash(relDir) + "/slide"
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "docker", "run", "--rm", "--entrypoint", "pdftoppm", "-v", root+":/data", "talkback-api", "-png", "-r", dpiStr, containerPDF, containerPrefix)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("pdftoppm (Docker) failed: %w; output=%s", err, string(out))
		}
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "pdftoppm", "-png", "-r", dpiStr, pdfPath, outPrefix)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("pdftoppm failed: %w; output=%s", err, string(out))
	}
	return nil
}

// slideNumberFromFilename extracts the page number from pdftoppm output like "slide-1.png" or "slide-01.png".
func slideNumberFromFilename(name string) int {
	base := strings.TrimSuffix(name, filepath.Ext(name)) // "slide-1" or "slide-01"
	parts := strings.Split(base, "-")
	if len(parts) < 2 {
		return 0
	}
	n, _ := strconv.Atoi(parts[len(parts)-1])
	return n
}

// ConvertSlidesToPNGsWithLibreOffice converts a local PPT/PPTX file to one PNG per slide.
// It first converts to PDF with LibreOffice (soffice), then uses pdftoppm to produce one PNG per page,
// because soffice --convert-to png only exports the first slide for PPT/PPTX.
// Callers are responsible for treating failures as best-effort only.
func ConvertSlidesToPNGsWithLibreOffice(srcPath string) ([]ConvertedSlide, error) {
	cmdPath, err := sofficeCmd()
	if err != nil {
		return nil, err
	}

	var tmpDir string
	if os.Getenv("TALKBACK_SOFFICE_CMD") != "" {
		base := uploadRootForTemp()
		if err := os.MkdirAll(base, 0755); err != nil {
			return nil, fmt.Errorf("create temp base for soffice: %w", err)
		}
		tmpDir, err = os.MkdirTemp(base, "talkback-slides-*")
	} else {
		tmpDir, err = os.MkdirTemp("", "talkback-slides-*")
	}
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmpDir)

	// Step 1: PPT/PPTX → PDF (soffice exports all slides to PDF; --convert-to png only does first slide)
	cmd := exec.Command(
		cmdPath,
		"--headless",
		"--convert-to", "pdf",
		"--outdir", tmpDir,
		srcPath,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("soffice conversion to PDF failed: %w; output=%s", err, string(output))
	}

	baseName := strings.TrimSuffix(filepath.Base(srcPath), filepath.Ext(srcPath))
	pdfPath := filepath.Join(tmpDir, baseName+".pdf")
	if _, err := os.Stat(pdfPath); err != nil {
		// On Windows LibreOffice may emit mixed-case extension (e.g. .PDF); resolve case-insensitively.
		candidates, _ := filepath.Glob(filepath.Join(tmpDir, baseName+".*"))
		resolved := ""
		for _, c := range candidates {
			if strings.EqualFold(filepath.Ext(c), ".pdf") {
				resolved = c
				break
			}
		}
		if resolved == "" {
			return nil, fmt.Errorf("soffice did not produce expected PDF %s: %w", pdfPath, err)
		}
		pdfPath = resolved
	}

	// Step 2: PDF → one PNG per page (pdftoppm)
	outPrefix := filepath.Join(tmpDir, "slide")
	if err := runPdfToPpm(pdfPath, outPrefix); err != nil {
		return nil, err
	}

	matches, err := filepath.Glob(filepath.Join(tmpDir, "slide-*.png"))
	if err != nil {
		return nil, err
	}
	if len(matches) == 0 {
		return nil, fmt.Errorf("no PNG slides were produced")
	}
	// Sort by slide number (slide-1, slide-2, ..., slide-10)
	sort.Slice(matches, func(i, j int) bool {
		return slideNumberFromFilename(filepath.Base(matches[i])) < slideNumberFromFilename(filepath.Base(matches[j]))
	})

	slides := make([]ConvertedSlide, 0, len(matches))
	for i, p := range matches {
		data, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		slides = append(slides, ConvertedSlide{
			Index: i + 1,
			Name:  filepath.Base(p),
			Data:  data,
		})
	}
	return slides, nil
}

