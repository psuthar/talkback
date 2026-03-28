package riskcontext

import (
	"path/filepath"
	"strings"
)

// AnalyzeProximity scores whether tests in the diff sit near the production code they exercise.
func AnalyzeProximity(in Input) ProximityInsight {
	if len(in.Files) == 0 || len(in.IsTest) != len(in.Files) {
		return ProximityInsight{Mode: "n_a", Detail: "no files to analyze"}
	}

	var nonTestPaths []string
	var testPaths []string
	for i, f := range in.Files {
		if i >= len(in.IsTest) {
			break
		}
		if in.IsTest[i] {
			testPaths = append(testPaths, f.Path)
		} else {
			nonTestPaths = append(nonTestPaths, f.Path)
		}
	}

	if len(nonTestPaths) == 0 {
		return ProximityInsight{Mode: "n_a", Detail: "only test files in diff"}
	}
	if len(testPaths) == 0 {
		return ProximityInsight{
			Mode:                 "distant",
			NonTestFiles:         len(nonTestPaths),
			WithNearbyTestInDiff: 0,
			Ratio:                0,
			Detail:               "No test files in diff; proximity of tests to changed code cannot be established from this diff alone.",
		}
	}

	var withNearby int
	for _, p := range nonTestPaths {
		if hasNearbyTestInDiff(p, testPaths) {
			withNearby++
		}
	}

	ratio := float64(withNearby) / float64(len(nonTestPaths))
	mode := "partial"
	switch {
	case ratio >= 0.75:
		mode = "co_located"
	case ratio >= 0.35:
		mode = "partial"
	default:
		mode = "distant"
	}

	detail := "Tests in this diff are mostly next to or under the same directories as changed production files."
	if mode == "distant" {
		detail = "Many changed production files have no test file in the same directory or an obvious sibling path in this diff."
	} else if mode == "partial" {
		detail = "Some production changes lack adjacent tests in the same diff; spot-check coverage."
	}

	return ProximityInsight{
		Mode:                 mode,
		NonTestFiles:         len(nonTestPaths),
		WithNearbyTestInDiff: withNearby,
		Ratio:                ratio,
		Detail:               detail,
	}
}

func hasNearbyTestInDiff(codePath string, testPaths []string) bool {
	codePath = filepath.ToSlash(strings.TrimSpace(codePath))
	dir := filepath.ToSlash(filepath.Dir(codePath))

	for _, t := range testPaths {
		t = filepath.ToSlash(strings.TrimSpace(t))
		if t == "" {
			continue
		}
		td := filepath.ToSlash(filepath.Dir(t))
		// Same directory (typical Go *_test.go + source)
		if td == dir {
			return true
		}
		// One package level of separation: test's dir is the direct parent of the code's dir, or vice versa.
		// Deliberately limited to one level to avoid false co-location (e.g. a test at "internal/"
		// should not be considered nearby to code at "internal/auth/handlers/").
		parentOfDir := filepath.ToSlash(filepath.Dir(dir))
		parentOfTd := filepath.ToSlash(filepath.Dir(td))
		if td == parentOfDir || dir == parentOfTd {
			return true
		}
		// Web: e2e tests apply broadly to web/ — weak signal
		if strings.Contains(t, "web/tests/e2e/") && strings.HasPrefix(codePath, "web/") {
			return true
		}
	}
	return false
}
