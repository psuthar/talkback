package utils

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
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

// ConvertSlidesToPNGsWithLibreOffice converts a local PPT/PPTX file to one PNG per slide using LibreOffice (soffice).
// It returns the converted slides in order. Callers are responsible for treating failures as best-effort only.
func ConvertSlidesToPNGsWithLibreOffice(srcPath string) ([]ConvertedSlide, error) {
	// Ensure soffice is available so errors are clear when LibreOffice is missing.
	if _, err := exec.LookPath("soffice"); err != nil {
		return nil, fmt.Errorf("soffice not found on PATH: %w", err)
	}

	tmpDir, err := os.MkdirTemp("", "talkback-slides-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmpDir)

	cmd := exec.Command(
		"soffice",
		"--headless",
		"--convert-to", "png",
		"--outdir", tmpDir,
		srcPath,
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("soffice conversion failed: %w; output=%s", err, string(output))
	}

	matches, err := filepath.Glob(filepath.Join(tmpDir, "*.png"))
	if err != nil {
		return nil, err
	}
	sort.Strings(matches)

	if len(matches) == 0 {
		return nil, fmt.Errorf("no PNG slides were produced")
	}

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

