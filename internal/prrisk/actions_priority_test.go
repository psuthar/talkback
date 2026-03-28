package prrisk

import "testing"

func TestPriorityForActionIDHighPriority(t *testing.T) {
	for _, id := range []string{
		"ci_fetch_depth_zero",
		"auth_e2e_gate",
		"rag_qna_citations_gate",
		"migrations_validation_gate",
		"add_tests_or_evidence",
	} {
		got := priorityForActionID(id)
		if got != PriorityHigh {
			t.Errorf("priorityForActionID(%q) = %q, want %q", id, got, PriorityHigh)
		}
	}
}

func TestPriorityForActionIDMediumPriority(t *testing.T) {
	for _, id := range []string{
		"workflow_config_validation",
		"materials_processing_gate",
		"context_align_pr_description",
	} {
		got := priorityForActionID(id)
		if got != PriorityMedium {
			t.Errorf("priorityForActionID(%q) = %q, want %q", id, got, PriorityMedium)
		}
	}
}

func TestPriorityForActionIDSupportingPriority(t *testing.T) {
	for _, id := range []string{
		"pr_review_summary",
		"context_scattered_review_plan",
		"context_improve_test_proximity",
		"context_hotspot_regression_focus",
	} {
		got := priorityForActionID(id)
		if got != PrioritySupporting {
			t.Errorf("priorityForActionID(%q) = %q, want %q", id, got, PrioritySupporting)
		}
	}
}

func TestPriorityForActionIDDefaultIsMedium(t *testing.T) {
	got := priorityForActionID("unknown_action_xyz")
	if got != PriorityMedium {
		t.Errorf("unknown action ID should default to medium, got %q", got)
	}
}

func TestSortRequiredActionsHighBeforeMediumBeforeSupporting(t *testing.T) {
	in := []RequiredAction{
		{ID: "pr_review_summary", Priority: PrioritySupporting},
		{ID: "auth_e2e_gate", Priority: PriorityHigh},
		{ID: "workflow_config_validation", Priority: PriorityMedium},
	}
	out := SortRequiredActions(in)
	if len(out) != 3 {
		t.Fatalf("expected 3 actions, got %d", len(out))
	}
	if out[0].ID != "auth_e2e_gate" {
		t.Errorf("first should be high-priority auth_e2e_gate, got %q", out[0].ID)
	}
	if out[1].ID != "workflow_config_validation" {
		t.Errorf("second should be medium-priority workflow_config_validation, got %q", out[1].ID)
	}
	if out[2].ID != "pr_review_summary" {
		t.Errorf("third should be supporting pr_review_summary, got %q", out[2].ID)
	}
}

func TestSortRequiredActionsSamePriorityByID(t *testing.T) {
	// Same priority tier — secondary sort is alphabetical by ID.
	in := []RequiredAction{
		{ID: "rag_qna_citations_gate", Priority: PriorityHigh},
		{ID: "auth_e2e_gate", Priority: PriorityHigh},
		{ID: "add_tests_or_evidence", Priority: PriorityHigh},
	}
	out := SortRequiredActions(in)
	if out[0].ID != "add_tests_or_evidence" {
		t.Errorf("first alphabetically among high: want add_tests_or_evidence, got %q", out[0].ID)
	}
	if out[1].ID != "auth_e2e_gate" {
		t.Errorf("second: want auth_e2e_gate, got %q", out[1].ID)
	}
	if out[2].ID != "rag_qna_citations_gate" {
		t.Errorf("third: want rag_qna_citations_gate, got %q", out[2].ID)
	}
}

func TestSortRequiredActionsSingleElement(t *testing.T) {
	in := []RequiredAction{{ID: "auth_e2e_gate", Priority: PriorityHigh}}
	out := SortRequiredActions(in)
	if len(out) != 1 || out[0].ID != "auth_e2e_gate" {
		t.Errorf("single element should be returned unchanged")
	}
}

func TestSortRequiredActionsNilInput(t *testing.T) {
	out := SortRequiredActions(nil)
	if out != nil {
		t.Errorf("nil input should return nil, got %v", out)
	}
}

func TestSortRequiredActionsDoesNotMutateInput(t *testing.T) {
	in := []RequiredAction{
		{ID: "pr_review_summary", Priority: PrioritySupporting},
		{ID: "auth_e2e_gate", Priority: PriorityHigh},
	}
	orig := in[0].ID
	SortRequiredActions(in)
	if in[0].ID != orig {
		t.Error("SortRequiredActions should not mutate the input slice")
	}
}
