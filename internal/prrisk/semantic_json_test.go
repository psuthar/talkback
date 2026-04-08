package prrisk

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWriteSemanticPRRiskJSON(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	out := filepath.Join(dir, "artifacts", "pr-risk.json")
	r := Result{
		ReportVersion: ReportVersionString(),
		GeneratedAt:   time.Unix(0, 0).UTC(),
		RiskScore:     28,
		RiskBand:      "medium",
		Factors: []RiskFactor{
			{ID: "a", Label: "Alpha", Points: 10},
			{ID: "b", Label: "Beta", Points: 5},
		},
		Enforcement: Enforcement{
			MergeRecommendation: "warn",
			RequiredValidations: []string{"ci", "config", "test", "process"},
		},
	}
	if err := WriteSemanticPRRiskJSON(out, r); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var got SemanticPRRiskFile
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got.MergeRecommendation != "WARN" {
		t.Fatalf("merge rec: %q", got.MergeRecommendation)
	}
	if got.Score != 28 || got.Band != "medium" {
		t.Fatalf("score/band: %+v", got)
	}
	if len(got.RequiredValidations) != 4 {
		t.Fatalf("validations: %#v", got.RequiredValidations)
	}
	if len(got.TopRiskFactors) != 2 {
		t.Fatalf("top factors: %#v", got.TopRiskFactors)
	}
}

func TestTopFactorLabelsOrder(t *testing.T) {
	t.Parallel()
	fs := []RiskFactor{
		{ID: "low", Label: "Low", Points: 1},
		{ID: "hi", Label: "High", Points: 99},
		{ID: "mid", Label: "Mid", Points: 50},
	}
	got := topFactorLabels(fs, 2)
	if len(got) != 2 || got[0] != "High" || got[1] != "Mid" {
		t.Fatalf("got %#v", got)
	}
}
