# Shadow gate rollout — shadow vs legacy verdicts

**Source ticket:** [SCRUM-266](https://suthar-team.atlassian.net/browse/SCRUM-266) — reconcile shadow vs legacy gate over ≥15 PRs.

**Status:** in progress; soak active since `SCRUM-265` merged at commit `0c89b72` (2026-05-02).

This doc tracks every PR that runs after the shadow gate went live. For each PR we record the verdict from the **legacy** in-tree gate (`scripts/release-readiness.sh` + `go run ./cmd/prrisk` driving `scripts/pr_gate.py`) alongside the verdict from the **shadow** [`release-readiness-core`](https://github.com/psuthar/release-readiness-core) gate (inlined CLIs in `.github/workflows/release-readiness.yml` `shadow-readiness` job). When the two disagree, the disagreement gets a root cause and a resolution — never a silent override.

## Stop condition (cutover gate for SCRUM-267)

- ≥ 15 PRs observed with both gates running.
- Mix of `PASS` / `WARN` / `BLOCK` represented across the sample.
- Every disagreement listed below has a root cause and a resolution.
- ≤ 1 unexplained disagreement remaining.
- Final reconciliation summary written, recommending cutover or further tuning.

Until all five hold, the shadow check stays non-required and SCRUM-267 cannot start.

## How to add an entry

When a PR runs through CI, capture both gates' final verdict:

- **Legacy** comes from the `release-readiness` job's `TalkBack PR Gate` check (artifact: `artifacts/pr-gate-summary.json` → `final_gate.status`).
- **Shadow** comes from the `shadow-readiness` job's `TalkBack PR Gate (shadow)` check (artifact: `artifacts/release-readiness-shadow/pr-gate-summary.json` → `final_gate.status`).

Add a new row to the table below. If the verdicts disagree, populate the **Root cause** and **Resolution** columns; otherwise leave them as `—`. For long resolutions, link to a follow-up section under [Disagreement notes](#disagreement-notes) keyed on the PR number.

| PR | Title | Legacy verdict | Shadow verdict | Agree? | Root cause | Resolution |
|----|-------|----------------|----------------|--------|------------|------------|
| [#231](https://github.com/psuthar/talkback/pull/231) | chore: bump release-readiness-core pin to 0.4.0 | WARN (medium, 20.0) | BLOCK (parse error) | No | Shadow wiring bug — combine pointed at `artifacts/release-readiness-shadow/release-readiness.json` but `release-readiness-evaluate` hardcodes the lean summary at `<repo-root>/artifacts/release-readiness.json` (see `release_readiness_core/readiness_evaluate.py::write_machine_readiness_summary`). The `--output-dir` flag controls full-report location only. | Snapshot the lean file into the shadow output dir before combine runs. Fixed in [#232](https://github.com/psuthar/talkback/pull/232) (`.github/workflows/release-readiness.yml` `Run release-readiness-evaluate (shadow)` step now `cp`s `artifacts/release-readiness.json` to `artifacts/release-readiness-shadow/`). After this fix, both gates compute the same `score=20 medium WARN` — the underlying gate verdicts already matched (cf. SCRUM-264 harness, 17/17 same band ±5). |
| [#232](https://github.com/psuthar/talkback/pull/232) | SCRUM-266: seed shadow-rollout tracking doc + fix shadow combine wiring | WARN (medium, 20.0) | WARN (medium, 20.0) | Yes | — | — — first agreement after PR #231's wiring fix. Confirms `release-readiness-pr-risk` produces identical scoring to the in-tree Go engine on a workflow-touching PR. The `ci_workflows` factor is the sole driver in both. |

## Disagreement notes

(Long-form notes for individual PRs go here when a row's resolution column is not enough.)

## Reference

- SCRUM-262 pin file: [`version.txt`](./version.txt) — the package version both shadow and legacy consume must remain consistent.
- SCRUM-263 gap-test harness: [`scripts/prrisk_gap_harness.py`](../../scripts/prrisk_gap_harness.py) — offline parity check against historical SHAs; complements the live shadow soak with a deterministic baseline.
- SCRUM-264 `release-readiness-doctor` pre-flight in `.github/workflows/release-readiness.yml` validates the YAMLs before either gate runs.
- SCRUM-265 shadow CI job: same workflow file, `shadow-readiness` job (inlined CLIs because `psuthar/release-readiness-core` is private and `uses:` cannot resolve to it).
- SCRUM-267 ticket description has the cutover plan, including the v0.4.0 `--warn-conclusion` phased rollout (`neutral` → `action_required` → `failure`).
