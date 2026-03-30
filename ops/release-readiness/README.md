# TalkBack release readiness (v1)

Deterministic, evidence-based checks before deploy. **Scoring and PASS/WARN/BLOCK are computed only by `scripts/release_readiness.py`** — no LLM in the decision path. An LLM may summarize `report.json` afterward only.

## What it does

1. **Evidence collection** — reads optional JSON artifacts (smoke, E2E, coverage, prod health) and `git diff` vs a base ref.
2. **Scoring** — applies YAML rules (`config.yaml`): blockers (smoke fail, critical E2E, migrations without validation, risky paths without validation evidence), warnings (missing artifacts, E2E retries, coverage drop, risky config paths).
3. **Outputs** — `artifacts/release-readiness/report.json`, `report.md`, and a **machine summary** at `artifacts/release-readiness.json` (`outcome`, `score`, `warnings`, `blockers`) for CI gates and PR summaries.
4. **PR risk (v2.6)** — deterministic diff-based risk from `go run ./cmd/prrisk`, emitting `pr_risk.json` and `pr_risk.md` under the output directory, plus a stable machine summary at **`artifacts/pr-risk.json`** (score, band, `merge_recommendation` as `PASS`/`WARN`/`BLOCK`, `required_validations`, `top_risk_factors`). The readiness script reads `pr_risk.json` and caps the outcome: PR Risk BLOCK → readiness BLOCK; PR Risk WARN → readiness at most WARN.
5. **Unified PR gate** — `scripts/pr_gate.py` combines PR Risk + Release Readiness into **`pr-gate-summary.json`** / **`pr-gate-summary.md`**. **`scripts/pr_gate_check_payload.py`** turns that JSON into **`artifacts/pr-gate-check.json`** for the GitHub Check **TalkBack PR Gate** (no duplicated combining rules in YAML).

CI runs `bash scripts/release-readiness.sh` (wrapper around `scripts/release_readiness.py`). If the evaluator crashes before writing `report.json`, the wrapper sets `READINESS_FAILED=true` in `GITHUB_ENV` for the final gate step.

## Core validations (mapped to changed paths)

| Area | Typical paths |
|------|----------------|
| auth/session | `internal/auth/**`, `internal/invitations/**`, session DB |
| upload/extraction | `session_materials`, `processing`, `transcript` |
| nav assets | `MaterialsTreePanel` in web |
| viewer | `SlideDeckViewer`, modes |
| Q&A / RAG | `session_ask`, `rag` |
| migrations | `db/migrations`, `internal/migrations` |

When a risk category is triggered by the diff, the report must show that validation satisfied **unless** smoke/E2E pass and infer coverage (see `infer_validations_when_pass` in `config.yaml`). **Migrations** require explicit `migrations_validated: true` in smoke JSON or `--migration-validated` / CI flag.

## Run locally

```bash
cd /path/to/talkback
python -m pip install -r ops/release-readiness/requirements.txt

# Demo: fixtures + no path risks (deterministic PASS)
python scripts/release_readiness.py --fixture-mode --output-dir artifacts/release-readiness

# Real: run Go tests, write smoke_results.json, then evaluate
if go test ./... -count=1; then
  echo '{"status":"passed","failed_count":0}' > smoke_results.json
else
  echo '{"status":"failed","failed_count":1}' > smoke_results.json
fi

python scripts/release_readiness.py \
  --config ops/release-readiness/config.yaml \
  --base-ref origin/main \
  --smoke-results smoke_results.json \
  --output-dir artifacts/release-readiness

# Optional but recommended: PR risk v2.6 (git diff signals; no Python)
# Must run BEFORE release_readiness.py if you want PR Risk to influence the outcome.
go run ./cmd/prrisk --repo-root . --base-ref origin/main --output-dir artifacts/release-readiness
```

Exit code: `0` for PASS (or WARN in `block_only` mode), `1` for BLOCK (or WARN in `warn_and_block` mode).

### Enforcement mode

| Mode | Exit 1 when | How to set |
|------|-------------|------------|
| `block_only` *(default)* | BLOCK | default |
| `warn_and_block` | WARN **or** BLOCK | `--enforcement-mode warn_and_block` or `READINESS_ENFORCEMENT_MODE=warn_and_block` |

The recommended initial policy is **`block_only`**: warnings are visible in the report and artifact but do not fail the workflow. Move to `warn_and_block` once the team is comfortable with the baseline warning rate.

### Environment variables

| Variable | Meaning |
|----------|---------|
| `RELEASE_READINESS_BASE_REF` | Default `origin/main` for `--base-ref` |
| `PRRISK_JIRA_ISSUE_KEY` | Optional; echoed into PR risk v2.x `integrations` for future Jira linking |
| `READINESS_ENFORCEMENT_MODE` | `block_only` (default) or `warn_and_block` |

## CI

GitHub Actions workflow `.github/workflows/release-readiness.yml` runs on `pull_request` and `workflow_dispatch`:

1. Runs `go run ./cmd/prrisk` (PR risk v2.6) with `continue-on-error: true`, writes `pr_risk.json`, `pr_risk.md`, and **`artifacts/pr-risk.json`**.
2. Runs `go test ./...` and writes `smoke_results.json`.
3. Installs Node 22, runs `npm install` in `web/`, and installs Playwright (chromium only).
4. Builds the React frontend (`npm run build`).
5. Starts the Go API in the background on port 8081 against the CI PostgreSQL service.
6. Waits up to 30s for the API health check at `/health`.
7. Runs `npx playwright test --reporter=json` with `|| true` so test failures do not abort the step.
8. Runs `python scripts/e2e_to_readiness.py` to convert the Playwright output to `e2e_results.json`.
9. Runs `bash scripts/release-readiness.sh` (with `continue-on-error: true` so later steps still run).
10. **Evaluate Release Readiness Outcome** — reads `artifacts/release-readiness.json` with `jq`; fails the job on `READINESS_FAILED`, on `BLOCK`, or on `WARN` when `READINESS_ENFORCEMENT_MODE=warn_and_block`; emits `::warning::` for `WARN` in `block_only` mode (workflow job stays green).
11. **Compute PR gate summary** — `python3 scripts/pr_gate.py` writes **`pr-gate-summary.json`** and **`pr-gate-summary.md`** (unified PASS/WARN/BLOCK from PR Risk + Release Readiness) and appends the **PR Gate** section to `GITHUB_STEP_SUMMARY`.
12. **Build PR gate GitHub Check payload** — `python3 scripts/pr_gate_check_payload.py` writes **`artifacts/pr-gate-check.json`** (title, summary, text, `workflow_should_fail`, `details_url`) from the gate JSON alone — no duplicated gate logic in YAML.
13. **Smoke-check action normalization** — optional guard on `normalize_action`.
14. **Evaluate PR risk semantic result** — `python3 scripts/evaluate_pr_risk_semantic.py` writes **`pr-risk-semantic.json`** (used for PR comment fallback and diagnostics).
15. **Upload Release Readiness Artifact** — uploads the entire **`artifacts/`** directory (`if: always()`), including gate files, readiness outputs, and PR risk outputs — runs **before** the gate check is published so BLOCK still uploads artifacts.
16. **Publish TalkBack PR Gate check** — GitHub Checks API (`checks: write`): creates or updates a check run named **`TalkBack PR Gate`** with conclusion **success** (PASS), **neutral** (WARN), or **failure** (BLOCK or gate/payload errors). **`details_url`** points at this workflow run when available.
17. **Post PR gate comment** — unified table on pull requests (or PR-risk-only fallback).
18. **Add PR Summary** — release readiness lines appended to the job summary UI.
19. **Enforce TalkBack PR gate outcome** — reads **`pr-gate-check.json`**; exits **1** when `workflow_should_fail` is true (unified **BLOCK** or gate could not be computed). Runs **after** upload and check publication.

### Branch protection vs TalkBack PR Gate

The primary reviewer-facing semantic status is the GitHub Check **`TalkBack PR Gate`**: **PASS** → green (success), **WARN** → yellow (neutral), **BLOCK** → red (failure). **WARN** is cautionary, not a merge block by itself; **BLOCK** is a hard stop. Add a branch protection required check on **`TalkBack PR Gate`** if you want those semantics enforced independently of other CI jobs. You may still require separate workflows for tests/builds. Fork PRs often cannot create checks with the default `GITHUB_TOKEN` (read-only); same-repo PRs need `permissions: checks: write`.

### E2E environment variables used in CI

| Variable | Value in CI | Purpose |
|----------|-------------|---------|
| `DATABASE_URL` | postgres service on localhost:5432 | API database |
| `PORT` | 8081 | API listen port (avoids clash with 8080 reserved by runner) |
| `RUN_MIGRATIONS` | true | Run DB migrations on API startup |
| `STORAGE_DRIVER` | local | Use local disk — no R2 credentials needed |
| `ENCRYPTION_KEY` | fixed CI value | Required for session token signing |
| `TALKBACK_API_BASE` | http://localhost:8081 | Playwright tests use this to call the API |
| `TALKBACK_ADMIN_EMAIL` | ci-admin@smoke.test | Admin user for test teardown |
| `TALKBACK_ADMIN_PASSWORD` | SmokePass123! | Admin password for teardown |

## E2E → readiness converter (`scripts/e2e_to_readiness.py`)

`scripts/e2e_to_readiness.py` converts the raw Playwright `--reporter=json` output into the `e2e_results.json` schema that `release_readiness_engine.py` expects.

### What it does

- Walks the Playwright suite tree to collect all leaf test specs.
- Maps each spec to one or more readiness validations by matching the spec's source file stem:

  | Validation | Source file stems |
  |------------|------------------|
  | `auth_session` | creator-access, participant-acceptance, invite-invalid-token, participant-happy-path, session-availability, session-routing |
  | `upload_extraction` | material-processing-state |
  | `nav_assets` | material-viewers |
  | `viewer_materials` | material-viewers, pptx-polling-stop |
  | `qa_rag` | qa-history |

- Sets a validation key to `true` if all tests in its group passed, `false` if any failed, or omits the key if no tests from that group ran (so the engine can apply inference instead of an explicit `false`).
- Counts retried tests (specs with more than one result entry) and surfaces them as `retries`.
- If the input file is missing or unparseable, writes a safe skipped/failed placeholder so downstream steps never fail on a missing artifact.

### Run locally

```bash
# After running Playwright with JSON reporter:
cd web
npx playwright test --reporter=json 2>/dev/null > ../playwright-results.json || true
cd ..

python scripts/e2e_to_readiness.py \
  --input playwright-results.json \
  --output e2e_results.json

cat e2e_results.json
```

### Test the converter without a browser

A minimal sample Playwright JSON fixture is provided at `fixtures/sample_e2e_playwright/playwright-results.json`:

```bash
python scripts/e2e_to_readiness.py \
  --input ops/release-readiness/fixtures/sample_e2e_playwright/playwright-results.json \
  --output /tmp/e2e_results.json

cat /tmp/e2e_results.json
# expected: status=passed, all 5 validations=true
```

## PR Risk integration

When `pr_risk.json` is present in the output directory (written by `go run ./cmd/prrisk`), the readiness engine reads the `enforcement.merge_recommendation` field and applies these deterministic caps:

| PR Risk recommendation | Effect on readiness |
|------------------------|---------------------|
| `pass` | No change; readiness outcome driven solely by smoke/E2E/coverage evidence |
| `warn` | Adds a warning; outcome is at most WARN even if all other checks pass |
| `block` | Adds a hard blocker; outcome is BLOCK regardless of other evidence |

The evidence summary (`pass_count`, `missing_count`, `unknown_count`, `fail_count`) from PR Risk is also surfaced in the readiness `reasons` list for report visibility.

**Graceful degradation:** if `pr_risk.json` is absent, unreadable, or has a parse error, the PR Risk block is silently skipped and readiness continues with evidence-only scoring.

**Ordering requirement:** `go run ./cmd/prrisk` must complete before `python scripts/release_readiness.py`. The CI workflow already ensures this ordering.

## Suppressing the risky-config warning via commit messages

When `.github/workflows/**`, `Dockerfile`, `deploy/**`, `go.mod`, or other paths listed under `risky_config_patterns` in `config.yaml` change, the readiness check warns:

> Risky config/workflow paths changed without validation note

You can suppress this warning by including a `Validation:` (or `Validate:`) section in **any commit** in the range being evaluated (`base_ref...HEAD`).  The section just needs to start the line — everything after the colon is the human-readable note:

```
Fix: update Dockerfile base image to Go 1.23

Validation:
- ran workflow_dispatch on staging branch
- confirmed Docker build succeeded and API /health returned ok
- verified E2E smoke suite passed end-to-end
```

The readiness script reads `git log base_ref...HEAD --format=%B` and scans for a line beginning with `Validation:` or `Validate:` (case-insensitive).  When found:

- `evidence.validation_note_present` → `true`
- `evidence.validation_note_source` → `"commit_message"`
- The `risky_config_without_note` warning and its score penalty are suppressed
- The markdown report row **Validation note** shows `yes (commit_message)`

This works for **direct commits to main/master** (push event) and **PR merges** (pull_request event) because the workflow already computes the correct `base_ref` for each event type:

| Event | `base_ref` used | Commit range scanned |
|-------|-----------------|----------------------|
| `pull_request` | `origin/<base-branch>` | PR head commits only |
| `push` | `github.event.before` (or `HEAD~1`) | The pushed commits |
| `workflow_dispatch` | `--base-ref` input (or `HEAD~1`) | Explicit or last commit |

## Remediation guidance

Every WARN or BLOCK report includes a **Remediation guidance** table in both `report.md` and `report.json` (`remediation_items` array). Each row maps directly to a failed check:

| Column | Meaning |
|--------|---------|
| `check` | The internal check key (e.g. `smoke_failed`, `e2e_critical`) |
| `severity` | `BLOCK` or `WARN` |
| `likely_cause` | Deterministic description of why this check fires |
| `recommended_action` | Concrete next step to resolve it |
| `fix_type` | Category: `code`, `test`, `config`, `process`, `infra`, `db` |

Guidance is config-driven — edit `remediation:` in `config.yaml` to adjust any entry. Unknown check keys fall back to a generic "Investigate check: <key>" entry.

### Branch protection with enforcement mode

To use the readiness outcome as a merge gate:

1. Enable **branch protection rules** on `main` in GitHub Settings → Branches.
2. Require the `release-readiness` status check to pass before merging.
3. Set enforcement mode in `.github/workflows/release-readiness.yml`:

```yaml
env:
  READINESS_ENFORCEMENT_MODE: block_only   # change to warn_and_block for stricter policy
```

**Recommended initial policy:** start with `block_only`. The workflow fails (and the merge is blocked) only on BLOCK outcomes. Warnings appear in the artifact but don't block. Upgrade to `warn_and_block` once warning noise is under control.

## Extending

- **New Relic / deploy health**: add a step that writes `prod_health.json`, pass `--prod-health`.
- **Coverage**: emit `coverage.json` with `line_percent` and `baseline_percent` (from main branch artifact or stored baseline).
- **New E2E test file**: add its stem(s) to `VALIDATION_FILE_STEMS` in `scripts/e2e_to_readiness.py`.

## Sample fixtures

| Directory | Purpose |
|-----------|---------|
| `fixtures/sample_pass/` | Happy-path smoke + E2E + coverage + health |
| `fixtures/sample_block_smoke/` | Failing smoke (expect BLOCK when combined with script) |
| `fixtures/sample_e2e_playwright/` | Minimal Playwright JSON reporter output for converter testing |

Run with `--fixture-mode` (loads `sample_pass`).
