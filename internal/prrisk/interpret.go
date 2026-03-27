package prrisk

import "fmt"

// buildInterpretation returns a plain-English summary of the risk result.
func buildInterpretation(r Result) string {
	switch r.RiskBand {
	case "low":
		return "Low risk. The diff is small and does not touch sensitive areas. Standard review is sufficient."
	case "medium":
		return fmt.Sprintf("Medium risk (score %.0f). Some risk factors are present but are manageable. Review the factors below before merging.", r.RiskScore)
	case "high":
		return fmt.Sprintf("High risk (score %.0f). Significant risk factors detected. Complete all required actions before merging.", r.RiskScore)
	case "critical":
		return fmt.Sprintf("Critical risk (score %.0f). Multiple high-impact areas changed. All required actions are mandatory before merge.", r.RiskScore)
	default:
		return ""
	}
}
