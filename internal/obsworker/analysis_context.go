package obsworker

import (
	"fmt"
	"strings"
)

// IncidentSummary is a deterministic, evidence-grounded summary of the incident
// used to anchor the AI model and improve reasoning quality for localized incidents.
type IncidentSummary struct {
	Status                    string  `json:"status"`
	Confidence                string  `json:"confidence"`
	IsSimulation              bool    `json:"is_simulation"`
	TotalErrors               int     `json:"total_errors"`
	UnexpectedErrors          int     `json:"unexpected_errors"`
	DominantErrorURI          string  `json:"dominant_error_uri,omitempty"`
	DominantErrorCount        int     `json:"dominant_error_count,omitempty"`
	DominantErrorSharePct     float64 `json:"dominant_error_share_pct,omitempty"`
	DominantTransaction       string  `json:"dominant_transaction,omitempty"`
	DominantTransactionCount  int     `json:"dominant_transaction_count,omitempty"`
	OverallP95ms              float64 `json:"overall_p95_ms,omitempty"`
	OverallReqPerMin          float64 `json:"overall_req_per_min,omitempty"`
	BroadLatencyRegression    bool    `json:"broad_latency_regression"`
	BroadErrorDistribution    bool   `json:"broad_error_distribution"`
	LikelyIsolatedIncident    bool   `json:"likely_isolated_incident"`
	ExpectedNoiseOnly         bool   `json:"expected_noise_only"`

	// Route classification for debug/test/synthetic fault detection
	DominantURIDebugLike         bool   `json:"dominant_uri_debug_like"`
	DominantTransactionDebugLike bool   `json:"dominant_transaction_debug_like"`
	OverallLatencyStable         bool   `json:"overall_latency_stable"`
	RecommendedDiagnosisMode    string `json:"recommended_diagnosis_mode,omitempty"`
}

// Debug-like path substrings (any match sets DominantURIDebugLike / DominantTransactionDebugLike).
var debugLikePathSubstrings = []string{"/debug/", "/fault/", "/test/", "/synthetic/", "/chaos/"}

// overallLatencyStableThresholdMs: p95 below this is considered "stable" for diagnosis mode.
const overallLatencyStableThresholdMs = 500.0

// isolatedIncidentDominancePct is the minimum share of errors from the top endpoint
// for the incident to be considered "likely isolated" (narrow fault path).
const isolatedIncidentDominancePct = 80.0

// broadLatencyYellowPct is the P95 increase (percent) above which we consider
// latency regression "broad" (not just one slow endpoint).
const broadLatencyYellowPct = 20.0

// ComputeIncidentSummary builds an IncidentSummary from the bundle and delta.
// Uses bundle.Results (errors_by_endpoint, errors_by_endpoint_uri, error_count),
// bundle.Delta, and bundle.Simulation.
func ComputeIncidentSummary(b *Bundle) IncidentSummary {
	var s IncidentSummary
	if b == nil {
		return s
	}
	if b.Summary != nil {
		s.Status = b.Summary.Status
		s.Confidence = b.Summary.Confidence
	}
	if b.Delta != nil {
		d := b.Delta
		if s.Status == "" {
			s.Status = d.Status
		}
		if s.Confidence == "" {
			s.Confidence = d.Confidence
		}
		s.TotalErrors = d.Current.Errors
		s.OverallP95ms = d.Current.P95ms
		s.OverallReqPerMin = d.Current.ReqPerMin
		// Unexpected = total minus expected auth (401) noise
		s.UnexpectedErrors = d.Current.Errors - d.ExpectedAuth401
		if s.UnexpectedErrors < 0 {
			s.UnexpectedErrors = 0
		}
		// Broad latency: significant p95 increase vs previous
		if d.Changes.P95Pct != nil && *d.Changes.P95Pct >= broadLatencyYellowPct {
			s.BroadLatencyRegression = true
		}
	}
	if b.Simulation != nil && b.Simulation.Enabled {
		s.IsSimulation = true
	}

	// Dominant error endpoint: prefer errors_by_endpoint (transactionName), fallback errors_by_endpoint_uri
	rows := getQueryResults(b, "errors_by_endpoint")
	facetKey := "facet"
	if len(rows) == 0 {
		rows = getQueryResults(b, "errors_by_endpoint_uri")
	}
	if len(rows) == 0 && len(b.DeepDiveResults) > 0 {
		for _, qr := range b.DeepDiveResults {
			if qr.Name == "errors_by_txn" && len(qr.Results) > 0 {
				rows = qr.Results
				facetKey = "name"
				break
			}
		}
	}
	var totalFromFacet int
	for _, row := range rows {
		c := parseInt(row["count"])
		totalFromFacet += c
	}
	if len(rows) > 0 && totalFromFacet > 0 {
		top := rows[0]
		s.DominantErrorCount = parseInt(top["count"])
		s.DominantErrorSharePct = float64(s.DominantErrorCount) / float64(totalFromFacet) * 100
		name := fmt.Sprint(top[facetKey])
		if name == "" {
			name = fmt.Sprint(top["name"])
		}
		s.DominantErrorURI = name
		s.DominantTransaction = name
		s.DominantTransactionCount = s.DominantErrorCount
		// Broad error distribution: top endpoint has < isolatedIncidentDominancePct of errors
		if s.DominantErrorSharePct < isolatedIncidentDominancePct {
			s.BroadErrorDistribution = true
		}
		// Debug-like route classification
		s.DominantURIDebugLike = isDebugLikePath(s.DominantErrorURI)
		s.DominantTransactionDebugLike = isDebugLikePath(s.DominantTransaction)
	}

	// Overall latency stable: low p95 and no broad regression
	if s.OverallP95ms > 0 && s.OverallP95ms < overallLatencyStableThresholdMs && !s.BroadLatencyRegression {
		s.OverallLatencyStable = true
	}

	// Likely isolated: one endpoint dominates errors, and we don't have broad latency regression
	if s.TotalErrors > 0 && s.UnexpectedErrors > 0 &&
		s.DominantErrorSharePct >= isolatedIncidentDominancePct &&
		!s.BroadLatencyRegression {
		s.LikelyIsolatedIncident = true
	}
	// Expected noise only: green and no unexpected errors
	if s.Status == "GREEN" && s.UnexpectedErrors == 0 {
		s.ExpectedNoiseOnly = true
	}

	// Recommended diagnosis mode
	if s.ExpectedNoiseOnly {
		s.RecommendedDiagnosisMode = "expected_auth_noise"
	} else if s.DominantURIDebugLike || s.DominantTransactionDebugLike {
		if s.LikelyIsolatedIncident && s.OverallLatencyStable {
			s.RecommendedDiagnosisMode = "synthetic_fault"
		} else {
			s.RecommendedDiagnosisMode = "isolated_route_failure"
		}
	} else if s.LikelyIsolatedIncident {
		s.RecommendedDiagnosisMode = "isolated_route_failure"
	} else if s.BroadLatencyRegression || s.BroadErrorDistribution {
		s.RecommendedDiagnosisMode = "broad_service_issue"
	} else {
		s.RecommendedDiagnosisMode = "unknown"
	}
	return s
}

func isDebugLikePath(s string) bool {
	lower := strings.ToLower(s)
	for _, sub := range debugLikePathSubstrings {
		if strings.Contains(lower, sub) {
			return true
		}
	}
	return false
}

// FormatIncidentSummaryForAI returns a short markdown block for the AI prompt.
// Only non-zero/non-empty fields are included to keep the block small (~300-500 chars).
func FormatIncidentSummaryForAI(s IncidentSummary) string {
	var lines []string
	lines = append(lines, "## Incident summary (deterministic)", "")
	if s.Status != "" {
		lines = append(lines, fmt.Sprintf("- status: %s", s.Status))
	}
	if s.Confidence != "" {
		lines = append(lines, fmt.Sprintf("- confidence: %s", s.Confidence))
	}
	if s.IsSimulation {
		lines = append(lines, "- is_simulation: true")
	}
	if s.TotalErrors > 0 {
		lines = append(lines, fmt.Sprintf("- total_errors: %d", s.TotalErrors))
	}
	if s.UnexpectedErrors > 0 {
		lines = append(lines, fmt.Sprintf("- unexpected_errors: %d", s.UnexpectedErrors))
	}
	if s.DominantTransaction != "" {
		lines = append(lines, fmt.Sprintf("- dominant_transaction: %s", s.DominantTransaction))
	}
	if s.DominantErrorURI != "" && s.DominantErrorURI != s.DominantTransaction {
		lines = append(lines, fmt.Sprintf("- dominant_error_uri: %s", s.DominantErrorURI))
	}
	if s.DominantErrorCount > 0 {
		lines = append(lines, fmt.Sprintf("- dominant_error_count: %d", s.DominantErrorCount))
	}
	if s.DominantErrorSharePct > 0 {
		lines = append(lines, fmt.Sprintf("- dominant_error_share_pct: %.0f", s.DominantErrorSharePct))
	}
	if s.OverallP95ms > 0 {
		lines = append(lines, fmt.Sprintf("- overall_p95_ms: %.2f", s.OverallP95ms))
	}
	if s.OverallReqPerMin > 0 {
		lines = append(lines, fmt.Sprintf("- overall_req_per_min: %.2f", s.OverallReqPerMin))
	}
	lines = append(lines, fmt.Sprintf("- broad_latency_regression: %t", s.BroadLatencyRegression))
	lines = append(lines, fmt.Sprintf("- broad_error_distribution: %t", s.BroadErrorDistribution))
	lines = append(lines, fmt.Sprintf("- likely_isolated_incident: %t", s.LikelyIsolatedIncident))
	if s.DominantURIDebugLike {
		lines = append(lines, "- dominant_uri_debug_like: true")
	}
	if s.DominantTransactionDebugLike {
		lines = append(lines, "- dominant_transaction_debug_like: true")
	}
	if s.OverallLatencyStable {
		lines = append(lines, "- overall_latency_stable: true")
	}
	if s.RecommendedDiagnosisMode != "" {
		lines = append(lines, fmt.Sprintf("- recommended_diagnosis_mode: %s", s.RecommendedDiagnosisMode))
	}
	if s.ExpectedNoiseOnly {
		lines = append(lines, "- expected_noise_only: true")
	}
	return strings.Join(lines, "\n")
}

// Speculative subsystem labels to drop when diagnosis suggests synthetic/debug fault.
var speculativeSubsystemLabels = []string{
	"database", "storage", "backend service", "backend", "infrastructure",
	"downstream", "cache", "message queue", "external api",
}

// SanitizeLikelySubsystems filters AI-suggested subsystems to keep only evidence-based ones
// when the incident looks like a synthetic/debug fault.
func SanitizeLikelySubsystems(summary IncidentSummary, subsystems []string) []string {
	if len(subsystems) == 0 {
		return subsystems
	}
	// When synthetic_fault or debug-like dominant URI, drop speculative labels.
	if summary.RecommendedDiagnosisMode != "synthetic_fault" && !summary.DominantURIDebugLike {
		return subsystems
	}
	var out []string
	lowerSpeculative := make([]string, len(speculativeSubsystemLabels))
	for i, s := range speculativeSubsystemLabels {
		lowerSpeculative[i] = strings.ToLower(s)
	}
	for _, sub := range subsystems {
		subLower := strings.ToLower(strings.TrimSpace(sub))
		if subLower == "" {
			continue
		}
		dropped := false
		for _, spec := range lowerSpeculative {
			if strings.Contains(subLower, spec) || strings.Contains(spec, subLower) {
				dropped = true
				break
			}
		}
		if !dropped {
			out = append(out, sub)
		}
	}
	// If we dropped everything, prefer a minimal evidence-based label
	if len(out) == 0 && (summary.DominantURIDebugLike || summary.DominantTransactionDebugLike) {
		out = append(out, "Debug/Fault Injection Route Handler")
	}
	return out
}
