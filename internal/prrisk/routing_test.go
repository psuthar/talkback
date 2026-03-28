package prrisk

import (
	"strings"
	"testing"

	riskcontext "github.com/psuthar/talkback/internal/prrisk/context"
)

func hasRoutingHint(hints []string, substr string) bool {
	for _, h := range hints {
		if strings.Contains(h, substr) {
			return true
		}
	}
	return false
}

func TestRoutingAuthDomain(t *testing.T) {
	s := Signals{DomainHits: map[string]int{DomainAuth: 1}, FileCount: 1}
	hints := ComputeRoutingHints(s, nil, nil)
	if !hasRoutingHint(hints, "auth") {
		t.Errorf("expected auth routing hint, got %v", hints)
	}
}

func TestRoutingRAGDomain(t *testing.T) {
	s := Signals{DomainHits: map[string]int{DomainRAG: 1}, FileCount: 1}
	hints := ComputeRoutingHints(s, nil, nil)
	if !hasRoutingHint(hints, "RAG") {
		t.Errorf("expected RAG routing hint, got %v", hints)
	}
}

func TestRoutingProcessingDomain(t *testing.T) {
	s := Signals{DomainHits: map[string]int{DomainProcessing: 1}, FileCount: 1}
	hints := ComputeRoutingHints(s, nil, nil)
	if !hasRoutingHint(hints, "ingestion") {
		t.Errorf("expected processing/ingestion routing hint, got %v", hints)
	}
}

func TestRoutingWebDomain(t *testing.T) {
	s := Signals{DomainHits: map[string]int{DomainWeb: 1}, FileCount: 1}
	hints := ComputeRoutingHints(s, nil, nil)
	if !hasRoutingHint(hints, "frontend") {
		t.Errorf("expected frontend routing hint for web domain, got %v", hints)
	}
}

func TestRoutingWebLargeFactor(t *testing.T) {
	s := Signals{DomainHits: map[string]int{}, FileCount: 1}
	factors := []RiskFactor{{ID: "web_large", Points: 12}}
	hints := ComputeRoutingHints(s, factors, nil)
	if !hasRoutingHint(hints, "frontend") {
		t.Errorf("expected frontend routing hint for web_large factor, got %v", hints)
	}
}

func TestRoutingMigrationsDomain(t *testing.T) {
	s := Signals{MigrationFiles: 1, DomainHits: map[string]int{DomainMigrations: 1}, FileCount: 1}
	hints := ComputeRoutingHints(s, nil, nil)
	if !hasRoutingHint(hints, "database") {
		t.Errorf("expected database/migrations routing hint, got %v", hints)
	}
}

func TestRoutingWorkflowsDomain(t *testing.T) {
	s := Signals{DomainHits: map[string]int{DomainWorkflows: 1}, FileCount: 1}
	hints := ComputeRoutingHints(s, nil, nil)
	if !hasRoutingHint(hints, "CI") {
		t.Errorf("expected CI/infra routing hint for workflows, got %v", hints)
	}
}

func TestRoutingDeployDomain(t *testing.T) {
	s := Signals{DomainHits: map[string]int{DomainDeploy: 1}, FileCount: 1}
	hints := ComputeRoutingHints(s, nil, nil)
	if !hasRoutingHint(hints, "CI") {
		t.Errorf("expected CI/infra routing hint for deploy, got %v", hints)
	}
}

func TestRoutingFocusedConcentration(t *testing.T) {
	ci := &riskcontext.ContextInsights{
		Concentration: riskcontext.ConcentrationInsight{Mode: "focused", TopPrefix: "internal/auth"},
	}
	s := Signals{DomainHits: map[string]int{}, FileCount: 3}
	hints := ComputeRoutingHints(s, nil, ci)
	if !hasRoutingHint(hints, "internal/auth") {
		t.Errorf("expected focus hint naming top prefix, got %v", hints)
	}
}

func TestRoutingScatteredConcentration(t *testing.T) {
	ci := &riskcontext.ContextInsights{
		Concentration: riskcontext.ConcentrationInsight{Mode: "scattered"},
	}
	s := Signals{DomainHits: map[string]int{}, FileCount: 10}
	hints := ComputeRoutingHints(s, nil, ci)
	if !hasRoutingHint(hints, "Split") {
		t.Errorf("expected split review hint for scattered change, got %v", hints)
	}
}

func TestRoutingScatteredRequiresTenFiles(t *testing.T) {
	ci := &riskcontext.ContextInsights{
		Concentration: riskcontext.ConcentrationInsight{Mode: "scattered"},
	}
	s := Signals{DomainHits: map[string]int{}, FileCount: 9}
	hints := ComputeRoutingHints(s, nil, ci)
	if hasRoutingHint(hints, "Split") {
		t.Errorf("scattered split hint should not fire with fewer than 10 files, got %v", hints)
	}
}

func TestRoutingHotspotOverlap(t *testing.T) {
	ci := &riskcontext.ContextInsights{
		Hotspots: []riskcontext.HotspotInsight{{Prefix: "internal/rag", RecentCount: 9}},
	}
	s := Signals{DomainHits: map[string]int{}, FileCount: 2}
	hints := ComputeRoutingHints(s, nil, ci)
	if !hasRoutingHint(hints, "internal/rag") {
		t.Errorf("expected hotspot attention hint naming prefix, got %v", hints)
	}
}

func TestRoutingIntentMismatch(t *testing.T) {
	ci := &riskcontext.ContextInsights{
		Intent: riskcontext.IntentInsight{Mismatch: true, DomainsExpected: []string{"auth"}},
	}
	s := Signals{DomainHits: map[string]int{}, FileCount: 2}
	hints := ComputeRoutingHints(s, nil, ci)
	if !hasRoutingHint(hints, "scope") {
		t.Errorf("expected scope-confirm hint for intent mismatch, got %v", hints)
	}
}

func TestRoutingFallbackGenericHint(t *testing.T) {
	// Only DomainOther — no specific domain hints fire, triggering the generic fallback.
	s := Signals{DomainHits: map[string]int{DomainOther: 1}, FileCount: 2}
	hints := ComputeRoutingHints(s, nil, nil)
	if !hasRoutingHint(hints, "CODEOWNERS") {
		t.Errorf("expected generic CODEOWNERS fallback hint, got %v", hints)
	}
}

func TestRoutingEmptyDiffNoHints(t *testing.T) {
	s := Signals{DomainHits: map[string]int{}, FileCount: 0}
	hints := ComputeRoutingHints(s, nil, nil)
	if len(hints) != 0 {
		t.Errorf("expected no routing hints for empty diff, got %v", hints)
	}
}

func TestRoutingNoDuplicates(t *testing.T) {
	// RAG + Processing both map to the same hint — should appear only once.
	s := Signals{DomainHits: map[string]int{DomainRAG: 1, DomainProcessing: 1}, FileCount: 3}
	hints := ComputeRoutingHints(s, nil, nil)
	seen := map[string]int{}
	for _, h := range hints {
		seen[h]++
	}
	for h, c := range seen {
		if c > 1 {
			t.Errorf("duplicate routing hint: %q (count %d)", h, c)
		}
	}
}
