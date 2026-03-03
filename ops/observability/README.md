# Observability Worker (obsworker)

CLI that queries New Relic NerdGraph (NRQL), builds a diagnostic bundle (JSON + Markdown), and makes it easy to paste into an AI co-engineer prompt.

## Agent loop (GitHub Actions)

The workflow **Observability Agent** (`.github/workflows/observability-agent.yml`) runs on **push to main**, **schedule (every 60 min)**, and **workflow_dispatch**:

1. Runs tests, then `go run ./cmd/obsworker`.
2. Uploads `ops/bundles/*-bundle.md` and `*-bundle.json` as artifacts (`talkback-observability-bundle-<run_id>`).
3. Creates or updates a **daily GitHub Issue** titled `TalkBack Observability Bundle - YYYY-MM-DD` (inbox): new runs add a comment with the latest bundle; new day creates a new issue with intro + co-engineer prompt + bundle.

**Required repo secrets:** `NEW_RELIC_API_KEY`, `NEW_RELIC_ACCOUNT_ID`.  
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

## Bundle contents

The bundle includes: **txn_summary** (count, avg/p95/max latency in ms), **discover_appnames** (list of app names in the last 60 minutes), **throughput**, **latency_p95**, **top_transactions_p95**, **error_count** and **top_errors** / **top_error_classes** from `TransactionError`. Markdown uses title-case section names and human-friendly units (e.g. `123.45 ms`, `5.80 req/min`).

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
