package riskcontext

// Weights tune contextual factor contributions (deterministic).
type Weights struct {
	ProximityLowPoints    float64
	ScatteredPoints       float64
	HotspotPoints         float64
	IntentMismatchPoints  float64
}

// DefaultWeights returns built-in v2.4 contextual weights.
func DefaultWeights() Weights {
	return Weights{
		ProximityLowPoints:   5,
		ScatteredPoints:      5,
		HotspotPoints:        4,
		IntentMismatchPoints: 6,
	}
}

// Analyze runs all contextual analyzers and returns insights plus optional score factors.
func Analyze(in Input, w Weights) (ContextInsights, []FactorContribution) {
	prox := AnalyzeProximity(in)
	conc := AnalyzeConcentration(in)
	hotspots, hotspotSkipReason := AnalyzeHotspots(in)
	intent := AnalyzeIntent(in)

	insights := ContextInsights{
		Proximity:          prox,
		Concentration:      conc,
		Hotspots:           hotspots,
		HotspotsSkipReason: hotspotSkipReason,
		Intent:             intent,
	}

	var factors []FactorContribution

	if shouldFlagProximity(in, prox) && w.ProximityLowPoints > 0 {
		factors = append(factors, FactorContribution{
			ID:     "context_test_proximity_distant",
			Label:  "Tests not co-located with changed code in this diff",
			Points: w.ProximityLowPoints,
			Detail: prox.Detail,
		})
	}

	if conc.Mode == "scattered" && len(in.Files) >= 10 && w.ScatteredPoints > 0 {
		factors = append(factors, FactorContribution{
			ID:     "context_change_scattered",
			Label:  "Change concentration is scattered across many areas",
			Points: w.ScatteredPoints,
			Detail: conc.Detail,
		})
	}

	if len(hotspots) > 0 && w.HotspotPoints > 0 {
		factors = append(factors, FactorContribution{
			ID:     "context_hotspot_overlap",
			Label:  "Diff overlaps a path prefix touched in multiple recent commits",
			Points: w.HotspotPoints,
			Detail: hotspots[0].Detail,
		})
	}

	if intent.Mismatch && len(intent.DomainsExpected) > 0 && w.IntentMismatchPoints > 0 {
		factors = append(factors, FactorContribution{
			ID:     "context_intent_mismatch",
			Label:  "PR title/body keywords do not align with paths in the diff",
			Points: w.IntentMismatchPoints,
			Detail: intent.Detail,
		})
	}

	return insights, factors
}

func shouldFlagProximity(in Input, prox ProximityInsight) bool {
	if prox.Mode != "distant" || prox.NonTestFiles < 2 {
		return false
	}
	if !hasSensitiveDomain(in.DomainHits) {
		return false
	}
	return true
}

func hasSensitiveDomain(h map[string]int) bool {
	if h == nil {
		return false
	}
	for _, k := range []string{"auth", "rag", "processing", "migrations", "api", "database"} {
		if h[k] > 0 {
			return true
		}
	}
	return false
}
