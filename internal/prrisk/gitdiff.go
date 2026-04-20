package prrisk

import (
	"bytes"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// headRef returns symbolic HEAD for diff range (best effort).
func headRef(repoRoot string) string {
	out, err := exec.Command("git", "-C", repoRoot, "rev-parse", "--symbolic-full-name", "HEAD").Output()
	if err != nil {
		return "HEAD"
	}
	s := strings.TrimSpace(string(out))
	if s == "" {
		return "HEAD"
	}
	return s
}

// DiffNumstat runs git diff --numstat base...HEAD.
func DiffNumstat(repoRoot, baseRef string) ([]FileChange, string) {
	var stdout, stderr bytes.Buffer
	cmd := exec.Command("git", "-C", repoRoot, "diff", "--numstat", fmt.Sprintf("%s...HEAD", baseRef))
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		stdout.Reset()
		stderr.Reset()
		cmd2 := exec.Command("git", "-C", repoRoot, "diff", "--numstat", baseRef)
		cmd2.Stdout = &stdout
		cmd2.Stderr = &stderr
		if err2 := cmd2.Run(); err2 != nil {
			msg := strings.TrimSpace(stderr.String())
			if msg == "" {
				msg = err2.Error()
			}
			return nil, msg
		}
	}
	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	var files []FileChange
	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) < 3 {
			continue
		}
		added, del := 0, 0
		if parts[0] != "-" {
			added, _ = strconv.Atoi(parts[0])
		}
		if parts[1] != "-" {
			del, _ = strconv.Atoi(parts[1])
		}
		path := strings.TrimSpace(parts[2])
		// Handle rename "path => newpath"
		if idx := strings.LastIndex(path, "\t"); idx >= 0 {
			path = strings.TrimSpace(path[idx+1:])
		}
		path = filepath.ToSlash(path)
		files = append(files, FileChange{Path: path, Added: added, Deleted: del})
	}
	return files, ""
}

// ExtractSignals builds Signals from git diff.
func ExtractSignals(repoRoot, baseRef string) Signals {
	repoRoot = filepath.Clean(repoRoot)
	files, gitErr := DiffNumstat(repoRoot, baseRef)

	valFound, valSnippet := detectValidationNote(repoRoot, baseRef)
	styleOnly, styleSnippet := detectStyleOnlyNote(repoRoot, baseRef)

	s := Signals{
		RepoRoot:              repoRoot,
		BaseRef:               baseRef,
		HeadRef:               headRef(repoRoot),
		DomainHits:            make(map[string]int),
		TestDomainHits:        make(map[string]int),
		TestUnitDomainHits:    make(map[string]int),
		TestE2EDomainHits:     make(map[string]int),
		Files:                 files,
		GitError:              gitErr,
		ValidationNoteFound:   valFound,
		ValidationNoteSnippet: valSnippet,
		StyleOnlyNoteFound:    styleOnly,
		StyleOnlyNoteSnippet:  styleSnippet,
	}
	for _, f := range files {
		s.FileCount++
		s.TotalAdded += f.Added
		s.TotalDeleted += f.Deleted
		d := ClassifyDomain(f.Path)
		s.DomainHits[d]++
		if IsTestPath(f.Path) {
			s.TestFiles++
			td := ClassifyArea(f.Path)
			s.TestDomainHits[td]++
			if IsE2EPath(f.Path) {
				s.E2ETestFiles++
				s.TestE2EDomainHits[td]++
			} else {
				s.UnitTestFiles++
				s.TestUnitDomainHits[td]++
			}
		}
		if IsConfigPath(f.Path) {
			s.ConfigFiles++
		}
		if IsMigrationPath(f.Path) {
			s.MigrationFiles++
		}
	}
	s.TotalLOC = s.TotalAdded + s.TotalDeleted
	if s.TotalLOC > 0 && s.TestFiles > 0 {
		testLOC := 0
		for _, f := range files {
			if IsTestPath(f.Path) {
				testLOC += f.Added + f.Deleted
			}
		}
		s.TestLOCRatio = float64(testLOC) / float64(s.TotalLOC)
	}
	return s
}

func detectValidationNote(repoRoot, baseRef string) (bool, string) {
	// Mirrors ops/release-readiness.py behavior but stays deterministic and scoped to this diff range.
	// We look for a line beginning with `Validation:` or `Validate:` (case-insensitive).
	cmd := exec.Command("git", "-C", repoRoot, "log", "--format=%B", fmt.Sprintf("%s...HEAD", baseRef))
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return false, ""
	}
	lines := strings.Split(stdout.String(), "\n")
	for _, line := range lines {
		stripped := strings.TrimSpace(line)
		if stripped == "" {
			continue
		}
		lower := strings.ToLower(stripped)
		if strings.HasPrefix(lower, "validation:") || strings.HasPrefix(lower, "validate:") {
			if len(stripped) > 120 {
				return true, stripped[:120]
			}
			return true, stripped
		}
	}
	_ = stderr // keep stderr for future debugging without changing behavior.
	return false, ""
}

// detectStyleOnlyNote checks commit messages for a "Style-only:" prefix, indicating
// a purely cosmetic/CSS change with no logic impact. Mirrors detectValidationNote.
func detectStyleOnlyNote(repoRoot, baseRef string) (bool, string) {
	cmd := exec.Command("git", "-C", repoRoot, "log", "--format=%B", fmt.Sprintf("%s...HEAD", baseRef))
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return false, ""
	}
	lines := strings.Split(stdout.String(), "\n")
	for _, line := range lines {
		stripped := strings.TrimSpace(line)
		if stripped == "" {
			continue
		}
		lower := strings.ToLower(stripped)
		if strings.HasPrefix(lower, "style-only:") || strings.HasPrefix(lower, "style only:") {
			if len(stripped) > 120 {
				return true, stripped[:120]
			}
			return true, stripped
		}
	}
	_ = stderr
	return false, ""
}
