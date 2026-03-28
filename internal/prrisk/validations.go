package prrisk

import (
	"sort"
	"strings"
)

// validationForAction maps required-action IDs to stable, machine-readable validation lines.
func validationForAction(id string) (string, bool) {
	switch id {
	case "ci_fetch_depth_zero":
		return "ci: full git history available for diff (fetch-depth: 0 or equivalent)", true
	case "pr_review_summary":
		return "process: PR description with scoped, evidence-backed review plan", true
	case "workflow_config_validation":
		return "config: workflow / deploy / go.mod changes validated against required checks", true
	case "auth_e2e_gate":
		return "test: auth/session/invite flows exercised (E2E or equivalent evidence)", true
	case "rag_qna_citations_gate":
		return "test: Q&A with citations validated for RAG-affecting changes", true
	case "materials_processing_gate":
		return "test: materials upload + processing smoke for pipeline changes", true
	case "migrations_validation_gate":
		return "db: migrations validated with rollback/reversal plan documented", true
	case "add_tests_or_evidence":
		return "test: tests or recorded evidence for sensitive paths", true
	case "context_align_pr_description":
		return "process: PR title/body aligned with actual diff (intent match)", true
	case "context_scattered_review_plan":
		return "process: structured review map for scattered multi-area change", true
	case "context_improve_test_proximity":
		return "test: tests co-located or explicitly linked for changed code", true
	case "context_hotspot_regression_focus":
		return "test: targeted regression for high-churn area touched by diff", true
	default:
		return "", false
	}
}

// ComputeRequiredValidations builds a deterministic ordered list of validations from
// required actions and global signals. Always includes a baseline CI line first when
// there is no git error.
func ComputeRequiredValidations(s Signals, actions []RequiredAction) []string {
	seen := make(map[string]struct{})
	var out []string

	add := func(line string) {
		line = strings.TrimSpace(line)
		if line == "" {
			return
		}
		if _, ok := seen[line]; ok {
			return
		}
		seen[line] = struct{}{}
		out = append(out, line)
	}

	if s.GitError == "" {
		add("ci: required status checks must pass before merge")
	} else {
		add("ci: restore reliable git diff before merge (see git error in report)")
	}

	ids := make([]string, 0, len(actions))
	for _, a := range actions {
		ids = append(ids, a.ID)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if v, ok := validationForAction(id); ok {
			add(v)
		}
	}

	if s.ValidationNoteFound {
		add("process: validation note present in commit — confirm it matches what was run")
	}

	return out
}
