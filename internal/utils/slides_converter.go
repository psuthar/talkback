package utils

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Limit concurrent slide conversions to reduce CPU/memory pressure on small hosts (e.g. Render.com).
// Slide conversion is best-effort and must not starve other background work like DOCX extraction.
var slideConversionSem = make(chan struct{}, 1)

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
// TALKBACK_SLIDE_DPI env (default 72); clamped to 72–300. Raise for sharper in-browser previews (slower).
func slideDPI() int {
	const defaultDPI = 72
	const minDPI, maxDPI = 72, 300
	if s := os.Getenv("TALKBACK_SLIDE_DPI"); s != "" {
		var n int
		if _, err := fmt.Sscanf(s, "%d", &n); err == nil && n >= minDPI && n <= maxDPI {
			return n
		}
	}
	return defaultDPI
}

// pdfToPpmTimeout bounds the PDF→PNG raster step. Large decks at high DPI can exceed 1–2 minutes.
// TALKBACK_PDFTOPPM_TIMEOUT is seconds (default 180). Previously this was hard-coded at 60s.
func pdfToPpmTimeout() time.Duration {
	const defaultSec = 180
	if s := strings.TrimSpace(os.Getenv("TALKBACK_PDFTOPPM_TIMEOUT")); s != "" {
		var sec int
		if _, err := fmt.Sscanf(s, "%d", &sec); err == nil && sec > 0 {
			return time.Duration(sec) * time.Second
		}
	}
	return defaultSec * time.Second
}

// slideParallelism returns the number of concurrent pdftoppm workers to use.
// TALKBACK_SLIDE_WORKERS env (default 4); clamped to 1–16.
// On Render.com shared CPU, 4 workers provides a good balance of parallelism without overloading.
func slideParallelism() int {
	const defaultWorkers = 4
	const minWorkers, maxWorkers = 1, 16
	if s := os.Getenv("TALKBACK_SLIDE_WORKERS"); s != "" {
		var n int
		if _, err := fmt.Sscanf(s, "%d", &n); err == nil && n >= minWorkers && n <= maxWorkers {
			return n
		}
	}
	return defaultWorkers
}

// pdfPageCount returns the number of pages in a PDF by running pdftoppm on the first page
// and counting all output files after a full run (or via pdfinfo if available).
// We use a lightweight probe: run pdfinfo if available, otherwise fall back to pdftoppm -l 1
// and see what the last page number pdftoppm would assign is by checking its output naming.
// If page count cannot be determined, returns 0 (caller should fall back to single-process mode).
func pdfPageCount(ctx context.Context, pdfPath string) int {
	// Try pdfinfo first — it's part of poppler-utils alongside pdftoppm.
	cmd := exec.CommandContext(ctx, "pdfinfo", pdfPath)
	out, err := cmd.Output()
	if err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			if strings.HasPrefix(line, "Pages:") {
				parts := strings.Fields(line)
				if len(parts) >= 2 {
					if n, parseErr := strconv.Atoi(parts[1]); parseErr == nil && n > 0 {
						return n
					}
				}
			}
		}
	}
	return 0
}

// pdfPageCountDocker returns the number of pages in a PDF via pdfinfo inside Docker.
func pdfPageCountDocker(ctx context.Context, containerPDF, root string) int {
	cmd := exec.CommandContext(ctx, "docker", "run", "--rm", "--entrypoint", "pdfinfo",
		"-v", root+":/data", "talkback-api", containerPDF)
	out, err := cmd.Output()
	if err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			if strings.HasPrefix(line, "Pages:") {
				parts := strings.Fields(line)
				if len(parts) >= 2 {
					if n, parseErr := strconv.Atoi(parts[1]); parseErr == nil && n > 0 {
						return n
					}
				}
			}
		}
	}
	return 0
}

// runPdfToPpmOnePage runs pdftoppm for a single page (pageNum, 1-based). Used by the parallel worker.
func runPdfToPpmOnePage(ctx context.Context, pdfPath, outPrefix, dpiStr string, pageNum int) error {
	pageStr := strconv.Itoa(pageNum)
	cmd := exec.CommandContext(ctx, "pdftoppm", "-png", "-r", dpiStr, "-f", pageStr, "-l", pageStr, pdfPath, outPrefix)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("pdftoppm page %d failed: %w; output=%s", pageNum, err, string(out))
	}
	return nil
}

// runPdfToPpmOnePageDocker runs pdftoppm for a single page inside Docker. Used by the parallel worker.
func runPdfToPpmOnePageDocker(ctx context.Context, root, containerPDF, containerPrefix, dpiStr string, pageNum int) error {
	pageStr := strconv.Itoa(pageNum)
	cmd := exec.CommandContext(ctx, "docker", "run", "--rm", "--entrypoint", "pdftoppm",
		"-v", root+":/data", "talkback-api",
		"-png", "-r", dpiStr, "-f", pageStr, "-l", pageStr,
		containerPDF, containerPrefix)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("pdftoppm (Docker) page %d failed: %w; output=%s", pageNum, err, string(out))
	}
	return nil
}

// runPdfToPpm runs pdftoppm -png to produce one PNG per page. When TALKBACK_SOFFICE_CMD is set, runs pdftoppm in Docker.
// Uses slideDPI() for resolution and slideParallelism() workers to convert pages concurrently.
// Parallel conversion significantly reduces wall-clock time on constrained CPUs (e.g. Render.com shared).
func runPdfToPpm(pdfPath, outPrefix string) error {
	dpi := slideDPI()
	dpiStr := strconv.Itoa(dpi)
	ppmDeadline := pdfToPpmTimeout()
	root := uploadRoot()
	workers := slideParallelism()

	ctx, cancel := context.WithTimeout(context.Background(), ppmDeadline)
	defer cancel()

	if os.Getenv("TALKBACK_SOFFICE_CMD") != "" {
		rel, err := filepath.Rel(root, pdfPath)
		if err != nil {
			return fmt.Errorf("pdf path not under upload root: %w", err)
		}
		relDir := filepath.Dir(rel)
		containerPDF := "/data/" + filepath.ToSlash(rel)
		containerPrefix := "/data/" + filepath.ToSlash(relDir) + "/slide"

		// Attempt parallel conversion if we can determine the page count.
		pageCount := pdfPageCountDocker(ctx, containerPDF, root)
		if pageCount > 1 && workers > 1 {
			log.Printf("slides timing: pdftoppm docker parallel workers=%d pages=%d", workers, pageCount)
			return runPdfToPpmParallelDocker(ctx, root, containerPDF, containerPrefix, dpiStr, pageCount, workers)
		}
		// Fall back to single-process conversion (unknown page count or single page).
		cmd := exec.CommandContext(ctx, "docker", "run", "--rm", "--entrypoint", "pdftoppm",
			"-v", root+":/data", "talkback-api",
			"-png", "-r", dpiStr, containerPDF, containerPrefix)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("pdftoppm (Docker) failed: %w; output=%s", err, string(out))
		}
		return nil
	}

	// Attempt parallel conversion if we can determine the page count.
	pageCount := pdfPageCount(ctx, pdfPath)
	if pageCount > 1 && workers > 1 {
		log.Printf("slides timing: pdftoppm parallel workers=%d pages=%d", workers, pageCount)
		return runPdfToPpmParallel(ctx, pdfPath, outPrefix, dpiStr, pageCount, workers)
	}
	// Fall back to single-process conversion (unknown page count or single page).
	cmd := exec.CommandContext(ctx, "pdftoppm", "-png", "-r", dpiStr, pdfPath, outPrefix)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("pdftoppm failed: %w; output=%s", err, string(out))
	}
	return nil
}

// runPdfToPpmParallel converts a multi-page PDF to PNGs using a bounded worker pool.
// Each worker converts exactly one page via pdftoppm -f N -l N.
// On a 4-worker pool, 13 pages complete in ~ceil(13/4) = 4 sequential rounds instead of 13.
func runPdfToPpmParallel(ctx context.Context, pdfPath, outPrefix, dpiStr string, pageCount, workers int) error {
	type result struct {
		page int
		err  error
	}
	jobs := make(chan int, pageCount)
	results := make(chan result, pageCount)

	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for page := range jobs {
				err := runPdfToPpmOnePage(ctx, pdfPath, outPrefix, dpiStr, page)
				results <- result{page: page, err: err}
			}
		}()
	}

	for page := 1; page <= pageCount; page++ {
		jobs <- page
	}
	close(jobs)

	go func() {
		wg.Wait()
		close(results)
	}()

	var firstErr error
	for r := range results {
		if r.err != nil && firstErr == nil {
			firstErr = r.err
		}
	}
	return firstErr
}

// runPdfToPpmParallelDocker is the Docker equivalent of runPdfToPpmParallel.
func runPdfToPpmParallelDocker(ctx context.Context, root, containerPDF, containerPrefix, dpiStr string, pageCount, workers int) error {
	type result struct {
		page int
		err  error
	}
	jobs := make(chan int, pageCount)
	results := make(chan result, pageCount)

	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for page := range jobs {
				err := runPdfToPpmOnePageDocker(ctx, root, containerPDF, containerPrefix, dpiStr, page)
				results <- result{page: page, err: err}
			}
		}()
	}

	for page := 1; page <= pageCount; page++ {
		jobs <- page
	}
	close(jobs)

	go func() {
		wg.Wait()
		close(results)
	}()

	var firstErr error
	for r := range results {
		if r.err != nil && firstErr == nil {
			firstErr = r.err
		}
	}
	return firstErr
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
	overallStart := time.Now()
	cmdPath, err := sofficeCmd()
	if err != nil {
		return nil, err
	}

	// Serialize slide conversions to avoid resource contention on constrained platforms.
	semWait := time.Now()
	slideConversionSem <- struct{}{}
	if d := time.Since(semWait); d > 200*time.Millisecond {
		log.Printf("slides timing: waited %v for conversion slot (queue) src=%s", d, srcPath)
	}
	defer func() { <-slideConversionSem }()

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
	sofficeTimeout := 3 * time.Minute
	if s := strings.TrimSpace(os.Getenv("TALKBACK_SOFFICE_TIMEOUT")); s != "" {
		if seconds, parseErr := strconv.Atoi(s); parseErr == nil && seconds > 0 {
			sofficeTimeout = time.Duration(seconds) * time.Second
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), sofficeTimeout)
	defer cancel()

	tSoffice := time.Now()
	cmd := exec.CommandContext(
		ctx,
		cmdPath,
		"--headless",
		"--convert-to", "pdf",
		"--outdir", tmpDir,
		srcPath,
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return nil, fmt.Errorf("soffice conversion to PDF timed out after %s", sofficeTimeout)
		}
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
	log.Printf("slides timing: soffice pptx→pdf %v src=%s", time.Since(tSoffice), srcPath)

	// Step 2: PDF → one PNG per page (pdftoppm)
	outPrefix := filepath.Join(tmpDir, "slide")
	tPpm := time.Now()
	if err := runPdfToPpm(pdfPath, outPrefix); err != nil {
		return nil, err
	}
	log.Printf("slides timing: pdftoppm pdf→png dpi=%d %v src=%s", slideDPI(), time.Since(tPpm), srcPath)

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

	tRead := time.Now()
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
	log.Printf("slides timing: read %d png files %v | convert total %v src=%s",
		len(slides), time.Since(tRead), time.Since(overallStart), srcPath)
	return slides, nil
}

