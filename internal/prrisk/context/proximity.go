package riskcontext

import (
	"path/filepath"
	"strings"
)

// AnalyzeProximity scores whether tests in the diff sit near the production code they exercise,
// and classifies behavioral coverage depth vs sensitive domains.
func AnalyzeProximity(in Input) ProximityInsight {
	if len(in.Files) == 0 || len(in.IsTest) != len(in.Files) {
		return ProximityInsight{
			Mode:                "n_a",
			StructuralAlignment: "n_a",
			BehavioralCoverage:  "unknown",
			Detail:              "no files to analyze",
		}
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
		return ProximityInsight{
			Mode:                "n_a",
			StructuralAlignment: "n_a",
			BehavioralCoverage:  "unknown",
			Detail:              "only test files in diff",
		}
	}
	if len(testPaths) == 0 {
		return ProximityInsight{
			Mode:                 "distant",
			StructuralAlignment:  "distant",
			BehavioralCoverage:   "unknown",
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

	behavioral := computeBehavioralCoverage(in, mode)
	if behavioral != "" && behavioral != "unknown" {
		detail += " " + behavioralCoverageNote(behavioral)
	}

	return ProximityInsight{
		Mode:                 mode,
		StructuralAlignment:  mode,
		BehavioralCoverage:   behavioral,
		NonTestFiles:         len(nonTestPaths),
		WithNearbyTestInDiff: withNearby,
		Ratio:                ratio,
		Detail:               detail,
	}
}

func computeBehavioralCoverage(in Input, structuralMode string) string {
	if structuralMode == "n_a" || structuralMode == "distant" {
		return "unknown"
	}
	if !hasSensitiveProductionDomains(in.DomainHits) {
		return "adequate"
	}
	if overlapsSensitiveTestDomains(in.TestE2EDomainHits, in.DomainHits) {
		return "adequate"
	}
	if overlapsSensitiveTestDomains(in.TestUnitDomainHits, in.DomainHits) {
		if structuralMode == "co_located" {
			return "shallow"
		}
		return "unknown"
	}
	return "unknown"
}

func hasSensitiveProductionDomains(h map[string]int) bool {
	if h == nil {
		return false
	}
	for _, k := range []string{"auth", "rag", "processing", "migrations", "api", "database"} {
		if h[k] > 0 {
			return true
		}
	}
	return false
}

func overlapsSensitiveTestDomains(testHits map[string]int, domainHits map[string]int) bool {
	if testHits == nil || domainHits == nil {
		return false
	}
	for _, k := range []string{"auth", "rag", "processing", "migrations", "api", "database"} {
		if domainHits[k] > 0 && testHits[k] > 0 {
			return true
		}
	}
	return false
}

func behavioralCoverageNote(depth string) string {
	switch depth {
	case "adequate":
		return "Behavioral depth: adequate for this diff’s risk class (E2E/domain overlap with sensitive areas, or non-sensitive production changes)."
	case "shallow":
		return "Behavioral depth: unit-level overlap with sensitive domains but no matching E2E evidence in this diff — consider deeper tests where applicable."
	default:
		return ""
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
		if td == dir {
			return true
		}
		parentOfDir := filepath.ToSlash(filepath.Dir(dir))
		parentOfTd := filepath.ToSlash(filepath.Dir(td))
		if td == parentOfDir || dir == parentOfTd {
			return true
		}
		if strings.Contains(t, "web/tests/e2e/") && strings.HasPrefix(codePath, "web/") {
			return true
		}
	}
	return false
}
