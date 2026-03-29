package prrisk

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// SemanticPRRiskFile is the stable machine-readable summary for CI (artifacts/pr-risk.json).
// Field names are fixed for downstream parsers and the GitHub Checks integration.
type SemanticPRRiskFile struct {
	ReportVersion       string   `json:"report_version"`
	Score               float64  `json:"score"`
	Band                string   `json:"band"`
	MergeRecommendation string   `json:"merge_recommendation"` // PASS | WARN | BLOCK
	RequiredValidations []string `json:"required_validations"`
	TopRiskFactors      []string `json:"top_risk_factors,omitempty"`
}

const semanticTopFactors = 5

// WriteSemanticPRRiskJSON writes artifacts/pr-risk.json beside the release-readiness output dir:
// if output-dir is "artifacts/release-readiness", the file is "artifacts/pr-risk.json".
func WriteSemanticPRRiskJSON(path string, r Result) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	enf := r.Enforcement
	rec := strings.ToUpper(strings.TrimSpace(enf.MergeRecommendation))
	if rec == "" {
		rec = "UNKNOWN"
	}
	top := topFactorLabels(r.Factors, semanticTopFactors)
	payload := SemanticPRRiskFile{
		ReportVersion:       r.ReportVersion,
		Score:               r.RiskScore,
		Band:                r.RiskBand,
		MergeRecommendation: rec,
		RequiredValidations: append([]string(nil), enf.RequiredValidations...),
		TopRiskFactors:      top,
	}
	b, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0644)
}

func topFactorLabels(factors []RiskFactor, n int) []string {
	if len(factors) == 0 || n <= 0 {
		return nil
	}
	cp := append([]RiskFactor(nil), factors...)
	sort.Slice(cp, func(i, j int) bool {
		if cp[i].Points == cp[j].Points {
			return cp[i].ID < cp[j].ID
		}
		return cp[i].Points > cp[j].Points
	})
	if len(cp) > n {
		cp = cp[:n]
	}
	out := make([]string, 0, len(cp))
	for _, f := range cp {
		label := strings.TrimSpace(f.Label)
		if label == "" {
			label = f.ID
		}
		out = append(out, label)
	}
	return out
}
