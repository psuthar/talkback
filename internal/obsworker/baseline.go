package obsworker

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// BaselineMetrics holds the key metrics used for delta detection.
type BaselineMetrics struct {
	TxnN      int     `json:"txn_n"`
	P95ms     float64 `json:"p95_ms"`
	ReqPerMin float64 `json:"req_per_min"`
	Errors    int     `json:"errors"`
	AvgMs     float64 `json:"avg_ms,omitempty"`
	MaxMs     float64 `json:"max_ms,omitempty"`
}

// Baseline is a snapshot of metrics at a point in time (stored as latest.json).
type Baseline struct {
	Timestamp     string          `json:"timestamp"`
	WindowMinutes int             `json:"window_minutes"`
	GitSHA        string          `json:"git_sha,omitempty"`
	Metrics       BaselineMetrics `json:"metrics"`
}

// DefaultBaselinePath is the default path for the latest baseline file.
const DefaultBaselinePath = "ops/baselines/latest.json"

// BaselinePath returns the baseline path from env or default.
func BaselinePath() string {
	if p := os.Getenv("OBS_BASELINE_PATH"); p != "" {
		return p
	}
	return DefaultBaselinePath
}

// LoadBaseline reads a baseline from path. If the file does not exist, returns (nil, nil).
func LoadBaseline(path string) (*Baseline, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read baseline %s: %w", path, err)
	}
	return LoadBaselineFromBytes(data)
}

// LoadBaselineFromBytes parses baseline JSON from bytes (e.g. from R2 Get). Returns (nil, error) on parse error.
func LoadBaselineFromBytes(data []byte) (*Baseline, error) {
	var b Baseline
	if err := json.Unmarshal(data, &b); err != nil {
		return nil, fmt.Errorf("parse baseline: %w", err)
	}
	return &b, nil
}

// WriteBaseline writes the baseline to path.
func WriteBaseline(path string, b Baseline) error {
	data, err := json.MarshalIndent(b, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal baseline: %w", err)
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("create baseline dir: %w", err)
		}
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write baseline %s: %w", path, err)
	}
	return nil
}
