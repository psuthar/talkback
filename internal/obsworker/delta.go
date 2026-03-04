package obsworker

import (
	"fmt"
	"math"
)

const pctEpsilon = 0.0001

// DeltaChanges holds percent or absolute changes for key metrics.
type DeltaChanges struct {
	P95Pct      *float64 `json:"p95_pct,omitempty"`
	ReqPerMinPct *float64 `json:"req_per_min_pct,omitempty"`
	ErrorsAbs   *int     `json:"errors_abs,omitempty"`
	TxnNAbs     *int     `json:"txn_n_abs,omitempty"`
}

// Delta holds current, previous, computed changes, status, confidence, and reasons.
type Delta struct {
	Current    BaselineMetrics `json:"current"`
	Previous   BaselineMetrics `json:"previous,omitempty"`
	Changes    DeltaChanges    `json:"changes,omitempty"`
	Status     string          `json:"status"`     // GREEN | YELLOW | RED
	Confidence string          `json:"confidence"` // LOW | MED | HIGH
	Reasons    []string        `json:"reasons"`
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

// ComputeDelta builds Delta from previous baseline and current metrics using the given thresholds.
func ComputeDelta(prev *Baseline, curr BaselineMetrics, t Thresholds) Delta {
	d := Delta{
		Current:    curr,
		Previous:   BaselineMetrics{},
		Status:     "GREEN",
		Confidence: confidenceFromN(curr.TxnN, t),
		Reasons:    nil,
	}

	if prev != nil {
		d.Previous = prev.Metrics
		// Populate changes
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

	// No previous baseline
	if prev == nil {
		d.Reasons = append(d.Reasons, "First run (no baseline)")
		return d
	}

	// LOW confidence reason
	if d.Confidence == "LOW" {
		d.Reasons = append(d.Reasons, fmt.Sprintf("LOW CONFIDENCE: txn count n=%d", curr.TxnN))
	}

	prevM := prev.Metrics

	// RED: errors > 0 AND (previous errors == 0 OR errors increased)
	if curr.Errors > 0 && (prevM.Errors == 0 || curr.Errors > prevM.Errors) {
		d.Status = "RED"
		d.Reasons = append(d.Reasons, fmt.Sprintf("errors increased (%d → %d)", prevM.Errors, curr.Errors))
	}

	// RED: p95 increased > 50% (only if prev p95 exists and confidence not LOW)
	if d.Confidence != "LOW" && prevM.P95ms > 0 && d.Changes.P95Pct != nil && *d.Changes.P95Pct > t.P95RedPct {
		d.Status = "RED"
		d.Reasons = append(d.Reasons, fmt.Sprintf("p95_ms increased +%.0f%% (%.1f → %.1f)", *d.Changes.P95Pct, prevM.P95ms, curr.P95ms))
	}

	// YELLOW: p95 increased > 20% (confidence not LOW) — only if not already RED
	if d.Status == "GREEN" && d.Confidence != "LOW" && prevM.P95ms > 0 && d.Changes.P95Pct != nil && *d.Changes.P95Pct > t.P95YellowPct {
		d.Status = "YELLOW"
		d.Reasons = append(d.Reasons, fmt.Sprintf("p95_ms increased +%.0f%% (%.1f → %.1f)", *d.Changes.P95Pct, prevM.P95ms, curr.P95ms))
	}

	// YELLOW: req_per_min dropped > 30% (confidence not LOW)
	if d.Status == "GREEN" && d.Confidence != "LOW" && prevM.ReqPerMin > 0 && d.Changes.ReqPerMinPct != nil && *d.Changes.ReqPerMinPct < -t.ThroughputDropPct {
		d.Status = "YELLOW"
		d.Reasons = append(d.Reasons, fmt.Sprintf("req/min dropped %.0f%% (%.2f → %.2f)", *d.Changes.ReqPerMinPct, prevM.ReqPerMin, curr.ReqPerMin))
	}

	return d
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
