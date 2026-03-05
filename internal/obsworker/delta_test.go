package obsworker

import (
	"strings"
	"testing"
)

func TestComputeDelta_FirstRun(t *testing.T) {
	th := DefaultThresholds()
	curr := BaselineMetrics{TxnN: 50, P95ms: 100, ReqPerMin: 5, Errors: 0}
	d := ComputeDelta(nil, curr, th, nil, Config{}, "")
	if d.Status != "GREEN" {
		t.Errorf("first run status: got %s, want GREEN", d.Status)
	}
	if d.Confidence != "MED" {
		t.Errorf("first run confidence: got %s, want MED (n=50)", d.Confidence)
	}
	if len(d.Reasons) != 1 || d.Reasons[0] != "First run (no baseline)" {
		t.Errorf("first run reasons: got %v", d.Reasons)
	}
	lines := DeltaSummaryLines(d)
	if len(lines) != 0 {
		t.Errorf("first run delta lines should be empty/N/A: got %v", lines)
	}
}

func TestComputeDelta_ErrorsIncrease(t *testing.T) {
	th := DefaultThresholds()
	prev := &Baseline{Metrics: BaselineMetrics{TxnN: 100, Errors: 0}}
	curr := BaselineMetrics{TxnN: 100, P95ms: 100, ReqPerMin: 5, Errors: 3}
	d := ComputeDelta(prev, curr, th, nil, Config{}, "")
	if d.Status != "RED" {
		t.Errorf("errors increase: got status %s, want RED", d.Status)
	}
	found := false
	for _, r := range d.Reasons {
		if r == "errors increased (0 → 3)" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected reason 'errors increased (0 → 3)', got %v", d.Reasons)
	}
}

func TestComputeDelta_P95Plus60Red(t *testing.T) {
	th := DefaultThresholds()
	prev := &Baseline{Metrics: BaselineMetrics{TxnN: 100, P95ms: 100, ReqPerMin: 5, Errors: 0}}
	curr := BaselineMetrics{TxnN: 100, P95ms: 160, ReqPerMin: 5, Errors: 0}
	d := ComputeDelta(prev, curr, th, nil, Config{}, "")
	if d.Status != "RED" {
		t.Errorf("p95 +60%%: got status %s, want RED", d.Status)
	}
	if d.Confidence != "HIGH" {
		t.Errorf("p95 +60%%: got confidence %s", d.Confidence)
	}
}

func TestComputeDelta_P95Plus25Yellow(t *testing.T) {
	th := DefaultThresholds()
	prev := &Baseline{Metrics: BaselineMetrics{TxnN: 100, P95ms: 100, ReqPerMin: 5, Errors: 0}}
	curr := BaselineMetrics{TxnN: 100, P95ms: 125, ReqPerMin: 5, Errors: 0}
	d := ComputeDelta(prev, curr, th, nil, Config{}, "")
	if d.Status != "YELLOW" {
		t.Errorf("p95 +25%%: got status %s, want YELLOW", d.Status)
	}
}

func TestComputeDelta_ReqMinDrop40Yellow(t *testing.T) {
	th := DefaultThresholds()
	prev := &Baseline{Metrics: BaselineMetrics{TxnN: 100, P95ms: 100, ReqPerMin: 10, Errors: 0}}
	curr := BaselineMetrics{TxnN: 100, P95ms: 100, ReqPerMin: 6, Errors: 0}
	d := ComputeDelta(prev, curr, th, nil, Config{}, "")
	if d.Status != "YELLOW" {
		t.Errorf("req/min -40%%: got status %s, want YELLOW", d.Status)
	}
}

func TestComputeDelta_LowConfidencePreventsP95Yellow(t *testing.T) {
	th := DefaultThresholds()
	prev := &Baseline{Metrics: BaselineMetrics{TxnN: 10, P95ms: 100, ReqPerMin: 5, Errors: 0}}
	curr := BaselineMetrics{TxnN: 15, P95ms: 125, ReqPerMin: 5, Errors: 0}
	d := ComputeDelta(prev, curr, th, nil, Config{}, "")
	// n=15 is LOW confidence; p95 +25% should NOT trigger YELLOW
	if d.Status != "GREEN" {
		t.Errorf("low confidence: got status %s, want GREEN (p95 rule should not fire)", d.Status)
	}
	if d.Confidence != "LOW" {
		t.Errorf("low confidence: got %s, want LOW", d.Confidence)
	}
	found := false
	for _, r := range d.Reasons {
		if strings.Contains(r, "LOW CONFIDENCE") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected LOW CONFIDENCE reason, got %v", d.Reasons)
	}
}

func TestComputeDelta_GreenNoChange(t *testing.T) {
	th := DefaultThresholds()
	prev := &Baseline{Metrics: BaselineMetrics{TxnN: 100, P95ms: 100, ReqPerMin: 5, Errors: 0}}
	curr := BaselineMetrics{TxnN: 100, P95ms: 100, ReqPerMin: 5, Errors: 0}
	d := ComputeDelta(prev, curr, th, nil, Config{}, "")
	if d.Status != "GREEN" {
		t.Errorf("no change: got status %s, want GREEN", d.Status)
	}
}

func TestDeltaSummaryLines(t *testing.T) {
	prev := BaselineMetrics{TxnN: 80, P95ms: 120, ReqPerMin: 4.2, Errors: 0}
	curr := BaselineMetrics{TxnN: 92, P95ms: 157.2, ReqPerMin: 3.1, Errors: 0}
	p95 := 31.0
	reqPct := -26.190476
	errAbs := 0
	nAbs := 12
	d := Delta{
		Current:  curr,
		Previous: prev,
		Changes: DeltaChanges{
			P95Pct:       &p95,
			ReqPerMinPct: &reqPct,
			ErrorsAbs:    &errAbs,
			TxnNAbs:      &nAbs,
		},
	}
	lines := DeltaSummaryLines(d)
	if len(lines) < 4 {
		t.Errorf("expected 4 delta lines, got %d: %v", len(lines), lines)
	}
}

func TestComputeDelta_ExpectedAuth401NoThresholdExceeded(t *testing.T) {
	th := DefaultThresholds()
	cfg := Config{AuthUserThreshold: 5, AuthErrorMessage: "Unauthorized", AuthErrorClass: "401"}
	prev := &Baseline{Metrics: BaselineMetrics{TxnN: 100, Errors: 2}}
	curr := BaselineMetrics{TxnN: 100, P95ms: 100, ReqPerMin: 5, Errors: 2}
	bundle := &Bundle{
		Results: []QueryResult{
			{Name: "top_error_classes", Results: []map[string]interface{}{{"facet": "401", "count": 2.0}}},
			{Name: "auth_failures_by_username", Results: []map[string]interface{}{{"auth.username": "u1", "count": 2.0}}},
		},
	}
	d := ComputeDelta(prev, curr, th, bundle, cfg, "")
	// 2 errors, both 401, no user above threshold 5 -> GREEN, expected noise
	if d.Status != "GREEN" {
		t.Errorf("expected auth below threshold: got status %s, want GREEN", d.Status)
	}
	if d.ExpectedAuth401 != 2 {
		t.Errorf("expected ExpectedAuth401=2, got %d", d.ExpectedAuth401)
	}
	if d.AuthThresholdExceeded {
		t.Error("AuthThresholdExceeded should be false when no user >= 5")
	}
	found := false
	for _, r := range d.Reasons {
		if strings.Contains(r, "Expected auth failures") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected 'Expected auth failures' in reasons, got %v", d.Reasons)
	}
}

func TestComputeDelta_AuthThresholdExceededYellow(t *testing.T) {
	th := DefaultThresholds()
	cfg := Config{AuthUserThreshold: 5, AuthErrorMessage: "Unauthorized", AuthErrorClass: "401"}
	prev := &Baseline{Metrics: BaselineMetrics{TxnN: 100, Errors: 10}}
	curr := BaselineMetrics{TxnN: 100, P95ms: 100, ReqPerMin: 5, Errors: 10}
	bundle := &Bundle{
		Results: []QueryResult{
			{Name: "top_error_classes", Results: []map[string]interface{}{{"facet": "401", "count": 10.0}}},
			{Name: "auth_failures_by_username", Results: []map[string]interface{}{{"auth.username": "attacker", "count": 8.0}}},
		},
	}
	d := ComputeDelta(prev, curr, th, bundle, cfg, "")
	if d.Status != "YELLOW" {
		t.Errorf("auth threshold exceeded: got status %s, want YELLOW", d.Status)
	}
	if !d.AuthThresholdExceeded {
		t.Error("AuthThresholdExceeded should be true")
	}
	if len(d.AuthHotspots) != 1 || d.AuthHotspots[0].Count != 8 {
		t.Errorf("expected one hotspot count=8, got %v", d.AuthHotspots)
	}
}

func TestComputeDelta_AuthThresholdExceededRed(t *testing.T) {
	th := DefaultThresholds()
	cfg := Config{AuthUserThreshold: 5, AuthErrorMessage: "Unauthorized", AuthErrorClass: "401"}
	prev := &Baseline{Metrics: BaselineMetrics{TxnN: 100, Errors: 20}}
	curr := BaselineMetrics{TxnN: 100, P95ms: 100, ReqPerMin: 5, Errors: 20}
	// 3 * threshold = 15; user with 20 -> RED
	bundle := &Bundle{
		Results: []QueryResult{
			{Name: "top_error_classes", Results: []map[string]interface{}{{"facet": "401", "count": 20.0}}},
			{Name: "auth_failures_by_username", Results: []map[string]interface{}{{"auth.username": "attacker", "count": 20.0}}},
		},
	}
	d := ComputeDelta(prev, curr, th, bundle, cfg, "")
	if d.Status != "RED" {
		t.Errorf("auth 3x threshold exceeded: got status %s, want RED", d.Status)
	}
}

func TestRecommendedNextQueries(t *testing.T) {
	// RED with errors
	dErr := Delta{Status: "RED", Current: BaselineMetrics{Errors: 2}}
	qErr := RecommendedNextQueries(dErr)
	if len(qErr) == 0 {
		t.Error("RED with errors should recommend queries")
	}
	// YELLOW with p95
	p95 := 25.0
	dP95 := Delta{Status: "YELLOW", Changes: DeltaChanges{P95Pct: &p95}}
	qP95 := RecommendedNextQueries(dP95)
	if len(qP95) == 0 {
		t.Error("YELLOW with p95 increase should recommend queries")
	}
}
