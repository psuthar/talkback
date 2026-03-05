package obsworker

import (
	"fmt"
	"math"
	"strings"
)

const pctEpsilon = 0.0001

// DeltaChanges holds percent or absolute changes for key metrics.
type DeltaChanges struct {
	P95Pct      *float64 `json:"p95_pct,omitempty"`
	ReqPerMinPct *float64 `json:"req_per_min_pct,omitempty"`
	ErrorsAbs   *int     `json:"errors_abs,omitempty"`
	TxnNAbs     *int     `json:"txn_n_abs,omitempty"`
}

// AuthHotspot is a username with high 401 count.
type AuthHotspot struct {
	Username string `json:"username"`
	Count    int    `json:"count"`
}

// Delta holds current, previous, computed changes, status, confidence, reasons, and auth classification.
type Delta struct {
	Current            BaselineMetrics `json:"current"`
	Previous           BaselineMetrics `json:"previous,omitempty"`
	Changes            DeltaChanges    `json:"changes,omitempty"`
	Status             string          `json:"status"`     // GREEN | YELLOW | RED
	Confidence         string          `json:"confidence"` // LOW | MED | HIGH
	Reasons            []string        `json:"reasons"`
	ExpectedAuth401    int             `json:"expected_auth_401,omitempty"` // 401 count treated as expected noise
	AuthHotspots       []AuthHotspot   `json:"auth_hotspots,omitempty"`    // usernames exceeding threshold
	AuthThresholdExceeded bool         `json:"auth_threshold_exceeded,omitempty"`
	UnexpectedErrorSummary string      `json:"unexpected_error_summary,omitempty"` // one-line summary of non-401 errors
}

// Thresholds for status rules (editable).
type Thresholds struct {
	P95RedPct      float64 // RED if p95 increase > this (default 50)
	P95YellowPct   float64 // YELLOW if p95 increase > this (default 20)
	ThroughputDropPct float64 // YELLOW if req/min drop > this (default 30)
	LowConfidenceN int    // LOW confidence if n < this (default 20)
	MedConfidenceN int    // MED if n < this (default 100)
}

// DefaultThresholds returns the default threshold values.
func DefaultThresholds() Thresholds {
	return Thresholds{
		P95RedPct:        50,
		P95YellowPct:     20,
		ThroughputDropPct: 30,
		LowConfidenceN:   20,
		MedConfidenceN:   100,
	}
}

// confidenceFromN returns LOW | MED | HIGH based on transaction count.
func confidenceFromN(n int, t Thresholds) string {
	if n < t.LowConfidenceN {
		return "LOW"
	}
	if n < t.MedConfidenceN {
		return "MED"
	}
	return "HIGH"
}

// pctChange returns (current - previous) / max(previous, epsilon). If prev is effectively missing, returns nil.
func pctChange(current, previous float64) *float64 {
	denom := math.Max(previous, pctEpsilon)
	if previous <= 0 && current == 0 {
		return nil
	}
	pct := (current - previous) / denom * 100
	return &pct
}

// getQueryResults returns results for a query by name from the bundle.
func getQueryResults(bundle *Bundle, name string) []map[string]interface{} {
	if bundle == nil {
		return nil
	}
	for _, qr := range bundle.Results {
		if qr.Name == name {
			return qr.Results
		}
	}
	return nil
}

// count401FromBundle returns approximate 401/Unauthorized count from top_error_classes and top_errors.
func count401FromBundle(bundle *Bundle, errorClass, errorMessage string) int {
	var n int
	for _, row := range getQueryResults(bundle, "top_error_classes") {
		if fmt.Sprint(row["facet"]) == errorClass || fmt.Sprint(row["name"]) == errorClass {
			if c, ok := row["count"].(float64); ok {
				n += int(c)
			}
			break
		}
	}
	if n > 0 {
		return n
	}
	for _, row := range getQueryResults(bundle, "top_errors") {
		if fmt.Sprint(row["facet"]) == errorMessage || fmt.Sprint(row["name"]) == errorMessage {
			if c, ok := row["count"].(float64); ok {
				n += int(c)
			}
			break
		}
	}
	return n
}

// parseAuthFailuresByUsername returns username -> count from auth_failures_by_username result rows.
func parseAuthFailuresByUsername(rows []map[string]interface{}) []AuthHotspot {
	var out []AuthHotspot
	for _, row := range rows {
		user := ""
		if u, ok := row["auth.username"]; ok && u != nil {
			user = fmt.Sprint(u)
		}
		if user == "" {
			if u, ok := row["facet"]; ok && u != nil {
				user = fmt.Sprint(u)
			}
		}
		if user == "" {
			if u, ok := row["name"]; ok && u != nil {
				user = fmt.Sprint(u)
			}
		}
		user = strings.TrimSpace(user)
		count := 0
		if c, ok := row["count"].(float64); ok {
			count = int(c)
		}
		if user != "" || count > 0 {
			out = append(out, AuthHotspot{Username: user, Count: count})
		}
	}
	return out
}

func parseInt(v interface{}) int {
	switch x := v.(type) {
	case float64:
		return int(x)
	case int:
		return x
	case int64:
		return int(x)
	}
	return 0
}

// ComputeDelta builds Delta from previous baseline, current metrics, bundle (for auth/errors), and thresholds.
func ComputeDelta(prev *Baseline, curr BaselineMetrics, t Thresholds, bundle *Bundle, cfg Config, appWarning string) Delta {
	d := Delta{
		Current:    curr,
		Previous:   BaselineMetrics{},
		Status:     "GREEN",
		Confidence: confidenceFromN(curr.TxnN, t),
		Reasons:    nil,
	}

	if appWarning != "" {
		d.Reasons = append(d.Reasons, appWarning)
	}

	if prev != nil {
		d.Previous = prev.Metrics
		if prev.Metrics.P95ms > 0 || curr.P95ms != 0 {
			if p := pctChange(curr.P95ms, prev.Metrics.P95ms); p != nil {
				d.Changes.P95Pct = p
			}
		}
		if prev.Metrics.ReqPerMin > 0 || curr.ReqPerMin != 0 {
			if p := pctChange(curr.ReqPerMin, prev.Metrics.ReqPerMin); p != nil {
				d.Changes.ReqPerMinPct = p
			}
		}
		absErr := curr.Errors - prev.Metrics.Errors
		d.Changes.ErrorsAbs = &absErr
		absN := curr.TxnN - prev.Metrics.TxnN
		d.Changes.TxnNAbs = &absN
	}

	if prev == nil {
		d.Reasons = append(d.Reasons, "First run (no baseline)")
		// Still compute auth classification for summary
		classifyAuth(bundle, cfg, &d)
		addExpectedNoiseReasons(&d, cfg)
		return d
	}

	if d.Confidence == "LOW" {
		d.Reasons = append(d.Reasons, fmt.Sprintf("LOW CONFIDENCE: txn count n=%d", curr.TxnN))
	}

	prevM := prev.Metrics
	threshold := cfg.AuthUserThreshold
	if threshold <= 0 {
		threshold = 5
	}

	// Auth: parse auth_failures_by_username and flag if any user >= threshold
	classifyAuth(bundle, cfg, &d)
	redThreshold := threshold * 3

	if d.AuthThresholdExceeded {
		for _, h := range d.AuthHotspots {
			if h.Count >= redThreshold {
				d.Status = "RED"
				d.Reasons = append(d.Reasons, fmt.Sprintf("High auth failures for username=%s: %d in window (threshold %d, RED at %d)", h.Username, h.Count, threshold, redThreshold))
				break
			}
		}
		if d.Status != "RED" {
			d.Status = "YELLOW"
			d.Reasons = append(d.Reasons, fmt.Sprintf("High auth failures for username(s) above threshold %d", threshold))
		}
	}

	// RED for errors: only when NOT all 401 expected noise (i.e. when we have unexpected errors or auth threshold exceeded)
	allErrorsAre401Expected := curr.Errors > 0 && d.ExpectedAuth401 >= curr.Errors && !d.AuthThresholdExceeded
	if curr.Errors > 0 && (prevM.Errors == 0 || curr.Errors > prevM.Errors) && !allErrorsAre401Expected {
		d.Status = "RED"
		d.Reasons = append(d.Reasons, fmt.Sprintf("errors increased (%d → %d)", prevM.Errors, curr.Errors))
	}

	if d.Confidence != "LOW" && prevM.P95ms > 0 && d.Changes.P95Pct != nil && *d.Changes.P95Pct > t.P95RedPct {
		d.Status = "RED"
		d.Reasons = append(d.Reasons, fmt.Sprintf("p95_ms increased +%.0f%% (%.1f → %.1f)", *d.Changes.P95Pct, prevM.P95ms, curr.P95ms))
	}

	if d.Status == "GREEN" && d.Confidence != "LOW" && prevM.P95ms > 0 && d.Changes.P95Pct != nil && *d.Changes.P95Pct > t.P95YellowPct {
		d.Status = "YELLOW"
		d.Reasons = append(d.Reasons, fmt.Sprintf("p95_ms increased +%.0f%% (%.1f → %.1f)", *d.Changes.P95Pct, prevM.P95ms, curr.P95ms))
	}

	if d.Status == "GREEN" && d.Confidence != "LOW" && prevM.ReqPerMin > 0 && d.Changes.ReqPerMinPct != nil && *d.Changes.ReqPerMinPct < -t.ThroughputDropPct {
		d.Status = "YELLOW"
		d.Reasons = append(d.Reasons, fmt.Sprintf("req/min dropped %.0f%% (%.2f → %.2f)", *d.Changes.ReqPerMinPct, prevM.ReqPerMin, curr.ReqPerMin))
	}

	addExpectedNoiseReasons(&d, cfg)
	return d
}

func classifyAuth(bundle *Bundle, cfg Config, d *Delta) {
	rows := getQueryResults(bundle, "auth_failures_by_username")
	d.AuthHotspots = parseAuthFailuresByUsername(rows)
	threshold := cfg.AuthUserThreshold
	if threshold <= 0 {
		threshold = 5
	}
	for _, h := range d.AuthHotspots {
		if h.Count >= threshold {
			d.AuthThresholdExceeded = true
			break
		}
	}
	d.ExpectedAuth401 = count401FromBundle(bundle, cfg.AuthErrorClass, cfg.AuthErrorMessage)
	// If threshold exceeded, don't treat 401 as expected for summary; otherwise expected = min(401 count, total errors)
	if !d.AuthThresholdExceeded && d.ExpectedAuth401 > 0 {
		if d.Current.Errors > 0 && d.ExpectedAuth401 > d.Current.Errors {
			d.ExpectedAuth401 = d.Current.Errors
		}
	} else if d.AuthThresholdExceeded {
		d.ExpectedAuth401 = 0
	}
	// Build unexpected error summary from top_error_classes excluding 401
	for _, row := range getQueryResults(bundle, "top_error_classes") {
		facet := fmt.Sprint(row["facet"])
		if facet == cfg.AuthErrorClass {
			continue
		}
		c := parseInt(row["count"])
		if c > 0 {
			if d.UnexpectedErrorSummary != "" {
				d.UnexpectedErrorSummary += "; "
			}
			d.UnexpectedErrorSummary += fmt.Sprintf("%s=%d", facet, c)
		}
	}
}

func addExpectedNoiseReasons(d *Delta, cfg Config) {
	if d.ExpectedAuth401 > 0 {
		d.Reasons = append(d.Reasons, fmt.Sprintf("Expected auth failures (%s %s): %d", cfg.AuthErrorMessage, cfg.AuthErrorClass, d.ExpectedAuth401))
	}
}

// DeltaSummaryLines returns a short list of "key deltas" lines for display (e.g. "p95_ms: 120.0 → 157.2 (+31%)").
// Returns nil when there was no previous baseline (all change fields nil).
func DeltaSummaryLines(d Delta) []string {
	if d.Changes.P95Pct == nil && d.Changes.ReqPerMinPct == nil && d.Changes.ErrorsAbs == nil && d.Changes.TxnNAbs == nil {
		return nil
	}
	var lines []string
	prev := d.Previous
	curr := d.Current

	if d.Changes.P95Pct != nil {
		lines = append(lines, fmt.Sprintf("p95_ms: %.1f → %.1f (%+.0f%%)", prev.P95ms, curr.P95ms, *d.Changes.P95Pct))
	} else if prev.P95ms != 0 || curr.P95ms != 0 {
		lines = append(lines, fmt.Sprintf("p95_ms: %.1f → %.1f (N/A)", prev.P95ms, curr.P95ms))
	}
	if d.Changes.ReqPerMinPct != nil {
		lines = append(lines, fmt.Sprintf("req/min: %.2f → %.2f (%+.0f%%)", prev.ReqPerMin, curr.ReqPerMin, *d.Changes.ReqPerMinPct))
	} else if prev.ReqPerMin != 0 || curr.ReqPerMin != 0 {
		lines = append(lines, fmt.Sprintf("req/min: %.2f → %.2f (N/A)", prev.ReqPerMin, curr.ReqPerMin))
	}
	if d.Changes.ErrorsAbs != nil {
		lines = append(lines, fmt.Sprintf("errors: %d → %d (%+d)", prev.Errors, curr.Errors, *d.Changes.ErrorsAbs))
	}
	if d.Changes.TxnNAbs != nil {
		lines = append(lines, fmt.Sprintf("txn n: %d → %d (%+d)", prev.TxnN, curr.TxnN, *d.Changes.TxnNAbs))
	}
	return lines
}

// RecommendedNextQueries returns deterministic recommended NRQL suggestions based on delta status/reasons.
func RecommendedNextQueries(d Delta) []string {
	var out []string
	// RED due to errors
	if d.Status == "RED" && d.Current.Errors > 0 {
		out = append(out, "FROM TransactionError SELECT count(*) FACET error.class, error.message")
		out = append(out, "FROM TransactionError SELECT count(*) FROM Transaction FACET name")
	}
	// RED/YELLOW due to p95
	if (d.Status == "RED" || d.Status == "YELLOW") && d.Changes.P95Pct != nil && *d.Changes.P95Pct > 0 {
		out = append(out, "SELECT percentile(duration, 95) * 1000 AS 'p95_ms' FROM Transaction FACET name")
		out = append(out, "SELECT percentile(duration, 95) * 1000 AS 'p95_ms' FROM Transaction FACET request.method")
	}
	// Throughput drop
	if d.Status == "YELLOW" && d.Changes.ReqPerMinPct != nil && *d.Changes.ReqPerMinPct < 0 {
		out = append(out, "SELECT count(*) FROM Transaction FACET name TIMESERIES")
	}
	return out
}
