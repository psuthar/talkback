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
