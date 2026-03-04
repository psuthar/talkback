package obsworker

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadBaseline_NotExists(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nonexistent.json")
	got, err := LoadBaseline(path)
	if err != nil {
		t.Fatalf("LoadBaseline(not exists): %v", err)
	}
	if got != nil {
		t.Errorf("LoadBaseline(not exists): got %+v, want nil", got)
	}
}

func TestWriteBaseline_LoadBaseline_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "latest.json")
	b := Baseline{
		Timestamp:     "20260304-101500",
		WindowMinutes: 30,
		GitSHA:        "abc1234",
		Metrics:       BaselineMetrics{TxnN: 120, P95ms: 180.2, ReqPerMin: 3.4, Errors: 0},
	}
	if err := WriteBaseline(path, b); err != nil {
		t.Fatalf("WriteBaseline: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("baseline file not created: %v", err)
	}
	loaded, err := LoadBaseline(path)
	if err != nil {
		t.Fatalf("LoadBaseline: %v", err)
	}
	if loaded == nil {
		t.Fatal("LoadBaseline: got nil")
	}
	if loaded.Timestamp != b.Timestamp || loaded.Metrics.TxnN != b.Metrics.TxnN {
		t.Errorf("round trip: got %+v", loaded)
	}
}

func TestBaselinePath(t *testing.T) {
	// Default when env unset (may be set in CI)
	path := BaselinePath()
	if path == "" {
		t.Error("BaselinePath() should not be empty")
	}
	if path != DefaultBaselinePath {
		// If OBS_BASELINE_PATH is set in env, path will differ
		t.Logf("BaselinePath() = %s (OBS_BASELINE_PATH may be set)", path)
	}
}
