# Q&A evaluation harness (`eval/qa`)

Operational entrypoint for fixture-backed Q&A evaluation: inventory JSON, versioned eval cases / score targets, dataset validation, and the SessionAsk runner.

## Layout

| Path | Purpose |
|------|---------|
| `fixture_fact_inventory.json` | Full candidate case list (26+ cases) with source refs and expected keywords/status. |
| `eval_cases_v1.json` | 18-case harness subset with hallucination constraints (aligned with FF-001–FF-018). |
| `expected_scores_v1.json` | Per-case correctness/hallucination targets and weights. |
| `schemas/` | JSON Schema (Draft 2020-12) for `eval_cases` and `expected_scores`. |
| `runs/` | Timestamped runner output (gitignored except `.gitkeep`). |
| `pilot_baseline.json` | Recorded pilot metrics (dry-run + validation); see **Pilot baseline**. |

## Prerequisites

- **Python 3** (3.10+ recommended; CI uses 3.12).
- **`jsonschema`** for dataset validation: install release-readiness deps from repo root:

  ```bash
  pip install -r ops/release-readiness/requirements.txt
  ```

- **Live SessionAsk runs** additionally require:
  - TalkBack API reachable (default `http://localhost:8080`).
  - Authenticated session (**`QA_EVAL_COOKIE`** or **`QA_EVAL_EMAIL`** + **`QA_EVAL_PASSWORD`**).
  - For real answers: working OpenAI (and DB) configuration as used by your environment.

## Environment variables

### Dataset validation (`scripts/qa_eval_datasets.py`)

No env vars required for defaults. Optional CLI flags override paths (see `--help`).

### Runner (`scripts/run_qa_eval.py`)

| Variable | Purpose |
|----------|---------|
| `TALKBACK_API_BASE` or `QA_EVAL_BASE_URL` | API origin (default `http://localhost:8080`). |
| `QA_EVAL_COOKIE` | `Cookie` header value (e.g. `tb_login=<uuid>`). |
| `QA_EVAL_EMAIL` / `QA_EVAL_PASSWORD` | Login via `/api/auth/login` if cookie unset. |
| `QA_EVAL_SESSIONS_JSON` | JSON map `fixture_id` → session UUID when **not** using `--auto-setup`. |

## Commands

### Validate eval datasets (schema + inventory alignment)

```bash
python3 scripts/qa_eval_datasets.py
```

Optional: `--no-inventory-check` for schema-only checks.

### Runner (no network): dry-run

Writes `run_manifest.json` and per-case JSON under `eval/qa/runs/<run_id>/` with `skipped_reason: dry_run`.

```bash
python3 scripts/run_qa_eval.py --dry-run --run-id my-smoke-run
```

### Runner (live): auto-create sessions per fixture

Requires auth env vars. Creates sessions, pastes/patches per internal fixture map, then calls SessionAsk for each inventory case.

```bash
export QA_EVAL_COOKIE='tb_login=...'
python3 scripts/run_qa_eval.py --auto-setup
```

### Runner (live): existing sessions

```bash
export QA_EVAL_COOKIE='tb_login=...'
export QA_EVAL_SESSIONS_JSON='{"smokeDocText_meridian_apac_churn":"<uuid>","qa_history_project_omega":"<uuid>",...}'
python3 scripts/run_qa_eval.py
```

Override inventory path:

```bash
python3 scripts/run_qa_eval.py --inventory eval/qa/fixture_fact_inventory.json --out-dir eval/qa/runs
```

## Artifacts

Each run creates:

- **`eval/qa/runs/<run_id>/run_manifest.json`** — `case_count`, `session_map`, `dry_run`, `base_url`, `cases_index` (pointers to case files).
- **`eval/qa/runs/<run_id>/cases/<case_id>.json`** — request URL (if applicable), HTTP status, `raw_body_text`, `parsed_json`, **`normalized`** (`answer_text`, `citation_count`, etc.).

Interpretation:

- **`dry_run`**: no HTTP; cases contain `skipped_reason: dry_run`.
- **Live success**: `http_status` 200/201 and `parsed_json` with `question` / `answer` objects.
- **Errors**: `error` or non-2xx status on `response`.

## Pilot baseline

`pilot_baseline.json` records one pilot pass: dataset validation exit code **0**, default inventory **26** cases, dry-run manifest **26** cases. It does **not** replace a full live eval with LLM scoring.

Refresh when you change default inventory size, eval JSON, or runner behavior.

## Known limitations

- **Judge / aggregate scoring**: keyword and expected-score JSON are inputs; automated judging and rolled-up reports may live in follow-on tasks—use artifacts for manual or downstream tooling review.
- **Dry-run** validates orchestration and disk output only.
- **Live runs** depend on auth, session limits, indexing latency, and model availability.
- **`eval_cases_v1`** (18 cases) is a subset of **`fixture_fact_inventory`**; the runner still iterates the **inventory** file by default unless you pass a different `--inventory`.

## Tests (CI)

Release-readiness installs Python deps and runs:

- `python3 scripts/qa_eval_datasets.py`
- `python3 scripts/test_qa_eval_datasets.py`
- `python3 scripts/test_qa_eval_harness_smoke.py`

## See also

- `scripts/run_qa_eval.py` — runner CLI and env documentation in the module docstring.
- `scripts/qa_eval_datasets.py` — dataset validation CLI.
