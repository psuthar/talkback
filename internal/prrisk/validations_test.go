package prrisk

import (
	"strings"
	"testing"
)

func TestValidationsBaselineCIFirst(t *testing.T) {
	s := Signals{DomainHits: map[string]int{}}
	vs := ComputeRequiredValidations(s, nil)
	if len(vs) == 0 || vs[0] != "ci: required status checks must pass before merge" {
		t.Errorf("expected baseline CI as first validation, got %v", vs)
	}
}

func TestValidationsGitErrorBaselineReplaced(t *testing.T) {
	s := Signals{GitError: "no merge base"}
	vs := ComputeRequiredValidations(s, nil)
	if len(vs) == 0 {
		t.Fatal("expected at least one validation when git error")
	}
	want := "ci: restore reliable git diff before merge (see git error in report)"
	if vs[0] != want {
		t.Errorf("expected git error baseline as first line, got %q", vs[0])
	}
	for _, v := range vs {
		if v == "ci: required status checks must pass before merge" {
			t.Error("standard CI baseline must not appear when git error is set")
		}
	}
}

func TestValidationsActionMappings(t *testing.T) {
	cases := []struct {
		id      string
		wantSub string
	}{
		{"auth_e2e_gate", "auth/session"},
		{"rag_qna_citations_gate", "Q&A"},
		{"materials_processing_gate", "materials upload"},
		{"migrations_validation_gate", "migrations"},
		{"add_tests_or_evidence", "tests or recorded evidence"},
		{"workflow_config_validation", "workflow / deploy"},
		{"ci_fetch_depth_zero", "full git history"},
		{"pr_review_summary", "scoped"},
		{"context_align_pr_description", "intent match"},
		{"context_scattered_review_plan", "scattered"},
		{"context_improve_test_proximity", "co-located"},
		{"context_hotspot_regression_focus", "targeted regression"},
	}
	s := Signals{}
	for _, c := range cases {
		vs := ComputeRequiredValidations(s, []RequiredAction{{ID: c.id}})
		found := false
		for _, v := range vs {
			if strings.Contains(v, c.wantSub) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("action %q: expected validation containing %q, got %v", c.id, c.wantSub, vs)
		}
	}
}

func TestValidationsUnknownActionIgnored(t *testing.T) {
	s := Signals{}
	vs := ComputeRequiredValidations(s, []RequiredAction{{ID: "unknown_action_xyz"}})
	// Only the baseline CI line expected — unknown action produces no validation.
	if len(vs) != 1 {
		t.Errorf("expected only baseline CI line for unknown action ID, got %v", vs)
	}
}

func TestValidationsValidationNoteAppended(t *testing.T) {
	s := Signals{ValidationNoteFound: true}
	vs := ComputeRequiredValidations(s, nil)
	found := false
	for _, v := range vs {
		if strings.Contains(v, "validation note") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected validation note line when ValidationNoteFound=true, got %v", vs)
	}
}

func TestValidationsDeterministicOrder(t *testing.T) {
	// Same action IDs in different order must produce identical validation lists.
	s := Signals{}
	acts1 := []RequiredAction{{ID: "auth_e2e_gate"}, {ID: "add_tests_or_evidence"}}
	acts2 := []RequiredAction{{ID: "add_tests_or_evidence"}, {ID: "auth_e2e_gate"}}
	vs1 := ComputeRequiredValidations(s, acts1)
	vs2 := ComputeRequiredValidations(s, acts2)
	if len(vs1) != len(vs2) {
		t.Fatalf("expected same length for same action set, got %d vs %d", len(vs1), len(vs2))
	}
	for i := range vs1 {
		if vs1[i] != vs2[i] {
			t.Errorf("position %d differs: %q vs %q", i, vs1[i], vs2[i])
		}
	}
}

func TestValidationsNoDuplicates(t *testing.T) {
	// Same action emitted twice should produce only one validation entry.
	s := Signals{}
	acts := []RequiredAction{{ID: "auth_e2e_gate"}, {ID: "auth_e2e_gate"}}
	vs := ComputeRequiredValidations(s, acts)
	seen := map[string]int{}
	for _, v := range vs {
		seen[v]++
	}
	for v, c := range seen {
		if c > 1 {
			t.Errorf("duplicate validation line: %q (count %d)", v, c)
		}
	}
}
