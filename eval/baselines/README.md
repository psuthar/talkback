# `eval/baselines/` — qa-eval CI baseline

This directory holds the **recorded baseline** that
`scripts/qa_eval_ci.py` consumes on every PR (Slice 1 of the GenAI
Guardrails epic — SCRUM-562 under SCRUM-560).

## Files

| Path | Owner | Refresh trigger |
|------|-------|-----------------|
| `qa_eval_baseline.json` | qa-eval-refresh workflow (manual `workflow_dispatch`) — a human inspects the live run's `report.json` and PRs a bump. | Confirmed quality drift on the live harness, judge-model swap, scorer change, or RAG-pipeline change. |
| `qa_eval_thresholds.yaml` | Slice 1 author / future tuners | After 2+ weeks of telemetry (Slice 6 follow-up scope), or when a deliberate quality-bar tightening is shipped. |

## Why this is *not* a live per-PR measurement

The TalkBack qa-eval harness (`scripts/run_qa_eval.py`) hits a live
TalkBack API + OpenAI judge. Running that in CI for every PR was
considered for Slice 1 and rejected as too heavy + flake-prone (see
SCRUM-562 description for the planning trail).

Instead, the per-PR gate compares the **baseline JSON in the working
tree** against the **baseline JSON on the PR's base ref**:

- PR that doesn't touch `qa_eval_baseline.json` → all deltas are 0 →
  gate passes silently.
- PR that bumps the baseline (typically after the refresh workflow
  surfaces a real drift) → gate validates that the bump is
  non-regression against the prior baseline, per
  `qa_eval_thresholds.yaml`. Any breach makes the CI step exit
  non-zero → release-readiness flips to WARN.

This catches **accidental baseline degradation** (someone runs the
refresh after a bad RAG change and PRs the worse numbers) without
paying per-PR OpenAI cost or fighting CI flake.

## Refresh workflow

1. Trigger `.github/workflows/qa-eval-refresh.yml` manually (Actions
   tab → `qa-eval-refresh` → Run workflow). It runs
   `scripts/run_qa_eval.py --auto-setup` against the live harness and
   uploads the new `report.json` as an artifact.
2. Download the artifact, copy the relevant `metrics` fields into
   `qa_eval_baseline.json`, bump `source_recorded_at` and
   `source_commit`.
3. Open a PR with just the baseline bump. The per-PR gate will
   compare the bump against the prior baseline using the thresholds
   here. If it WARNs, that's the gate doing its job — investigate
   before merging.
4. Merge when green.

Cron-on-a-schedule + auto-baseline-PR is a deliberate follow-up once
the manual refresh path is proven (Slice 1's scope keeps it
manual-only — the live infra wiring is the riskier piece).
