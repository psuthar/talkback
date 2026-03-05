package obsworker

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteBundle_ProducesJSONAndMD(t *testing.T) {
	dir := t.TempDir()
	b := &Bundle{
		Metadata: BundleMetadata{
			Timestamp:  "20260102-120000",
			WindowMins: 30,
			AppName:    "TestApp",
		},
		Results: []QueryResult{
			{Name: "q1", NRQL: "SELECT 1", Results: []map[string]interface{}{}},
		},
	}

	jsonPath, mdPath, err := WriteBundle(b, dir)
	if err != nil {
		t.Fatalf("WriteBundle: %v", err)
	}

	if _, err := os.Stat(jsonPath); err != nil {
		t.Errorf("JSON file not created: %v", err)
	}
	if _, err := os.Stat(mdPath); err != nil {
		t.Errorf("MD file not created: %v", err)
	}
	if !strings.HasSuffix(jsonPath, ".json") {
		t.Errorf("jsonPath should end with .json: %s", jsonPath)
	}
	if !strings.HasSuffix(mdPath, ".md") {
		t.Errorf("mdPath should end with .md: %s", mdPath)
	}
}

func TestWriteBundle_JSONContainsRequiredKeys(t *testing.T) {
	dir := t.TempDir()
	b := &Bundle{
		Metadata: BundleMetadata{
			Timestamp:  "20260102-120000",
			WindowMins: 30,
			AppName:    "MyApp",
		},
		Results: []QueryResult{
			{Name: "throughput", NRQL: "SELECT count(*) FROM T", Results: []map[string]interface{}{{"n": 1}}},
		},
	}

	jsonPath, _, err := WriteBundle(b, dir)
	if err != nil {
		t.Fatalf("WriteBundle: %v", err)
	}

	raw, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	meta, ok := decoded["metadata"].(map[string]interface{})
	if !ok {
		t.Fatal("JSON missing metadata object")
	}
	for _, key := range []string{"timestamp", "window_minutes"} {
		if _, has := meta[key]; !has {
			t.Errorf("metadata missing key %q", key)
		}
	}
	if meta["app_name"] != "MyApp" {
		t.Errorf("metadata.app_name: got %v", meta["app_name"])
	}

	results, ok := decoded["results"].([]interface{})
	if !ok || len(results) == 0 {
		t.Fatal("JSON missing results array with at least one item")
	}
	first, ok := results[0].(map[string]interface{})
	if !ok {
		t.Fatal("first result not an object")
	}
	for _, key := range []string{"name", "nrql", "results"} {
		if _, has := first[key]; !has {
			t.Errorf("result missing key %q", key)
		}
	}
}

func TestWriteBundle_MarkdownContainsTitleQueryNamesAndNRQL(t *testing.T) {
	dir := t.TempDir()
	b := &Bundle{
		Metadata: BundleMetadata{
			Timestamp:  "20260102-120000",
			WindowMins: 30,
		},
		Results: []QueryResult{
			{Name: "throughput", NRQL: "SELECT count(*) FROM Transaction SINCE 30 minutes ago", Results: nil},
			{Name: "errors", NRQL: "SELECT count(*) FROM Transaction WHERE error IS NOT NULL", Results: nil},
		},
	}

	_, mdPath, err := WriteBundle(b, dir)
	if err != nil {
		t.Fatalf("WriteBundle: %v", err)
	}

	content, err := os.ReadFile(mdPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	md := string(content)

	if !strings.Contains(md, "# New Relic Diagnostic Bundle") {
		t.Error("markdown should contain bundle title")
	}
	if !strings.Contains(md, "## Throughput") {
		t.Error("markdown should contain title-case section Throughput")
	}
	if !strings.Contains(md, "## Errors") {
		t.Error("markdown should contain title-case section Errors")
	}
	if !strings.Contains(md, "SELECT count(*) FROM Transaction SINCE 30 minutes ago") {
		t.Error("markdown should contain NRQL string")
	}
	if !strings.Contains(md, "SELECT count(*) FROM Transaction WHERE error IS NOT NULL") {
		t.Error("markdown should contain second NRQL string")
	}
}

func TestWriteBundle_OutputInRequestedDir(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "bundles")
	b := &Bundle{
		Metadata: BundleMetadata{Timestamp: "20260102-120000", WindowMins: 30},
		Results:  []QueryResult{{Name: "q", NRQL: "SELECT 1", Results: nil}},
	}

	jsonPath, mdPath, err := WriteBundle(b, sub)
	if err != nil {
		t.Fatalf("WriteBundle: %v", err)
	}
	if !strings.HasPrefix(jsonPath, sub) {
		t.Errorf("jsonPath should be under %s: %s", sub, jsonPath)
	}
	if !strings.HasPrefix(mdPath, sub) {
		t.Errorf("mdPath should be under %s: %s", sub, mdPath)
	}
}

func TestRenderMarkdown_ContainsSummaryWhenDeltaSet(t *testing.T) {
	dir := t.TempDir()
	delta := Delta{
		Status:     "YELLOW",
		Confidence: "MED",
		Reasons:    []string{"p95_ms increased +31% (120.0 → 157.2)"},
	}
	b := &Bundle{
		Metadata: BundleMetadata{Timestamp: "20260304-101500", WindowMins: 30},
		Delta:    &delta,
		Results:  []QueryResult{{Name: "txn_summary", NRQL: "SELECT 1", Results: nil}},
	}
	_, mdPath, err := WriteBundle(b, dir)
	if err != nil {
		t.Fatalf("WriteBundle: %v", err)
	}
	content, err := os.ReadFile(mdPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	md := string(content)
	if !strings.Contains(md, "## Summary") {
		t.Error("markdown should contain ## Summary when Delta is set")
	}
	if !strings.Contains(md, "**Status:** YELLOW") {
		t.Error("markdown should contain Status: YELLOW")
	}
	if !strings.Contains(md, "## Key Deltas") {
		t.Error("markdown should contain ## Key Deltas")
	}
}

func TestRenderMarkdown_ContainsExpectedNoiseAndAuthHotspots(t *testing.T) {
	dir := t.TempDir()
	delta := Delta{
		Status:          "GREEN",
		Confidence:      "HIGH",
		Reasons:         []string{"Expected auth failures (Unauthorized 401): 3"},
		ExpectedAuth401: 3,
		AuthHotspots:    []AuthHotspot{{Username: "user@example.com", Count: 2}},
	}
	b := &Bundle{
		Metadata: BundleMetadata{Timestamp: "20260304-101500", WindowMins: 30},
		Delta:    &delta,
		Results:  []QueryResult{{Name: "txn_summary", NRQL: "SELECT 1", Results: nil}},
	}
	_, mdPath, err := WriteBundle(b, dir)
	if err != nil {
		t.Fatalf("WriteBundle: %v", err)
	}
	content, err := os.ReadFile(mdPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	md := string(content)
	if !strings.Contains(md, "## Expected Noise") {
		t.Error("markdown should contain ## Expected Noise")
	}
	if !strings.Contains(md, "## Unexpected Errors") {
		t.Error("markdown should contain ## Unexpected Errors")
	}
	if !strings.Contains(md, "## Auth Failure Hotspots") {
		t.Error("markdown should contain ## Auth Failure Hotspots")
	}
	if !strings.Contains(md, "Unauthorized (401): 3") {
		t.Error("markdown should show expected 401 count")
	}
	if !strings.Contains(md, "user@example.com: 2") {
		t.Error("markdown should show auth hotspot username and count")
	}
}

func TestWriteBundle_JSONContainsSummaryAndSimulation(t *testing.T) {
	dir := t.TempDir()
	b := &Bundle{
		Metadata:   BundleMetadata{Timestamp: "20260304-101500", WindowMins: 30, AppName: "Test"},
		Summary:    &BundleSummary{Status: "RED", Confidence: "HIGH"},
		Simulation: &Simulation{Enabled: true, ForcedStatus: "RED", Reason: "Simulated for testing"},
		Delta:      &Delta{Status: "RED", Confidence: "HIGH", Reasons: []string{"FORCED STATUS=RED"}},
		Results:    []QueryResult{{Name: "q1", NRQL: "SELECT 1", Results: nil}},
	}
	jsonPath, _, err := WriteBundle(b, dir)
	if err != nil {
		t.Fatalf("WriteBundle: %v", err)
	}
	raw, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var decoded struct {
		Summary    *struct { Status string `json:"status"`; Confidence string `json:"confidence"` } `json:"summary"`
		Simulation *struct { Enabled bool `json:"enabled"`; ForcedStatus string `json:"forced_status"`; Reason string `json:"reason"` } `json:"simulation"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if decoded.Summary == nil || decoded.Summary.Status != "RED" || decoded.Summary.Confidence != "HIGH" {
		t.Errorf("JSON summary: got %+v", decoded.Summary)
	}
	if decoded.Simulation == nil || !decoded.Simulation.Enabled || decoded.Simulation.ForcedStatus != "RED" {
		t.Errorf("JSON simulation: got %+v", decoded.Simulation)
	}
}

func TestRenderMarkdown_SimulationLineWhenForced(t *testing.T) {
	delta := Delta{
		Status:     "GREEN",
		Confidence: "HIGH",
		Reasons:    []string{"FORCED STATUS=RED", "Simulated for testing"},
	}
	b := &Bundle{
		Metadata:   BundleMetadata{Timestamp: "20260304-101500", WindowMins: 30},
		Delta:      &delta,
		Simulation: &Simulation{Enabled: true, ForcedStatus: "RED", Reason: "Simulated for testing"},
		Results:    []QueryResult{{Name: "q1", NRQL: "SELECT 1", Results: nil}},
	}
	md := b.RenderMarkdown()
	if !strings.Contains(md, "## Simulation") {
		t.Error("markdown should contain ## Simulation section when simulation enabled")
	}
	if !strings.Contains(md, "FORCED STATUS=RED") {
		t.Error("markdown should contain FORCED STATUS=RED")
	}
	if !strings.Contains(md, "**Reason:** Simulated for testing") {
		t.Error("markdown should include Reason when present")
	}
}

func TestRenderMarkdown_AutomaticDeepDiveSection(t *testing.T) {
	delta := Delta{Status: "RED", Confidence: "HIGH", Reasons: []string{"errors increased"}}
	b := &Bundle{
		Metadata: BundleMetadata{Timestamp: "20260304-101500", WindowMins: 30},
		Delta:    &delta,
		Results:  []QueryResult{{Name: "txn_summary", NRQL: "SELECT 1", Results: nil}},
		DeepDiveResults: []QueryResult{
			{Name: "latency_by_txn_p95", NRQL: "SELECT p95...", Results: []map[string]interface{}{{"name": "TxA", "p95_ms": 120.0}}},
		},
	}
	md := b.RenderMarkdown()
	if !strings.Contains(md, "## Automatic Deep Dive") {
		t.Error("markdown should contain ## Automatic Deep Dive when DeepDiveResults non-empty")
	}
	if !strings.Contains(md, "Triggered by status=RED") {
		t.Error("markdown should contain trigger line Triggered by status=RED")
	}
	if !strings.Contains(md, "Latency By Txn P95") {
		t.Error("markdown should contain deep-dive query section title")
	}
}

func TestRenderMarkdown_DeepDiveTriggeredBySimulationOverride(t *testing.T) {
	delta := Delta{
		Status:     "GREEN",
		Confidence: "HIGH",
		Reasons:    []string{"FORCED STATUS=RED", "Testing deep dive"},
	}
	b := &Bundle{
		Metadata:        BundleMetadata{Timestamp: "20260304-101500", WindowMins: 30},
		Delta:           &delta,
		Simulation:      &Simulation{Enabled: true, ForcedStatus: "RED", Reason: "Testing deep dive"},
		Results:         []QueryResult{{Name: "q1", NRQL: "SELECT 1", Results: nil}},
		DeepDiveResults: []QueryResult{{Name: "errors_by_txn", NRQL: "SELECT count...", Results: nil}},
	}
	md := b.RenderMarkdown()
	if !strings.Contains(md, "Triggered by simulation override") {
		t.Error("when simulation enabled, deep dive trigger line should say simulation override")
	}
}
