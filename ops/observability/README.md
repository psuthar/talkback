# Observability Worker (obsworker)

CLI that queries New Relic NerdGraph (NRQL), builds a diagnostic bundle (JSON + Markdown), and makes it easy to paste into an AI co-engineer prompt.

## Agent loop (GitHub Actions)

The workflow **Observability Agent** (`.github/workflows/observability-agent.yml`) runs on **push to main**, **schedule (every 60 min)**, and **workflow_dispatch**:

1. Runs tests, then `go run ./cmd/obsworker`.
2. Uploads `ops/bundles/*-bundle.md` and `*-bundle.json` as artifacts (`talkback-observability-bundle-<run_id>`).
3. Creates or updates a **daily GitHub Issue** titled `TalkBack Observability Bundle - YYYY-MM-DD` (inbox): new runs add a comment with the latest bundle; new day creates a new issue with intro + co-engineer prompt + bundle.

**Required repo secrets:** `NEW_RELIC_API_KEY`, `NEW_RELIC_ACCOUNT_ID`.  
**Optional repo secrets (for Key Deltas in CI):** GitHub Actions runners are ephemeral, so the baseline is lost between runs. To get **Key Deltas** (instead of "First run (no baseline)") on the second and later runs, add the same R2 secrets you use for the app: `R2_BUCKET`, `R2_ENDPOINT`, `R2_ACCESS_KEY_ID`, `R2_SECRET_ACCESS_KEY`, and optionally `R2_PREFIX`. The workflow sets `OBS_BASELINE_R2=1` so obsworker stores the baseline in your R2 bucket.  
**Optional repo variables:** `OBS_WINDOW_MINUTES` (default 30), `OBS_APP_NAME` (filters Transaction queries to this app), `NEW_RELIC_REGION` (default US). To generate smoke traffic before obsworker in CI, set `RUN_OBS_SMOKE` to `true` and `TALKBACK_BASE_URL` to your service URL (e.g. Render).

Create labels `observability` and `agent` in the repo so the workflow can tag the issue (optional; workflow still runs if labels are missing).

## Required env vars

| Variable | Description |
|----------|-------------|
| `NEW_RELIC_API_KEY` | NerdGraph / user API key (create in New Relic: Account → API keys) |
| `NEW_RELIC_ACCOUNT_ID` | Your New Relic account ID (integer; from URL or account settings) |

## Optional env vars

| Variable | Default | Description |
|----------|---------|-------------|
| `NEW_RELIC_REGION` | `US` | `US` or `EU` (changes API endpoint) |
| `OBS_WINDOW_MINUTES` | `30` | NRQL time window in minutes |
| `OBS_APP_NAME` | (empty) | When set, Transaction-based NRQL queries add `WHERE appName = '...'` so results are for one app only (no single quotes in name). |
| `OBS_BUNDLES_DIR` | `ops/bundles` | Output directory for bundle files (relative to CWD or absolute) |
| `OBS_BASELINE_PATH` | (derived) | Path to `latest.json` baseline file; default is sibling of bundles dir: `ops/baselines/latest.json`. Set to an absolute path for a fixed location. |
| `OBS_BASELINE_R2` | (unset) | When set to `1` or `true`, load and save the baseline from **R2** (same bucket as app). Use on **Render** or any ephemeral filesystem so the second run sees the first run’s baseline and shows **Key Deltas** instead of “First run (no baseline)”. Requires the same `R2_*` env vars as the API. All observability data in R2 lives under a **dedicated path** so it never mixes with application data: object key `observability/baselines/latest.json` (with `R2_PREFIX` if set, e.g. `talkback/observability/baselines/latest.json`). |

## Bundle contents

The bundle includes: **txn_summary** (count, avg/p95/max latency in ms), **discover_appnames** (list of app names in the last 60 minutes), **throughput**, **latency_p95**, **top_transactions_p95**, **error_count** and **top_errors** / **top_error_classes** from `TransactionError`. Markdown uses title-case section names and human-friendly units (e.g. `123.45 ms`, `5.80 req/min`).

## Observability storage (dedicated path)

All observability data is kept separate from application data:

- **Local / file:** Baseline file lives at `ops/baselines/latest.json` (or `OBS_BASELINE_PATH`). Bundle output goes to `ops/bundles/` (or `OBS_BUNDLES_DIR`). These are under the repo’s `ops/` tree and are not used by the app.
- **R2:** When `OBS_BASELINE_R2=1`, the baseline is stored at object key **`observability/baselines/latest.json`** (with `R2_PREFIX` if set, e.g. `talkback/observability/baselines/latest.json`). Application data uses paths like `sessions/...`, so observability never shares the same path prefix.

## Baseline and Key Deltas

obsworker stores a **baseline** (metrics from the previous run) to compute **Key Deltas** and status (e.g. GREEN/AMBER). The first run has no baseline, so the bundle shows “First run (no baseline)” and “Key Deltas: N/A”. On the second run, if the baseline is still available, the bundle shows deltas (e.g. “P95 +12 ms”) and a more confident status.

- **Local / CI with persistent disk:** The baseline is written to `ops/baselines/latest.json` (or `OBS_BASELINE_PATH`). As long as you run from the same repo and don’t delete that file, the second run will use it.
- **Render or GitHub Actions (ephemeral):** The filesystem is wiped between runs, so the baseline is lost. Set **`OBS_BASELINE_R2=1`** and the same **R2_*** env vars as your app so the baseline is stored in your R2 bucket under the **dedicated path** `observability/baselines/` (no mixing with app data such as `sessions/`). In GitHub Actions, add repo secrets `R2_BUCKET`, `R2_ENDPOINT`, `R2_ACCESS_KEY_ID`, `R2_SECRET_ACCESS_KEY` (and optionally `R2_PREFIX`); the workflow already passes them to obsworker. The next run will load the baseline and show deltas.

## Run (from repo root)

```bash
NEW_RELIC_API_KEY=your_key NEW_RELIC_ACCOUNT_ID=12345678 go run ./cmd/obsworker
```

Or export vars and run:

```bash
export NEW_RELIC_API_KEY=...
export NEW_RELIC_ACCOUNT_ID=...
go run ./cmd/obsworker
```

Output is written to:

- `ops/bundles/<timestamp>-bundle.json`
- `ops/bundles/<timestamp>-bundle.md`

The CLI prints the paths at the end.

## What to do next

**From the daily issue (CI):** Open the issue for today, copy the latest comment (bundle) + the co-engineer prompt from the issue body, paste into Cursor or ChatGPT for analysis.

**Local run:**

1. Open `ops/observability/COENGINEER_PROMPT.md`.
2. Copy the prompt text and the instructions.
3. Paste the contents of `ops/bundles/<timestamp>-bundle.md` into the “Paste the diagnostic bundle” section.
4. Submit to ChatGPT, Cursor, or another AI to get ranked hypotheses and next actions.

## Smoke test (local)

From repo root, with valid `NEW_RELIC_API_KEY` and `NEW_RELIC_ACCOUNT_ID` set:

```bash
go run ./cmd/obsworker
```

Expect two files under `ops/bundles/` and exit code 0.

## Generate representative traffic (obssmoke)

To ensure the observation window includes more than `/health`, run **obssmoke** before obsworker (e.g. locally or in CI with `RUN_OBS_SMOKE=true`):

```bash
TALKBACK_BASE_URL=http://localhost:8080 go run ./cmd/obssmoke
```

- Default URL: `http://localhost:8080`. Hits `/health` five times and optionally `GET /api/sessions` (if 401/403, only `/health` is used).
- Exits non-zero only when all requests fail. Prints a short summary of ok/fail counts.

## Troubleshooting

| Symptom | What to check |
|--------|----------------|
| **No bundle files** | obsworker must run from repo root (or set `OBS_BUNDLES_DIR`). Ensure `ops/bundles/` exists (`.gitkeep` is committed). |
| **Missing env var** | You’ll see a clear message, e.g. `NEW_RELIC_API_KEY is required`. In Actions, add the secret in Settings → Secrets and variables → Actions. |
| **API failure** | Non-200 or GraphQL errors show status and message; check key, account ID, and region. NerdGraph client uses timeouts; avoid huge windows. |
| **Queries file not found** | Run from the repository root so `ops/observability/queries.json` exists. |
| **Workflow fails on “Validate required secrets”** | Add `NEW_RELIC_API_KEY` and `NEW_RELIC_ACCOUNT_ID` as repo secrets. |
| **Empty or sparse NRQL results** | Increase `OBS_WINDOW_MINUTES` (e.g. 30). Ensure the app name in New Relic matches; set `OBS_APP_NAME` if needed. |
| **Always “First run (no baseline)” / “Key Deltas: N/A” on Render** | Render’s filesystem is ephemeral; the baseline file is lost between runs. Set `OBS_BASELINE_R2=1` and the same `R2_BUCKET`, `R2_ENDPOINT`, `R2_ACCESS_KEY_ID`, `R2_SECRET_ACCESS_KEY` (and optionally `R2_PREFIX`) as your app so the baseline is stored in R2. The second run will then load it and show deltas. |
