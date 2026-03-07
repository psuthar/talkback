package obsworker

import (
	"strings"
	"testing"
)

func TestComputeIncidentSummary_LikelyIsolatedIncident(t *testing.T) {
	// One endpoint dominates errors; no broad latency regression -> LikelyIsolatedIncident
	b := &Bundle{
		Summary: &BundleSummary{Status: "RED", Confidence: "HIGH"},
		Delta: &Delta{
			Status:         "RED",
			Confidence:     "HIGH",
			Current:        BaselineMetrics{Errors: 8, P95ms: 0.1, ReqPerMin: 15},
			Previous:       BaselineMetrics{Errors: 0},
			ExpectedAuth401: 0,
			Changes: DeltaChanges{P95Pct: floatPtr(0)}, // no p95 spike
		},
		Results: []QueryResult{
			{
				Name: "errors_by_endpoint",
				Results: []map[string]interface{}{
					{"facet": "WebTransaction/Go/POST /debug/fault/error", "count": float64(8)},
				},
			},
		},
	}
	s := ComputeIncidentSummary(b)
	if !s.LikelyIsolatedIncident {
		t.Errorf("expected LikelyIsolatedIncident=true when one endpoint has 100%% of errors and no latency regression, got false")
	}
	if s.DominantTransaction != "WebTransaction/Go/POST /debug/fault/error" {
		t.Errorf("expected DominantTransaction to be the fault endpoint, got %q", s.DominantTransaction)
	}
	if s.DominantErrorSharePct != 100 {
		t.Errorf("expected DominantErrorSharePct=100, got %.0f", s.DominantErrorSharePct)
	}
	if s.BroadErrorDistribution {
		t.Error("expected BroadErrorDistribution=false when one endpoint dominates")
	}
	// Debug/fault route classification
	if !s.DominantURIDebugLike {
		t.Error("expected DominantURIDebugLike=true for /debug/fault/error")
	}
	if !s.DominantTransactionDebugLike {
		t.Error("expected DominantTransactionDebugLike=true for transaction containing /debug/fault/error")
	}
	if s.RecommendedDiagnosisMode != "synthetic_fault" {
		t.Errorf("expected RecommendedDiagnosisMode=synthetic_fault when isolated debug route, got %q", s.RecommendedDiagnosisMode)
	}
	if !s.OverallLatencyStable {
		t.Error("expected OverallLatencyStable=true when p95 is low and no broad regression")
	}
}

func TestComputeIncidentSummary_BroadErrorDistribution(t *testing.T) {
	// Errors spread across endpoints -> BroadErrorDistribution, not isolated
	b := &Bundle{
		Summary: &BundleSummary{Status: "RED"},
		Delta:   &Delta{Current: BaselineMetrics{Errors: 10}, ExpectedAuth401: 0},
		Results: []QueryResult{
			{
				Name: "errors_by_endpoint",
				Results: []map[string]interface{}{
					{"facet": "/api/a", "count": float64(4)},
					{"facet": "/api/b", "count": float64(3)},
					{"facet": "/api/c", "count": float64(3)},
				},
			},
		},
	}
	s := ComputeIncidentSummary(b)
	if !s.BroadErrorDistribution {
		t.Error("expected BroadErrorDistribution=true when top endpoint has <80%% of errors")
	}
	if s.LikelyIsolatedIncident {
		t.Error("expected LikelyIsolatedIncident=false when errors are spread")
	}
	if s.DominantErrorSharePct >= 80 {
		t.Errorf("expected dominant share < 80%%, got %.0f", s.DominantErrorSharePct)
	}
}

func TestComputeIncidentSummary_Simulation(t *testing.T) {
	b := &Bundle{
		Summary:    &BundleSummary{Status: "RED"},
		Simulation: &Simulation{Enabled: true, ForcedStatus: "RED"},
	}
	s := ComputeIncidentSummary(b)
	if !s.IsSimulation {
		t.Error("expected IsSimulation=true")
	}
}

func TestComputeIncidentSummary_ExpectedNoiseOnly(t *testing.T) {
	b := &Bundle{
		Summary: &BundleSummary{Status: "GREEN"},
		Delta:   &Delta{Current: BaselineMetrics{Errors: 2}, ExpectedAuth401: 2},
	}
	s := ComputeIncidentSummary(b)
	if !s.ExpectedNoiseOnly {
		t.Error("expected ExpectedNoiseOnly=true when GREEN and unexpected_errors=0")
	}
}

func TestSanitizeLikelySubsystems_SyntheticFaultDropsSpeculative(t *testing.T) {
	summary := IncidentSummary{
		RecommendedDiagnosisMode: "synthetic_fault",
		DominantURIDebugLike:     true,
	}
	subsystems := []string{"database", "backend service", "Debug/Fault Injection Route Handler"}
	got := SanitizeLikelySubsystems(summary, subsystems)
	for _, s := range got {
		if s == "database" || s == "backend service" {
			t.Errorf("synthetic_fault should drop speculative subsystems like %q, got %v", s, got)
		}
	}
	if len(got) == 0 {
		t.Error("expected at least one evidence-based label kept")
	}
	found := false
	for _, s := range got {
		if strings.Contains(s, "Debug") || strings.Contains(s, "Fault") || strings.Contains(s, "Route") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected route/handler-level label kept, got %v", got)
	}
}

func TestIncidentNote_SyntheticFault(t *testing.T) {
	s := IncidentSummary{RecommendedDiagnosisMode: "synthetic_fault"}
	note := IncidentNote(s)
	if note == "" {
		t.Fatal("expected non-empty incident note")
	}
	if !strings.Contains(note, "debug") && !strings.Contains(note, "fault") {
		t.Errorf("synthetic_fault note should mention debug/fault: %q", note)
	}
}

func TestLikelyCodeArea_SyntheticFault(t *testing.T) {
	s := IncidentSummary{RecommendedDiagnosisMode: "synthetic_fault"}
	area := LikelyCodeArea(s)
	if area != "Debug/Fault injection route handler" {
		t.Errorf("expected Debug/Fault injection route handler, got %q", area)
	}
}

func floatPtr(f float64) *float64 { return &f }
