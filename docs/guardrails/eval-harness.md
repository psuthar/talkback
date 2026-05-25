# qa-eval CI gate (Slice 1 / SCRUM-562)

The per-PR baseline-compare gate for the GenAI Guardrails epic
([SCRUM-560](https://suthar-team.atlassian.net/browse/SCRUM-560)) and
the contract under which downstream slices (SCRUM-563 / 564 / 565 /
566 / 567) add their own metrics.

## What the gate does on every PR

`.github/workflows/release-readiness.yml` runs
`scripts/qa_eval_ci.py` after the base ref has been fetched. The
script:

1. Loads `eval/baselines/qa_eval_baseline.json` from the **working
   tree** (i.e. as it exists in this PR).
2. Loads `eval/baselines/qa_eval_baseline.json` from the **base ref**
   (`origin/<base>`) via `git show`. If the file doesn't exist on the
   base ref — e.g. this PR is the one introducing the file, or a
   branch off a tag — the comparison is recorded as skipped and the
   gate passes.
3. Runs a CLI-surface sanity check (`python3 scripts/run_qa_eval.py
   --help`) so accidental upstream refactors of the harness fail loudly
   here rather than silently at the next refresh.
4. Applies `eval/baselines/qa_eval_thresholds.yaml` to the deltas.
5. Emits `artifacts/qa-eval-summary.json` (carried on the
   release-readiness artifact bundle).
6. Exits non-zero on threshold breach → the CI step fails → the
   release-readiness gate flips to **WARN** for the PR.

It is **not** a live measurement. The harness is not invoked against a
running TalkBack on PRs. That was considered for Slice 1 and rejected
as too heavy + flake-prone for a foundational gate (see SCRUM-562
description for the planning trail).

## Metric set today (Slice 1)

| Metric | Source | Threshold default |
|--------|--------|-------------------|
| `correctness_percentage` | `scripts/run_qa_eval.py` aggregate (existing) | drop > 2pp → WARN |
| `hallucination_count` | same | any increase → WARN |
| `weighted_correctness` | same | drop > 0.02 → WARN |
| `p95_latency_ms` | `aggregate_report` (added by SCRUM-562 — computed inline from `CaseResult.duration_ms`) | rise > 25% → WARN |
| `overall_pass` | same | must remain `true` |

Skip semantics: a missing metric on either side (null in the baseline
or null in the current run) is recorded as `skipped` and counts as a
pass. This is the path for pre-SCRUM-562 baselines that didn't
populate `p95_latency_ms`; the next refresh closes the gap.

## Metric set NOT in Slice 1 (added by downstream slices, per SCRUM-569)

| Metric | Delivered by | Notes |
|--------|--------------|-------|
| `refusal_when_oos_rate` | [SCRUM-564 (Slice 3)](https://suthar-team.atlassian.net/browse/SCRUM-564) | Needs both refusals (introduced by Slice 3 enforce mode) and the legitimate-vs-OOS labeled fixture set ([SCRUM-570 — Slice 1b](https://suthar-team.atlassian.net/browse/SCRUM-570)) to exist before it can be computed. |
| `citation_rate` | [SCRUM-565 (Slice 4a)](https://suthar-team.atlassian.net/browse/SCRUM-565) | Fraction of answered cases that cited at least one retrieved chunk. |
| `groundedness_rate` | [SCRUM-566 (Slice 4b)](https://suthar-team.atlassian.net/browse/SCRUM-566) | Computed via the same LLM-as-judge call the runtime guardrail uses. |

Each downstream slice extends the runner's `aggregate_report` to emit
its new metric, then extends `qa_eval_thresholds.yaml` to gate on it.
This file (Slice 1) does not contain any of those three metrics.

## Local invocation

One command (run from repo root):

```sh
python3 scripts/qa_eval_ci.py
```

Defaults: baseline at `eval/baselines/qa_eval_baseline.json`,
thresholds at `eval/baselines/qa_eval_thresholds.yaml`, base ref
`origin/main` (override via `QA_EVAL_BASE_REF`), output to
`artifacts/qa-eval-summary.json`. Exit code 0 = pass, 1 = threshold
breach, 2 = bad input (missing/malformed file).

The unit tests live alongside in `scripts/test_qa_eval_ci.py`:

```sh
python3 scripts/test_qa_eval_ci.py
```

## Refresh procedure (when to bump the baseline)

The baseline only changes when a legitimate quality / latency drift is
observed via the live harness — judge-model swap, scorer rewrite,
RAG-pipeline change, case-set turnover. The `qa-eval-refresh` workflow
(`.github/workflows/qa-eval-refresh.yml`, **`workflow_dispatch` only**
for now) is the documented trigger:

1. Actions tab → `qa-eval-refresh` → **Run workflow**. The workflow
   sanity-checks the harness CLI and prints the human-runnable
   procedure (the live CI wiring is deferred to a follow-up — running
   the harness in CI against a docker-compose'd TalkBack + secrets is
   meaningful infra not yet justified at Slice 1 scope).

2. From a workstation with a reachable TalkBack + `OPENAI_API_KEY`:

   ```sh
   export QA_EVAL_COOKIE='tb_login=<uuid>'
   export OPENAI_API_KEY='sk-...'
   python3 scripts/run_qa_eval.py \
     --auto-setup \
     --run-id refresh-$(date +%Y%m%d-%H%M%S)
   ```

3. Inspect `eval/qa/runs/<run-id>/report.json`. Sanity-check the case
   set still matches the inventory; confirm correctness, hallucination,
   and latency look right.

4. Copy the relevant `metrics` fields into
   `eval/baselines/qa_eval_baseline.json` and bump
   `source_recorded_at` + `source_commit`.

5. Open a PR with the baseline bump. The per-PR `qa_eval_ci.py` gate
   validates that the bump is non-regression vs. the prior baseline
   using `eval/baselines/qa_eval_thresholds.yaml`. A breach makes the
   PR Gate flip to **WARN** — investigate before merging.

## Thresholds

`eval/baselines/qa_eval_thresholds.yaml` is the single source. Tighten
deliberately, with a one-line PR-description rationale per change.
Slice 6 (post-2-weeks-telemetry follow-up, out of this epic) is the
natural home for evidence-based tuning.

## Cross-references

- [Inventory](inventory.md) — every LLM call site this gate eventually protects
- [Threat model](threat-model.md) — mode 4 (refusal-bombing) closure relies on this gate
- [Refusal shape](refusal-shape.md) — what the guardrails return when they fire
- [Log shape](log-shape.md) — what every LLM call records (Slice 5)
- Epic: [SCRUM-560](https://suthar-team.atlassian.net/browse/SCRUM-560)
- This slice: [SCRUM-562](https://suthar-team.atlassian.net/browse/SCRUM-562)
