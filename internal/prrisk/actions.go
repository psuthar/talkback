package prrisk

import (
	"fmt"

	riskcontext "github.com/psuthar/talkback/internal/prrisk/context"
)

func ComputeRequiredActions(s Signals, factors []RiskFactor, reducers []RiskReducer, riskScore float64, riskBand string, insights *riskcontext.ContextInsights) []RequiredAction {
	has := factorIDs(factors)

	// Evidence levels per sensitive domain.
	evidenceLevel := func(domain string) string {
		if s.TestE2EDomainHits != nil && s.TestE2EDomainHits[domain] > 0 {
			return "e2e"
		}
		if s.TestUnitDomainHits != nil && s.TestUnitDomainHits[domain] > 0 {
			return "unit"
		}
		return "none"
	}

	hasValidationNote := s.ValidationNoteFound

	gateCritical := riskBand == "critical" || riskScore >= 70
	gateHigh := riskBand == "high" || riskScore >= 45

	var out []RequiredAction
	seen := make(map[string]struct{})
	add := func(a RequiredAction) {
		if _, ok := seen[a.ID]; ok {
			return
		}
		if a.Priority == "" {
			a.Priority = priorityForActionID(a.ID)
		}
		seen[a.ID] = struct{}{}
		out = append(out, a)
	}

	// Always required if we couldn't compute a reliable diff range.
	if okBool(has, "git_unavailable") {
		add(RequiredAction{
			ID:          "ci_fetch_depth_zero",
			Title:       "Ensure git history is available for diff",
			FixType:     "infra",
			AppliesWhen: "git diff base...HEAD was unavailable",
			Checklist: []string{
				"Confirm CI uses `fetch-depth: 0` (or an equivalent full-history checkout).",
				"Re-run PR risk scoring after the checkout depth fix.",
			},
		})
	}

	// Diff-size / review hygiene — always when large-diff signals fire (trust / review load).
	if okBool(has, "diff_large") || okBool(has, "diff_very_large") || okBool(has, "many_files") {
		add(RequiredAction{
			ID:      "pr_review_summary",
			Title:   "Make PR review scoped and evidence-backed",
			FixType: "process",
			Checklist: []string{
				"Add a PR description summary: what changed and why.",
				"Group changes by subsystem so reviewers can validate quickly.",
			},
		})
	}

	// Workflow / config validation — always when these factors are present.
	if okBool(has, "ci_workflows") || okBool(has, "deploy_config") || okBool(has, "go_mod_deps") {
		msg := "Confirm required checks and env parity before merge."
		if hasValidationNote {
			msg = "Validation note is present; confirm required checks and env parity before merge."
		}
		add(RequiredAction{
			ID:      "workflow_config_validation",
			Title:   "Validate workflow / deploy config changes",
			FixType: "config",
			Checklist: []string{
				msg,
				"If CI fails, identify whether it is test flakiness vs behavior change and update evidence accordingly.",
			},
		})
	}

	// Sensitive domains.
	if gateCritical || gateHigh {
		if okBool(has, "domain_auth") {
			level := evidenceLevel(DomainAuth)
			title := "Validate auth/session flows (login, invite, participant)"
			check := []string{
				"Ensure auth E2E coverage is green for the affected flow(s).",
				"Spot-check cookie/session behavior changes in staging-like conditions (SameSite, HTTPS).",
			}
			if level == "none" {
				check[0] = "Run auth/session E2E flows before merge (login, invite, participant)."
			} else if level == "unit" {
				check[0] = "Confirm auth unit tests pass; run auth E2E smoke for login/invite/participant before merge."
			}
			add(RequiredAction{
				ID:          "auth_e2e_gate",
				Title:       title,
				FixType:     "test",
				AppliesWhen: "auth/session/invite domain changed",
				Checklist:   check,
			})
		}

		if okBool(has, "domain_rag") {
			level := evidenceLevel(DomainRAG)
			checklist := []string{
				"Run `qa_rag`-targeted E2E smoke and confirm citations attach to answers.",
				"If relevant, re-index or verify embedding job health post-deploy.",
			}
			if level == "none" {
				checklist[0] = "Run Q&A with citations E2E before merge (session ask + citations verification)."
			} else if level == "unit" {
				checklist[0] = "Confirm unit-level RAG changes pass; run Q&A-with-citations E2E smoke before merge."
			}
			add(RequiredAction{
				ID:          "rag_qna_citations_gate",
				Title:       "Validate Q&A with citations for decision-grade answers",
				FixType:     "test",
				AppliesWhen: "RAG / Q&A pipelines changed",
				Checklist:   checklist,
			})
		}

		if okBool(has, "domain_processing") {
			level := evidenceLevel(DomainProcessing)
			checklist := []string{
				"Run a materials upload + processing smoke on a representative file.",
				"Confirm transcript/job worker logs look healthy (no silent failures).",
			}
			if level == "none" {
				checklist[0] = "Run materials upload + processing smoke before merge (representative file)."
			} else if level == "unit" {
				checklist[0] = "Confirm processing unit tests pass; run processing smoke before merge."
			}
			add(RequiredAction{
				ID:          "materials_processing_gate",
				Title:       "Validate materials upload + processing pipeline",
				FixType:     "process",
				AppliesWhen: "processing/transcription pipeline changed",
				Checklist:   checklist,
			})
		}
		if okBool(has, "domain_orchestration") {
			level := evidenceLevel(DomainOrchestration)
			checklist := []string{
				"Run creator orchestration recommendation flow checks (list/sync + approve/reject draft paths).",
				"Confirm no autonomous send/post behavior is introduced in orchestration paths.",
			}
			if level == "none" {
				checklist[0] = "Run orchestration smoke/E2E before merge (recommendations panel + draft approve/reject)."
			} else if level == "unit" {
				checklist[0] = "Confirm orchestration unit/integration tests pass; run creator orchestration smoke before merge."
			}
			add(RequiredAction{
				ID:          "orchestration_creator_gate",
				Title:       "Validate creator orchestration recommendation flows",
				FixType:     "test",
				AppliesWhen: "orchestration recommendation/review paths changed",
				Checklist:   checklist,
			})
		}

		if okBool(has, "domain_migrations") {
			level := evidenceLevel(DomainMigrations)
			checklist := []string{
				"Run migrations with validation evidence and confirm expected schema/data behavior.",
				"Verify rollback plan (or migration reversal strategy) is documented and executable.",
			}
			if level == "e2e" {
				checklist[0] = "Ensure migration validation tests/evidence are part of CI and are green before merge."
			} else if level == "unit" {
				checklist[0] = "Confirm unit coverage exists for migrations; run migration validation smoke before merge."
			}
			add(RequiredAction{
				ID:          "migrations_validation_gate",
				Title:       "Validate database migrations before merge",
				FixType:     "db",
				AppliesWhen: "migration files changed",
				Checklist:   checklist,
			})
		}

		if okBool(has, "tests_missing") {
			add(RequiredAction{
				ID:          "add_tests_or_evidence",
				Title:       "Add/update tests (or record evidence) before merge",
				FixType:     "test",
				AppliesWhen: "sensitive code changed without any test file changes in this diff",
				Checklist: []string{
					"Add or update unit/integration tests for the changed packages.",
					"Re-run `go test ./...` and ensure E2E smoke covers the sensitive area(s).",
				},
			})
		}
	}

	// If risk is only medium, still require evidence for tests_missing and workflow/config changes.
	if !gateHigh && okBool(has, "tests_missing") {
		add(RequiredAction{
			ID:      "add_tests_or_evidence",
			Title:   "Add/update tests before merge",
			FixType: "test",
			Checklist: []string{
				"Add or update tests for changed code paths and confirm `go test ./...` passes.",
			},
		})
	}

	if insights != nil {
		if insights.Intent.Mismatch {
			add(RequiredAction{
				ID:      "context_align_pr_description",
				Title:   "Align PR title/description with the diff",
				FixType: "process",
				Checklist: []string{
					"Update the PR title or body so keywords match the areas actually changed, or narrow the diff to match the stated intent.",
					"If the scope is intentional, explain why expected domains are not touched.",
				},
			})
		}
		if insights.Concentration.Mode == "scattered" && s.FileCount >= 10 {
			add(RequiredAction{
				ID:      "context_scattered_review_plan",
				Title:   "Structure review for a scattered change",
				FixType: "process",
				Checklist: []string{
					"Add a short map of files grouped by subsystem (or commit) to speed review.",
					"Call out cross-cutting concerns explicitly (auth, DB, RAG, web).",
				},
			})
		}
		if insights.Proximity.Mode == "distant" && insights.Proximity.NonTestFiles >= 2 && hasSensitiveDomainHit(s) {
			add(RequiredAction{
				ID:      "context_improve_test_proximity",
				Title:   "Improve test proximity for changed code",
				FixType: "test",
				Checklist: []string{
					"Add or reference tests in the same package or directory as changed production files.",
					"If tests live elsewhere, link them in the PR description.",
				},
			})
		}
		if len(insights.Hotspots) > 0 {
			p := insights.Hotspots[0].Prefix
			add(RequiredAction{
				ID:      "context_hotspot_regression_focus",
				Title:   "Extra regression focus on active path (recent commits)",
				FixType: "process",
				Checklist: []string{
					fmt.Sprintf("Prefix `%s` is active in recent history; run targeted smoke for behavior touching this area.", p),
					"Watch for unintended side effects in adjacent modules.",
				},
			})
		}
	}

	_ = reducers

	return SortRequiredActions(out)
}

// hasSensitiveDomainHit reports whether any sensitive domain has file hits in this diff.
// Sensitive domains require elevated test evidence: auth, rag, processing, migrations, api, database.
func hasSensitiveDomainHit(s Signals) bool {
	return s.DomainHits[DomainAuth] > 0 ||
		s.DomainHits[DomainRAG] > 0 ||
		s.DomainHits[DomainProcessing] > 0 ||
		s.DomainHits[DomainOrchestration] > 0 ||
		s.DomainHits[DomainMigrations] > 0 ||
		s.DomainHits[DomainAPI] > 0 ||
		s.DomainHits[DomainDatabase] > 0
}
