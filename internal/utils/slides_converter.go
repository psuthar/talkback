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
//
// SCRUM-444/445: extended for the PDF pipeline. Legacy manifests
// ({"slides":[...]}) decode with Format == "" (treated as "pngs"). New PDF
// manifests carry Format="pdf" + SlideCount + a PDF locator (PDFStorageKey for
// R2, PDFStoragePath for the local-disk driver). Slides remains the legacy
// per-image list, empty under the PDF path.
type SlideManifest struct {
	Format         string               `json:"format,omitempty"`
	SlideCount     int                  `json:"slide_count,omitempty"`
	PDFStorageKey  string               `json:"pdf_storage_key,omitempty"`
	PDFStoragePath string               `json:"pdf_storage_path,omitempty"`
	Slides         []SlideManifestEntry `json:"slides,omitempty"`
}

// SlideManifestEntry describes a single derived slide asset.
type SlideManifestEntry struct {
	Index      int    `json:"index"`
	StorageKey string `json:"storage_key"`
}

// SlidesPipelinePDF identifies the PDF pipeline; SlidesPipelinePNGs is the legacy raster path.
const (
	SlidesPipelinePDF  = "pdf"
	SlidesPipelinePNGs = "pngs"
)

// SlidesPipeline returns the active slides pipeline ("pdf" or "pngs") from the
// TALKBACK_SLIDES_PIPELINE env. Default and any non-"pdf" value are treated as
// the legacy "pngs" pipeline so the cutover stays a one-line render.yaml flip.
func SlidesPipeline() string {
	if v := strings.ToLower(strings.TrimSpace(os.Getenv("TALKBACK_SLIDES_PIPELINE"))); v == SlidesPipelinePDF {
		return SlidesPipelinePDF
	}
	return SlidesPipelinePNGs
}

// ConvertPPTXToPDFFn is the active PPTX→PDF converter, wired to
// ConvertPPTXToPDFWithLibreOffice in production. Exposed as a variable so
// SCRUM-444/445 tests can inject a stub that returns a known PDF path without
// invoking the real soffice binary (CI may not have LibreOffice installed).
var ConvertPPTXToPDFFn func(srcPath string) (string, int, func(), error) = ConvertPPTXToPDFWithLibreOffice

// ConvertSlidesToPNGsFn is the equivalent injection point for the legacy PNG
// raster path. Keeping both injection points symmetric lets the same test
// scaffolding cover the regression case for flag="pngs".
var ConvertSlidesToPNGsFn func(srcPath string) ([]ConvertedSlide, error) = ConvertSlidesToPNGsWithLibreOffice

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

// pdftoppmCmd returns the executable to use for PDF→PNG rasterisation in
// local mode: TALKBACK_PDFTOPPM_CMD if set, otherwise PATH-resolved
// "pdftoppm". Symmetric with sofficeCmd so ops can swap the binary path
// (e.g. point at a homebrew-installed poppler in non-PATH locations) without
// recompiling.
//
// Docker mode bypasses this helper entirely — slide conversion runs inside
// the talkback-api container via `docker run --entrypoint pdftoppm`, gated
// on TALKBACK_SOFFICE_CMD.
//
// SCRUM-287: introduced as part of the .pptx ingest hardening; the prior
// code invoked bare "pdftoppm" with no override + no startup health check,
// so a missing/relocated poppler-utils install caused slide derivation to
// fail in a background goroutine with no user-visible signal.
func pdftoppmCmd() (string, error) {
	if cmd := os.Getenv("TALKBACK_PDFTOPPM_CMD"); cmd != "" {
		return cmd, nil
	}
	path, err := exec.LookPath("pdftoppm")
	if err != nil {
		return "", fmt.Errorf("pdftoppm not found on PATH (install poppler-utils or set TALKBACK_PDFTOPPM_CMD): %w", err)
	}
	return path, nil
}

// unoconvCmd returns the unoconv executable path: TALKBACK_UNOCONV_CMD if set, otherwise searches PATH.
func unoconvCmd() (string, error) {
	if cmd := os.Getenv("TALKBACK_UNOCONV_CMD"); cmd != "" {
		return cmd, nil
	}
	path, err := exec.LookPath("unoconv")
	if err != nil {
		return "", fmt.Errorf("unoconv not found on PATH: %w", err)
	}
	return path, nil
}

// useUnoconv reports whether the unoconv persistent-listener path should be used for PPTX→PDF conversion.
// It returns false when TALKBACK_SOFFICE_CMD is set (Docker wrapper mode) or when unoconv is absent.
// Set TALKBACK_USE_UNOCONV=false to explicitly disable even when unoconv is installed.
func useUnoconv() bool {
	if os.Getenv("TALKBACK_SOFFICE_CMD") != "" {
		return false // Docker wrapper mode: unoconv is not applicable
	}
	if v := strings.ToLower(strings.TrimSpace(os.Getenv("TALKBACK_USE_UNOCONV"))); v == "false" {
		return false
	}
	_, err := unoconvCmd()
	return err == nil
}

// convertWithUnoconv converts srcPath to PDF using the unoconv persistent listener, writing
// the result into outDir. Returns the path to the produced PDF or an error.
// unoconv maintains a background LibreOffice listener process; subsequent conversions reuse it,
// reducing per-call cost from ~8-10s (fresh soffice) to ~0.5-1s.
func convertWithUnoconv(ctx context.Context, srcPath, outDir string) (string, error) {
	cmdPath, err := unoconvCmd()
	if err != nil {
		return "", err
	}
	cmd := exec.CommandContext(ctx, cmdPath, "-f", "pdf", "-o", outDir, srcPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			return "", fmt.Errorf("unoconv conversion to PDF timed out")
		}
		return "", fmt.Errorf("unoconv conversion to PDF failed: %w; output=%s", err, string(out))
	}
	baseName := strings.TrimSuffix(filepath.Base(srcPath), filepath.Ext(srcPath))
	pdfPath := filepath.Join(outDir, baseName+".pdf")
	if _, err := os.Stat(pdfPath); err != nil {
		// Try case-insensitive extension match.
		candidates, _ := filepath.Glob(filepath.Join(outDir, baseName+".*"))
		for _, c := range candidates {
			if strings.EqualFold(filepath.Ext(c), ".pdf") {
				return c, nil
			}
		}
		return "", fmt.Errorf("unoconv did not produce expected PDF %s: %w", pdfPath, err)
	}
	return pdfPath, nil
}

// WarmLibreOffice runs a minimal conversion so the OS page-caches the binary and shared libraries
// and the LibreOffice user profile is created before the first real PPTX upload.
// When unoconv is active, the warm-up call also starts the persistent listener so the first real
// conversion reuses it (~0.5s) instead of spawning a fresh process (~8-10s).
//
// Call in a background goroutine at API startup — the function is non-blocking by design but
// may take up to 30s on a cold host. Failure is logged but never fatal.
// Skipped when TALKBACK_SOFFICE_CMD is set (Docker wrapper mode).
func WarmLibreOffice() {
	if os.Getenv("TALKBACK_SOFFICE_CMD") != "" {
		log.Println("LibreOffice warm-up: skipped (TALKBACK_SOFFICE_CMD is set)")
		return
	}

	// Write a tiny HTML file — any format LibreOffice can convert triggers profile/listener init.
	tmpFile, err := os.CreateTemp("", "talkback-lo-warmup-*.html")
	if err != nil {
		log.Printf("LibreOffice warm-up: could not create temp file: %v", err)
		return
	}
	tmpFile.WriteString("<html><body>warmup</body></html>")
	tmpFile.Close()
	defer os.Remove(tmpFile.Name())

	outDir, err := os.MkdirTemp("", "talkback-lo-warmup-out-*")
	if err != nil {
		log.Printf("LibreOffice warm-up: could not create temp dir: %v", err)
		return
	}
	defer os.RemoveAll(outDir)

	start := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if useUnoconv() {
		// Start the unoconv listener by performing a dummy conversion.
		if _, err := convertWithUnoconv(ctx, tmpFile.Name(), outDir); err != nil {
			log.Printf("LibreOffice warm-up (unoconv): failed in %v: %v", time.Since(start), err)
			return
		}
		log.Printf("LibreOffice warm-up complete (unoconv listener started) in %v", time.Since(start))
		return
	}

	cmdPath, err := sofficeCmd()
	if err != nil {
		log.Printf("LibreOffice warm-up: skipped (%v)", err)
		return
	}
	cmd := exec.CommandContext(ctx, cmdPath, "--headless", "--convert-to", "pdf", "--outdir", outDir, tmpFile.Name())
	if out, err := cmd.CombinedOutput(); err != nil {
		log.Printf("LibreOffice warm-up: conversion failed in %v: %v; output=%s", time.Since(start), err, string(out))
		return
	}
	log.Printf("LibreOffice warm-up complete (soffice direct) in %v", time.Since(start))
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
	cmdPath, err := pdftoppmCmd()
	if err != nil {
		log.Printf("[WARN] slides: pdftoppm unavailable: %v", err)
		return err
	}
	cmd := exec.CommandContext(ctx, cmdPath, "-png", "-r", dpiStr, "-f", pageStr, "-l", pageStr, pdfPath, outPrefix)
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
	cmdPath, err := pdftoppmCmd()
	if err != nil {
		log.Printf("[WARN] slides: pdftoppm unavailable: %v", err)
		return err
	}
	cmd := exec.CommandContext(ctx, cmdPath, "-png", "-r", dpiStr, pdfPath, outPrefix)
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
// Step 1 converts to PDF (via unoconv when available, falling back to soffice --headless).
// Step 2 uses pdftoppm to rasterise each PDF page to PNG.
// Callers are responsible for treating failures as best-effort only.
func ConvertSlidesToPNGsWithLibreOffice(srcPath string) ([]ConvertedSlide, error) {
	overallStart := time.Now()

	// Validate that at least one converter is available before acquiring the
	// semaphore. We need to know about both up front because the unoconv path
	// can flake on cold starts or specific .pptx files; if it does, we want to
	// fall back to soffice instead of failing the whole pipeline (the doc on
	// this function has always promised that fallback — the implementation
	// just didn't honor it).
	unoconvActive := useUnoconv()
	_, sofficeAvailErr := sofficeCmd()
	if !unoconvActive && sofficeAvailErr != nil {
		return nil, sofficeAvailErr
	}

	// Serialize slide conversions to avoid resource contention on constrained platforms.
	semWait := time.Now()
	slideConversionSem <- struct{}{}
	if d := time.Since(semWait); d > 200*time.Millisecond {
		log.Printf("slides timing: waited %v for conversion slot (queue) src=%s", d, srcPath)
	}
	defer func() { <-slideConversionSem }()

	var tmpDir string
	var mkdirErr error
	if os.Getenv("TALKBACK_SOFFICE_CMD") != "" {
		base := uploadRootForTemp()
		if err := os.MkdirAll(base, 0755); err != nil {
			return nil, fmt.Errorf("create temp base for soffice: %w", err)
		}
		tmpDir, mkdirErr = os.MkdirTemp(base, "talkback-slides-*")
	} else {
		tmpDir, mkdirErr = os.MkdirTemp("", "talkback-slides-*")
	}
	if mkdirErr != nil {
		return nil, mkdirErr
	}
	defer os.RemoveAll(tmpDir)

	// Step 1: PPT/PPTX → PDF
	// unoconv reuses a persistent LibreOffice listener (~0.5-1s); soffice spawns a fresh process (~8-10s).
	sofficeTimeout := 3 * time.Minute
	if s := strings.TrimSpace(os.Getenv("TALKBACK_SOFFICE_TIMEOUT")); s != "" {
		if seconds, parseErr := strconv.Atoi(s); parseErr == nil && seconds > 0 {
			sofficeTimeout = time.Duration(seconds) * time.Second
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), sofficeTimeout)
	defer cancel()

	tConv := time.Now()
	var pdfPath string
	if unoconvActive {
		var err error
		pdfPath, err = convertWithUnoconv(ctx, srcPath, tmpDir)
		if err != nil {
			// SCRUM-X: unoconv flaked. Common after a cold start, after the
			// LibreOffice listener crashed, or on .pptx files that trigger a
			// known unoconv parse bug (the warm-up logs "unoconv did not
			// produce expected PDF" — the same path runs here on real
			// uploads). Fall through to a fresh soffice rather than failing
			// the whole pipeline. Costs ~8-10s on first invocation but
			// avoids the user-visible Failed status SCRUM-287's retry
			// surfaced.
			if sofficeAvailErr != nil {
				return nil, fmt.Errorf("slides conversion failed: unoconv errored (%w) and soffice is unavailable: %v", err, sofficeAvailErr)
			}
			log.Printf("[WARN] slides: unoconv failed (%v); falling back to soffice for %s", err, srcPath)
			unoconvActive = false
		} else {
			log.Printf("slides timing: unoconv pptx→pdf %v src=%s", time.Since(tConv), srcPath)
		}
	}
	if !unoconvActive {
		soffStart := time.Now()
		cmdPath, _ := sofficeCmd() // validated above (sofficeAvailErr was nil)
		cmd := exec.CommandContext(ctx, cmdPath, "--headless", "--convert-to", "pdf", "--outdir", tmpDir, srcPath)
		output, err := cmd.CombinedOutput()
		if err != nil {
			if ctx.Err() == context.DeadlineExceeded {
				return nil, fmt.Errorf("soffice conversion to PDF timed out after %s", sofficeTimeout)
			}
			return nil, fmt.Errorf("soffice conversion to PDF failed: %w; output=%s", err, string(output))
		}
		baseName := strings.TrimSuffix(filepath.Base(srcPath), filepath.Ext(srcPath))
		pdfPath = filepath.Join(tmpDir, baseName+".pdf")
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
		log.Printf("slides timing: soffice pptx→pdf %v src=%s", time.Since(soffStart), srcPath)
	}

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

// ConvertPPTXToPDFWithLibreOffice converts srcPath to a single PDF on disk via the
// same Step 1 (unoconv-or-soffice) used by ConvertSlidesToPNGsWithLibreOffice. The
// pdftoppm rasterisation step is intentionally omitted — under the PDF pipeline
// (SCRUM-444) the SPA renders pages on demand via PDF.js, eliminating the
// per-deck pdftoppm memory peak (~150-200 MB on Render Starter).
//
// On success the caller receives:
//   - pdfPath:    absolute path to the produced PDF (caller streams to storage).
//   - slideCount: page count from pdfinfo, or 0 if pdfinfo is unavailable.
//   - cleanup:    closes/removes the temp dir holding pdfPath; always non-nil
//     when err == nil. Callers should defer cleanup() immediately.
//
// The function honors the same TALKBACK_SOFFICE_TIMEOUT, TALKBACK_SOFFICE_CMD,
// TALKBACK_USE_UNOCONV, and TALKBACK_UPLOAD_ROOT envs as the legacy converter so
// ops have one consistent surface across pipelines.
func ConvertPPTXToPDFWithLibreOffice(srcPath string) (string, int, func(), error) {
	overallStart := time.Now()

	unoconvActive := useUnoconv()
	_, sofficeAvailErr := sofficeCmd()
	if !unoconvActive && sofficeAvailErr != nil {
		return "", 0, nil, sofficeAvailErr
	}

	semWait := time.Now()
	slideConversionSem <- struct{}{}
	if d := time.Since(semWait); d > 200*time.Millisecond {
		log.Printf("slides timing: waited %v for conversion slot (queue) src=%s", d, srcPath)
	}
	// Hold the semaphore until cleanup runs so concurrent PPTX uploads still
	// serialise through soffice (the converter cost is what we cap, not the
	// caller's R2 upload).
	releaseSem := func() { <-slideConversionSem }

	var tmpDir string
	var mkdirErr error
	if os.Getenv("TALKBACK_SOFFICE_CMD") != "" {
		base := uploadRootForTemp()
		if err := os.MkdirAll(base, 0755); err != nil {
			releaseSem()
			return "", 0, nil, fmt.Errorf("create temp base for soffice: %w", err)
		}
		tmpDir, mkdirErr = os.MkdirTemp(base, "talkback-pdfslides-*")
	} else {
		tmpDir, mkdirErr = os.MkdirTemp("", "talkback-pdfslides-*")
	}
	if mkdirErr != nil {
		releaseSem()
		return "", 0, nil, mkdirErr
	}
	cleanup := func() {
		_ = os.RemoveAll(tmpDir)
		releaseSem()
	}

	sofficeTimeout := 3 * time.Minute
	if s := strings.TrimSpace(os.Getenv("TALKBACK_SOFFICE_TIMEOUT")); s != "" {
		if seconds, parseErr := strconv.Atoi(s); parseErr == nil && seconds > 0 {
			sofficeTimeout = time.Duration(seconds) * time.Second
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), sofficeTimeout)
	defer cancel()

	tConv := time.Now()
	var pdfPath string
	if unoconvActive {
		var err error
		pdfPath, err = convertWithUnoconv(ctx, srcPath, tmpDir)
		if err != nil {
			if sofficeAvailErr != nil {
				cleanup()
				return "", 0, nil, fmt.Errorf("slides conversion failed: unoconv errored (%w) and soffice is unavailable: %v", err, sofficeAvailErr)
			}
			log.Printf("[WARN] slides: unoconv failed (%v); falling back to soffice for %s", err, srcPath)
			unoconvActive = false
		} else {
			log.Printf("slides timing: unoconv pptx→pdf %v src=%s", time.Since(tConv), srcPath)
		}
	}
	if !unoconvActive {
		soffStart := time.Now()
		cmdPath, _ := sofficeCmd()
		cmd := exec.CommandContext(ctx, cmdPath, "--headless", "--convert-to", "pdf", "--outdir", tmpDir, srcPath)
		output, err := cmd.CombinedOutput()
		if err != nil {
			cleanup()
			if ctx.Err() == context.DeadlineExceeded {
				return "", 0, nil, fmt.Errorf("soffice conversion to PDF timed out after %s", sofficeTimeout)
			}
			return "", 0, nil, fmt.Errorf("soffice conversion to PDF failed: %w; output=%s", err, string(output))
		}
		baseName := strings.TrimSuffix(filepath.Base(srcPath), filepath.Ext(srcPath))
		pdfPath = filepath.Join(tmpDir, baseName+".pdf")
		if _, err := os.Stat(pdfPath); err != nil {
			candidates, _ := filepath.Glob(filepath.Join(tmpDir, baseName+".*"))
			resolved := ""
			for _, c := range candidates {
				if strings.EqualFold(filepath.Ext(c), ".pdf") {
					resolved = c
					break
				}
			}
			if resolved == "" {
				cleanup()
				return "", 0, nil, fmt.Errorf("soffice did not produce expected PDF %s: %w", pdfPath, err)
			}
			pdfPath = resolved
		}
		log.Printf("slides timing: soffice pptx→pdf %v src=%s", time.Since(soffStart), srcPath)
	}

	slideCount := pdfPageCount(ctx, pdfPath)
	log.Printf("slides timing: pptx→pdf total %v pages=%d src=%s", time.Since(overallStart), slideCount, srcPath)
	return pdfPath, slideCount, cleanup, nil
}

