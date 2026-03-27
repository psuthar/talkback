package prrisk

// floorMinNotLowBand is the minimum final score when floor rules apply.
// Bands: low < 20, medium 20–44.9, … — so 20 prevents "low" for trust-critical signals.
const floorMinNotLowBand = 20.0

// computeFloorThreshold returns the minimum allowed score and human-readable reasons
// when any trust floor factor is present in the factor list.
func computeFloorThreshold(factors []RiskFactor) (min float64, reasons []string) {
	has := factorIDs(factors)

	if okBool(has, "ci_workflows") {
		min = maxFloat(min, floorMinNotLowBand)
		reasons = append(reasons, "CI workflow changes cannot score in the low band (floor applies)")
	}
	if okBool(has, "deploy_config") {
		min = maxFloat(min, floorMinNotLowBand)
		reasons = append(reasons, "Deploy/hosting config changes cannot score in the low band (floor applies)")
	}
	if okBool(has, "go_mod_deps") {
		min = maxFloat(min, floorMinNotLowBand)
		reasons = append(reasons, "Go module/dependency lockfile changes cannot score in the low band (floor applies)")
	}
	if okBool(has, "diff_very_large") {
		min = maxFloat(min, floorMinNotLowBand)
		reasons = append(reasons, "Very large diff cannot score in the low band (floor applies)")
	}
	if okBool(has, "diff_large") {
		min = maxFloat(min, floorMinNotLowBand)
		reasons = append(reasons, "Large diff cannot score in the low band (floor applies)")
	}
	if okBool(has, "many_files") {
		min = maxFloat(min, floorMinNotLowBand)
		reasons = append(reasons, "Many files touched cannot score in the low band (floor applies)")
	}
	return min, reasons
}

// applyRiskFloor raises netBeforeFloor to the floor threshold when floor rules apply.
func applyRiskFloor(netBeforeFloor float64, factors []RiskFactor) (final float64, applied bool, floorMin float64, reasons []string) {
	floorMin, reasons = computeFloorThreshold(factors)
	if floorMin <= 0 {
		return clamp100(netBeforeFloor), false, 0, nil
	}
	if netBeforeFloor < floorMin {
		return clamp100(floorMin), true, floorMin, reasons
	}
	return clamp100(netBeforeFloor), false, floorMin, reasons
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
