package riskcontext

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

// AnalyzeConcentration measures whether churn is focused in a few areas or scattered.
func AnalyzeConcentration(in Input) ConcentrationInsight {
	if len(in.Files) == 0 {
		return ConcentrationInsight{Mode: "balanced", Detail: "no files"}
	}

	locByPrefix := make(map[string]int)
	for _, f := range in.Files {
		p := filepath.ToSlash(strings.TrimSpace(f.Path))
		prefix := twoSegmentPrefix(p)
		loc := f.Added + f.Deleted
		locByPrefix[prefix] += loc
	}

	total := 0
	for _, v := range locByPrefix {
		total += v
	}
	if total == 0 {
		return ConcentrationInsight{Mode: "balanced", UniqueDirs: len(locByPrefix), Detail: "no LOC churn"}
	}

	var prefs []string
	for k := range locByPrefix {
		prefs = append(prefs, k)
	}
	sort.Slice(prefs, func(i, j int) bool {
		return locByPrefix[prefs[i]] > locByPrefix[prefs[j]]
	})

	top := prefs[0]
	topShare := float64(locByPrefix[top]) / float64(total)

	const focusedLargeLOC = 2000 // align with very-large diff scale; concentrated churn in one area

	// Herfindahl index on LOC shares
	hhi := 0.0
	for _, v := range locByPrefix {
		s := float64(v) / float64(total)
		hhi += s * s
	}

	mode := "balanced"
	detail := "Churn is spread across several areas in a typical way."
	if len(locByPrefix) >= 6 && hhi < 0.18 && len(in.Files) >= 10 {
		mode = "scattered"
		detail = fmt.Sprintf("Churn is spread across %d top-level areas (HHI=%.2f); review may be harder to scope.", len(locByPrefix), hhi)
	} else if topShare >= 0.55 && len(locByPrefix) <= 4 {
		mode = "focused"
		detail = fmt.Sprintf("Most churn (~%.0f%%) sits under `%s`.", topShare*100, top)
		if total >= focusedLargeLOC {
			mode = "focused_large"
			detail = fmt.Sprintf(
				"Large concentrated churn (~%d LOC); ~%.0f%% sits under `%s` — prioritize regression around this area.",
				total, topShare*100, top,
			)
		}
	}

	return ConcentrationInsight{
		Mode:       mode,
		TopPrefix:  top,
		TopShare:   topShare,
		HHI:        hhi,
		UniqueDirs: len(locByPrefix),
		Detail:     detail,
	}
}

func twoSegmentPrefix(p string) string {
	p = strings.Trim(p, "/")
	parts := strings.Split(p, "/")
	if len(parts) == 0 {
		return "."
	}
	if len(parts) == 1 {
		return parts[0]
	}
	return parts[0] + "/" + parts[1]
}
