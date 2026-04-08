package prrisk

import (
	"path/filepath"
)

// mitigationMap is factor ID → recommended actions (deterministic).
var mitigationMap = map[string][]string{
	"git_unavailable": {
		"Ensure CI checks out enough history (`fetch-depth: 0`) so `git diff base...HEAD` works.",
		"Run `go run ./cmd/prrisk --base-ref <ref>` locally with a valid base.",
	},
	"diff_very_large": {
		"Split the PR into smaller reviews (backend vs frontend vs docs).",
		"Add a high-level summary of behavioral changes in the PR description.",
	},
	"diff_large": {
		"Review commit-by-commit; consider feature flags for risky paths.",
	},
	"many_files": {
		"Group changes by subsystem in the PR description for reviewers.",
	},
	"domain_auth": {
		"Run auth/session flows manually or via E2E (login, invite, participant).",
		"Verify cookie/session settings in staging (SameSite, HTTPS).",
	},
	"domain_migrations": {
		"Run migrations against a copy of prod-like data; verify rollback plan.",
		"Coordinate deploy order (API before/after migration as required).",
	},
	"domain_rag": {
		"Smoke-test Q&A with citations on a session with materials.",
		"Watch embedding/index job logs after deploy.",
	},
	"domain_processing": {
		"Run material upload + processing pipeline smoke on a sample file.",
		"Confirm Whisper/job worker env in target environment.",
	},
	"domain_orchestration": {
		"Validate creator orchestration flows (recommendation list/sync, draft approve/reject).",
		"Verify orchestration remains human-in-the-loop (no autonomous send/post actions).",
	},
	"web_large": {
		"Run `npm run build` and spot-check creator/participant UIs.",
		"Cross-browser smoke if CSS/layout changed.",
	},
	"ci_workflows": {
		"Validate workflow YAML in a fork or `act` where possible.",
		"Confirm secrets and required checks still match branch protection.",
	},
	"deploy_config": {
		"Review Render/Docker/env parity with production.",
		"Schedule deploy during low-traffic window if infra changes.",
	},
	"go_mod_deps": {
		"Run `go test ./...` and check for transitive license/compatibility issues.",
		"Consider `go mod verify` in CI.",
	},
	"tests_missing": {
		"Add or update unit/integration tests for changed packages.",
		"If behavior is unchanged refactor-only, note that in the PR description.",
	},
	"context_test_proximity_distant": {
		"Add tests next to changed packages or link existing tests in the PR description.",
		"Prefer package-local *_test.go over only end-to-end coverage for the same change.",
	},
	"context_change_scattered": {
		"Split the PR description by subsystem or commit so reviewers can validate incrementally.",
		"Consider follow-up PRs if the scatter is accidental.",
	},
	"context_hotspot_overlap": {
		"Run focused regression on the overlapping prefix; several recent commits touched it, so regressions are likelier.",
		"Scan related modules for unintended behavior changes.",
	},
	"context_intent_mismatch": {
		"Update the PR title/body to match the diff, or adjust the diff to match the stated intent.",
		"If intentional, explain the scope change explicitly for reviewers.",
	},
}

// Mitigate returns mitigations for each factor that has a non-empty mapping.
func Mitigate(factors []RiskFactor) []Mitigation {
	var out []Mitigation
	seen := make(map[string]struct{})
	for _, f := range factors {
		if _, ok := seen[f.ID]; ok {
			continue
		}
		seen[f.ID] = struct{}{}
		actions := mitigationMap[f.ID]
		if len(actions) == 0 {
			actions = []string{"Review this factor in the context of your change and add validation notes to the PR."}
		}
		out = append(out, Mitigation{FactorID: f.ID, Actions: actions})
	}
	return out
}

// goModChanged reports whether go.mod or go.sum changed.
func goModChanged(s Signals) bool {
	for _, f := range s.Files {
		b := filepath.Base(f.Path)
		if b == "go.mod" || b == "go.sum" {
			return true
		}
	}
	return false
}
