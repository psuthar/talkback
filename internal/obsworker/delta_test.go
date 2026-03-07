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

// Baseline validity: prev txn_n = 0 -> pct deltas nil, reason includes "Baseline building"
func TestComputeDelta_BaselineInvalidPrevTxnZero(t *testing.T) {
	th := DefaultThresholds()
	prev := &Baseline{Metrics: BaselineMetrics{TxnN: 0, P95ms: 0, ReqPerMin: 0, Errors: 0}}
	curr := BaselineMetrics{TxnN: 244, P95ms: 238.3, ReqPerMin: 8.13, Errors: 0}
	d := ComputeDelta(prev, curr, th, nil, Config{}, "")
	if d.BaselineValidPrev {
		t.Error("BaselineValidPrev should be false when prev txn_n=0")
	}
	if d.Changes.P95Pct != nil {
		t.Errorf("P95Pct should be nil when baseline invalid, got %v", *d.Changes.P95Pct)
	}
	if d.Changes.ReqPerMinPct != nil {
		t.Errorf("ReqPerMinPct should be nil when baseline invalid, got %v", *d.Changes.ReqPerMinPct)
	}
	if d.Changes.ErrorsAbs == nil || d.Changes.TxnNAbs == nil {
		t.Error("absolute deltas (errors, txn_n) should still be set when baseline invalid")
	}
	if *d.Changes.TxnNAbs != 244 {
		t.Errorf("txn_n abs: got %d, want 244", *d.Changes.TxnNAbs)
	}
	found := false
	for _, r := range d.Reasons {
		if strings.Contains(r, "Baseline building") && strings.Contains(r, "prev txn_n=0") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected reason containing 'Baseline building' and 'prev txn_n=0', got %v", d.Reasons)
	}
	lines := DeltaSummaryLines(d)
	for _, line := range lines {
		if strings.HasPrefix(line, "p95_ms:") {
			if !strings.Contains(line, "N/A") {
				t.Errorf("Key Deltas p95 should show N/A when baseline invalid, got %q", line)
			}
			if !strings.Contains(line, "baseline") {
				t.Errorf("Key Deltas p95 should include baseline reason when baseline invalid, got %q", line)
			}
			if !strings.Contains(line, "Δ") {
				t.Errorf("Key Deltas p95 should show absolute delta when %% suppressed, got %q", line)
			}
		}
	}
}

// prev txn_n = 10 -> pct deltas nil (baseline invalid)
func TestComputeDelta_BaselineInvalidPrevTxn10(t *testing.T) {
	th := DefaultThresholds()
	prev := &Baseline{Metrics: BaselineMetrics{TxnN: 10, P95ms: 100, ReqPerMin: 5, Errors: 0}}
	curr := BaselineMetrics{TxnN: 50, P95ms: 150, ReqPerMin: 8, Errors: 0}
	d := ComputeDelta(prev, curr, th, nil, Config{}, "")
	if d.BaselineValidPrev {
		t.Error("BaselineValidPrev should be false when prev txn_n=10")
	}
	if d.Changes.P95Pct != nil {
		t.Errorf("P95Pct should be nil when baseline invalid (txn_n=10), got %v", *d.Changes.P95Pct)
	}
	if d.Changes.ReqPerMinPct != nil {
		t.Errorf("ReqPerMinPct should be nil when baseline invalid, got %v", *d.Changes.ReqPerMinPct)
	}
	if d.Changes.TxnNAbs == nil || *d.Changes.TxnNAbs != 40 {
		t.Errorf("txn_n abs should be 40 when baseline invalid, got %v", d.Changes.TxnNAbs)
	}
}

// prev txn_n = 25 and prev p95 = 0 -> pct nil (prev metric too small)
func TestComputeDelta_ValidBaselinePrevP95Zero(t *testing.T) {
	th := DefaultThresholds()
	prev := &Baseline{Metrics: BaselineMetrics{TxnN: 25, P95ms: 0, ReqPerMin: 5, Errors: 0}}
	curr := BaselineMetrics{TxnN: 30, P95ms: 238.3, ReqPerMin: 6, Errors: 0}
	d := ComputeDelta(prev, curr, th, nil, Config{}, "")
	if !d.BaselineValidPrev {
		t.Error("BaselineValidPrev should be true when prev txn_n=25")
	}
	if d.Changes.P95Pct != nil {
		t.Errorf("P95Pct should be nil when prev p95=0 (too small), got %v", *d.Changes.P95Pct)
	}
	// req/min prev 5 > epsilon, so ReqPerMinPct should be set
	if d.Changes.ReqPerMinPct == nil {
		t.Error("ReqPerMinPct should be set when prev req/min=5 and baseline valid")
	}
}

// prev txn_n = 25 and prev p95 = 100, curr p95 = 150 -> pct = +50%
func TestComputeDelta_ValidBaselineP95Plus50(t *testing.T) {
	th := DefaultThresholds()
	prev := &Baseline{Metrics: BaselineMetrics{TxnN: 25, P95ms: 100, ReqPerMin: 10, Errors: 0}}
	curr := BaselineMetrics{TxnN: 30, P95ms: 150, ReqPerMin: 10, Errors: 0}
	d := ComputeDelta(prev, curr, th, nil, Config{}, "")
	if !d.BaselineValidPrev {
		t.Error("BaselineValidPrev should be true when prev txn_n=25")
	}
	if d.Changes.P95Pct == nil {
		t.Fatal("P95Pct should be set when baseline valid and prev p95 > epsilon")
	}
	if *d.Changes.P95Pct < 49 || *d.Changes.P95Pct > 51 {
		t.Errorf("p95 100 -> 150: got pct %.1f, want ~50", *d.Changes.P95Pct)
	}
	lines := DeltaSummaryLines(d)
	var p95Line string
	for _, line := range lines {
		if strings.HasPrefix(line, "p95_ms:") {
			p95Line = line
			break
		}
	}
	if p95Line == "" {
		t.Fatal("expected p95_ms line in Key Deltas")
	}
	if !strings.Contains(p95Line, "+50%") && !strings.Contains(p95Line, "+51%") {
		t.Errorf("p95 line should show +50%% or +51%%, got %q", p95Line)
	}
}

func TestApplySimulationOverrides(t *testing.T) {
	cfg := Config{ForceStatus: "RED", ForceReason: "Simulated for testing"}
	d := Delta{Status: "GREEN", Reasons: []string{"no change", "High auth failures..."}}
	ApplySimulationOverrides(cfg, &d)
	if d.Status != "RED" {
		t.Errorf("after override: got status %s, want RED", d.Status)
	}
	// Simulation replaces real-world reasons with simulation-only
	if len(d.Reasons) != 2 {
		t.Errorf("expected 2 reasons (FORCED + reason), got %d: %v", len(d.Reasons), d.Reasons)
	}
	if d.Reasons[0] != "FORCED STATUS=RED" {
		t.Errorf("first reason want FORCED STATUS=RED, got %q", d.Reasons[0])
	}
	if d.Reasons[1] != "Simulated for testing" {
		t.Errorf("second reason want Simulated for testing, got %q", d.Reasons[1])
	}
}

func TestDeltaSummaryLines_SmallBaselineP95SuppressesPercent(t *testing.T) {
	prev := BaselineMetrics{TxnN: 25, P95ms: 10, ReqPerMin: 5, Errors: 0}
	curr := BaselineMetrics{TxnN: 30, P95ms: 100, ReqPerMin: 6, Errors: 0}
	d := Delta{
		Current: curr, Previous: prev,
		BaselineValidPrev: true,
		Changes: DeltaChanges{
			P95PctReason: "small baseline",
			ReqPerMinPct: func() *float64 { v := 20.0; return &v }(),
			ErrorsAbs:    func() *int { v := 0; return &v }(),
			TxnNAbs:      func() *int { v := 5; return &v }(),
		},
	}
	lines := DeltaSummaryLines(d)
	var p95Line string
	for _, line := range lines {
		if strings.HasPrefix(line, "p95_ms:") {
			p95Line = line
			break
		}
	}
	if p95Line == "" {
		t.Fatal("expected p95_ms line")
	}
	if strings.Contains(p95Line, "%") && !strings.Contains(p95Line, "N/A") {
		t.Errorf("small baseline should not show raw %% (use N/A), got %q", p95Line)
	}
	if !strings.Contains(p95Line, "Δ") {
		t.Errorf("should show absolute delta, got %q", p95Line)
	}
	if !strings.Contains(p95Line, "small baseline") {
		t.Errorf("should mention small baseline, got %q", p95Line)
	}
}


func TestApplySimulationOverrides_NoOpWhenEmpty(t *testing.T) {
	cfg := Config{}
	d := Delta{Status: "YELLOW", Reasons: []string{"p95 up"}}
	ApplySimulationOverrides(cfg, &d)
	if d.Status != "YELLOW" {
		t.Errorf("status should be unchanged when ForceStatus empty, got %s", d.Status)
	}
	if len(d.Reasons) != 1 {
		t.Errorf("reasons should be unchanged, got %d", len(d.Reasons))
	}
}
