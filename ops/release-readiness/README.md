# TalkBack release readiness

Deterministic, evidence-based PR gate before deploy. Scoring and PASS/WARN/BLOCK come from [`release-readiness-core`](https://github.com/psuthar/release-readiness-core), pinned in [`version.txt`](./version.txt). No LLM in the decision path; an LLM may summarize `report.json` afterward.

The migration to `release-readiness-core` (epic SCRUM-261) is **complete**:

- SCRUM-262: pin file (`version.txt`) committed.
- SCRUM-263 / 264: gap-test harness + `pr-risk-config.yaml` reached parity with the legacy Go engine.
- SCRUM-265: shadow gate ran alongside legacy on every PR.
- SCRUM-266: 22-PR soak window (see [`shadow-rollout.md`](./shadow-rollout.md)) — 0 unexplained disagreements.
- SCRUM-267: legacy in-tree gate (`cmd/prrisk`, `internal/prrisk`, `scripts/{pr_gate,release_readiness,evaluate_pr_risk_semantic,e2e_to_readiness}.py`, `scripts/release-readiness.sh`) deleted; CI is now release-readiness-core only.

## What it does

1. **Evidence collection** — Go tests, Playwright e2e, coverage, prod health, git diff vs base ref.
2. **PR risk** — `release-readiness-pr-risk` writes `pr_risk.json` / `pr_risk.md` plus a stable `artifacts/pr-risk.json` summary.
3. **Readiness scoring** — `release-readiness-evaluate` writes `artifacts/release-readiness/report.json`, `report.md`, and the lean machine summary `artifacts/release-readiness.json`.
4. **Unified gate** — `release-readiness-combine` produces `pr-gate-summary.json` / `pr-gate-summary.md` (PASS/WARN/BLOCK from PR Risk + Release Readiness).
5. **GitHub Check** — `release-readiness-check-payload --check-name "TalkBack PR Gate" --warn-conclusion action_required` writes `pr-gate-check.json`. The workflow's github-script step publishes it; payload conclusion flows through unmodified (no remap).

## Pin upgrade workflow

The pin in [`version.txt`](./version.txt) is the **single source of truth** for which `release-readiness-core` build CI installs.

- Reader: [`scripts/release_readiness_core_pin.sh`](../../scripts/release_readiness_core_pin.sh) — emits `version`, `sha`, `ref`, or `spec` (the latter is what `pip install` consumes).
- To bump: edit `version.txt` (set `version=`, `sha=`, `ref=` together), run the [`shadow-rollout.md`](./shadow-rollout.md) tracking once on the next PR to confirm parity.
- All seven CLIs (`release-readiness-evaluate`, `release-readiness-pr-risk`, `release-readiness-combine`, `release-readiness-check-payload`, `playwright-to-readiness`, `release-readiness-doctor`, `release-readiness-init`) ship at every pinned version.

## Core validations (mapped to changed paths)

| Area | Typical paths |
|------|----------------|
| auth/session | `internal/auth/**`, `internal/invitations/**`, session DB |
| upload/extraction | `session_materials`, `processing`, `transcript` |
| nav assets | `MaterialsTreePanel` in web |
| viewer | `SlideDeckViewer`, modes |
| Q&A / RAG | `session_ask`, `rag` |
| orchestration | `internal/orchestration/**`, `session_orchestration*`, creator recommendations UI |
| migrations | `db/migrations`, `internal/migrations` |

When a risk category is triggered by the diff, the report must show that validation satisfied **unless** smoke/e2e infer coverage (`infer_validations_when_pass` in `config.yaml`). **Migrations** require explicit `migrations_validated: true` in smoke JSON or `--migration-validated` / CI flag.

## Run locally

Install the pinned `release-readiness-core` plus the small auxiliary deps used by config validation:

```bash
pip install "$(scripts/release_readiness_core_pin.sh spec)"
pip install -r ops/release-readiness/requirements.txt
```

Then drive the four CLIs against your working tree:

```bash
mkdir -p artifacts/release-readiness

# 1) PR risk
release-readiness-pr-risk \
  --repo-root . \
  --base-ref origin/main \
  --output-dir artifacts/release-readiness \
  --config ops/release-readiness/pr-risk-config.yaml

# 2) Smoke / coverage / e2e evidence — same as CI:
go test ./... -count=1
echo '{"status":"passed","failed_count":0,"total_count":1,"validations":{"migrations_validated":true}}' > smoke_results.json

# (optional, post-Playwright)
playwright-to-readiness \
  --input playwright-results.json \
  --output e2e_results.json \
  --validation-map ops/release-readiness/e2e-validation-map.yaml

# 3) Readiness evaluate
release-readiness-evaluate \
  --repo-root . \
  --config ops/release-readiness/config.yaml \
  --base-ref origin/main \
  --smoke-results smoke_results.json \
  --e2e-results e2e_results.json \
  --output-dir artifacts/release-readiness

# 4) Combine into the unified gate + GitHub Checks payload
release-readiness-combine \
  --pr-risk-json artifacts/release-readiness/pr-risk.json \
  --readiness-json artifacts/release-readiness.json \
  --readiness-report-json artifacts/release-readiness/run/report.json \
  --output-dir artifacts

release-readiness-check-payload \
  --gate-json artifacts/pr-gate-summary.json \
  --output artifacts/pr-gate-check.json \
  --check-name "TalkBack PR Gate" \
  --warn-conclusion action_required
```

`final_gate.status` in `pr-gate-summary.json` is the authoritative PASS / WARN / BLOCK verdict.

## CI

`.github/workflows/release-readiness.yml` (one job, `release-readiness`) runs on `pull_request`, `push`, and `workflow_dispatch`:

1. Set up Go + Python; install `release-readiness-core` from the pin.
2. **Pre-flight** — `release-readiness-doctor` validates `config.yaml` + `pr-risk-config.yaml`.
3. **PR risk scoring** — `release-readiness-pr-risk` writes `pr-risk.json` + `pr_risk.md`.
4. **Go tests + smoke_results.json + coverage.json** — same as before.
5. **Frontend build + API server + Playwright e2e** — collect `playwright-results.json`.
6. **Convert e2e** — `playwright-to-readiness` writes `e2e_results.json` (validation-map at [`e2e-validation-map.yaml`](./e2e-validation-map.yaml)).
7. **Run release-readiness-evaluate** — writes `report.json`, `report.md`, lean `release-readiness.json`.
8. **Evaluate Release Readiness Outcome** — fails the job on `BLOCK` (or on `WARN` when `READINESS_ENFORCEMENT_MODE=warn_and_block`).
9. **Run release-readiness-combine** — writes `pr-gate-summary.json` + `.md`.
10. **Build PR gate GitHub Check payload** — `release-readiness-check-payload --warn-conclusion action_required`.
11. **Upload Release Readiness Artifact** — uploads `artifacts/`, `smoke_results.json`, `e2e_results.json`, `coverage.json`.
12. **Publish TalkBack PR Gate check** — github-script reads `pr-gate-check.json` and updates the named check; conclusion flows through unmodified.
13. **Post PR gate comment** — unified table on pull requests.
14. **Add PR Summary** — release-readiness lines appended to the job summary UI.
15. **Enforce TalkBack PR gate outcome** — exits 1 when `workflow_should_fail` is true (BLOCK).

### Branch protection

The reviewer-facing semantic status is the GitHub Check `TalkBack PR Gate`. After the SCRUM-267 cutover, with `--warn-conclusion action_required`, the mapping is:

| Final Gate status | Check conclusion | Effect |
|-------------------|------------------|--------|
| `PASS` | `success` | green check, merge allowed |
| `WARN` | `action_required` | yellow check, merge blocked by branch protection |
| `BLOCK` | `failure` | red check, merge blocked |

Add a branch-protection required check on `TalkBack PR Gate` to enforce these semantics independently of other CI jobs.

### Phase-3 (optional) strictness

To turn the workflow status RED on WARN (so the workflow itself fails, not just the published check), bump `--warn-conclusion failure` in step 10. This is the strictest enforcement and only worth adopting once the team is comfortable with the Phase-2 baseline.

### E2E debug artifacts (CI)

The release-readiness job stages Playwright outputs under `artifacts/e2e-playwright/`, included in the `release-readiness` artifact upload (`if: always()`):

| Path | Contents |
|------|----------|
| `README.txt` | Short summary: failed specs (file + title when `ok: false`), plus `e2e_results.json` fields |
| `playwright-results.json` | Raw `npx playwright test --reporter=json` output |
| `e2e_results.json` | Output of `playwright-to-readiness` |
| `playwright-report/` | HTML report — open `index.html` locally after download |
| `test-results/` | Traces, screenshots (from Playwright config: trace on first retry, screenshot on failure) |

## E2E validation map

The mapping from Playwright spec stems → readiness validation keys lives in [`e2e-validation-map.yaml`](./e2e-validation-map.yaml). Edit there when you add a new e2e file:

1. Identify which validation(s) the test exercises (`auth_session`, `upload_extraction`, `nav_assets`, `viewer_materials`, `qa_rag`, …).
2. Add the test's file stem (without `.e2e.ts` / `.spec.ts`) to the matching list.
3. The `playwright-to-readiness` CLI picks it up on the next CI run.

A validation is `true` if all tests in its group pass, `false` if any fail, and absent if no tests from that group ran (so the engine can apply inference instead of an explicit `false`).

## Suppressing the risky-config warning via commit messages

When `.github/workflows/**`, `Dockerfile`, `deploy/**`, `go.mod`, or other paths listed under `risky_config_patterns` in `config.yaml` change, the readiness check warns:

> Risky config/workflow paths changed without validation note

Suppress by including a `Validation:` (or `Validate:`) section in **any commit** in the range:

```
Fix: update Dockerfile base image to Go 1.23

Validation:
- ran workflow_dispatch on staging branch
- confirmed Docker build succeeded and API /health returned ok
- verified e2e smoke suite passed end-to-end
```

`release-readiness-evaluate` scans `git log base_ref...HEAD --format=%B` for a line beginning with `Validation:` or `Validate:` (case-insensitive); when found, the warning and its score penalty are suppressed.

## Remediation guidance

Every WARN or BLOCK `report.json` includes a `remediation_items` array (mirrored in `report.md`). Each row maps directly to a failed check (`check`, `severity`, `likely_cause`, `recommended_action`, `fix_type`). Guidance is config-driven via `remediation:` in `config.yaml`.

## Extending

- **New Relic / deploy health**: write `prod_health.json`, pass `--prod-health` to `release-readiness-evaluate`.
- **Coverage**: emit `coverage.json` with `line_percent` and `baseline_percent`.
- **New e2e file**: add its stem to [`e2e-validation-map.yaml`](./e2e-validation-map.yaml).
