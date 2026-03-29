package prrisk

import "sort"

const (
	PriorityHigh       = "high"
	PriorityMedium     = "medium"
	PrioritySupporting = "supporting"
)

// priorityForActionID maps action IDs to a priority tier (signal-derived defaults).
func priorityForActionID(id string) string {
	switch id {
	case "ci_fetch_depth_zero",
		"auth_e2e_gate",
		"rag_qna_citations_gate",
		"migrations_validation_gate",
		"add_tests_or_evidence":
		return PriorityHigh
	case "workflow_config_validation",
		"materials_processing_gate",
		"context_align_pr_description":
		return PriorityMedium
	case "pr_review_summary",
		"context_scattered_review_plan",
		"context_improve_test_proximity",
		"context_hotspot_regression_focus":
		return PrioritySupporting
	default:
		return PriorityMedium
	}
}

// SortRequiredActions orders actions high → medium → supporting, then by ID.
func SortRequiredActions(actions []RequiredAction) []RequiredAction {
	if len(actions) <= 1 {
		return actions
	}
	rank := func(p string) int {
		switch p {
		case PriorityHigh:
			return 0
		case PriorityMedium:
			return 1
		default:
			return 2
		}
	}
	out := append([]RequiredAction(nil), actions...)
	sort.SliceStable(out, func(i, j int) bool {
		pi, pj := out[i].Priority, out[j].Priority
		if pi == "" {
			pi = priorityForActionID(out[i].ID)
		}
		if pj == "" {
			pj = priorityForActionID(out[j].ID)
		}
		ri, rj := rank(pi), rank(pj)
		if ri != rj {
			return ri < rj
		}
		return out[i].ID < out[j].ID
	})
	return out
}
