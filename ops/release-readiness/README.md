# TalkBack release readiness (v1)

Deterministic, evidence-based checks before deploy. **Scoring and PASS/WARN/BLOCK are computed only by `scripts/release_readiness.py`** — no LLM in the decision path. An LLM may summarize `report.json` afterward only.

## What it does

1. **Evidence collection** — reads optional JSON artifacts (smoke, E2E, coverage, prod health) and `git diff` vs a base ref.
2. **Scoring** — applies YAML rules (`config.yaml`): blockers (smoke fail, critical E2E, migrations without validation, risky paths without validation evidence), warnings (missing artifacts, E2E retries, coverage drop, risky config paths).
3. **Outputs** — `artifacts/release-readiness/report.json` and `report.md`.

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
```

Exit code: `0` for PASS/WARN, `1` for BLOCK.

### Environment variables

| Variable | Meaning |
|----------|---------|
| `RELEASE_READINESS_BASE_REF` | Default `origin/main` for `--base-ref` |

## CI

GitHub Actions workflow `.github/workflows/release-readiness.yml` runs on `pull_request` and `workflow_dispatch`, runs `go test`, writes `smoke_results.json`, runs the readiness script, and **always uploads** `artifacts/release-readiness/` even when the outcome is WARN or BLOCK.

## Extending

- **New Relic / deploy health**: add a step that writes `prod_health.json`, pass `--prod-health`.
- **Coverage**: emit `coverage.json` with `line_percent` and `baseline_percent` (from main branch artifact or stored baseline).
- **Stricter E2E**: run Playwright with JSON reporter and pass `--e2e-results` path.

## Sample fixtures

| Directory | Purpose |
|-----------|---------|
| `fixtures/sample_pass/` | Happy-path smoke + E2E + coverage + health |
| `fixtures/sample_block_smoke/` | Failing smoke (expect BLOCK when combined with script) |

Run with `--fixture-mode` (loads `sample_pass`).
