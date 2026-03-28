package prrisk

import "fmt"

// buildInterpretation returns a plain-English summary of the risk result.
func buildInterpretation(r Result) string {
	floorNote := ""
	if r.ScoreMath.FloorApplied {
		floorNote = fmt.Sprintf(
			" A risk floor raised the score from %.0f to %.0f so trust-critical signals are not masked by reducers.",
			r.ScoreMath.NetBeforeFloor, r.ScoreMath.FinalScore,
		)
	}
	switch r.RiskBand {
	case "low":
		return "Low risk. The diff is small and does not touch sensitive areas. Standard review is sufficient."
	case "medium":
		return fmt.Sprintf("Medium risk (score %.0f). Some risk factors are present but are manageable. Review the factors below before merging.%s", r.RiskScore, floorNote)
	case "high":
		return fmt.Sprintf("High risk (score %.0f). Significant risk factors detected. Complete all required actions before merging.%s", r.RiskScore, floorNote)
	case "critical":
		return fmt.Sprintf("Critical risk (score %.0f). Multiple high-impact areas changed. All required actions are mandatory before merge.%s", r.RiskScore, floorNote)
	default:
		return ""
	}
}
