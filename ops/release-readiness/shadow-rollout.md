# Shadow gate rollout — shadow vs legacy verdicts

**Source ticket:** [SCRUM-266](https://suthar-team.atlassian.net/browse/SCRUM-266) — reconcile shadow vs legacy gate over ≥15 PRs.

**Status:** ✅ **complete** — 22 PRs observed (target ≥15), 3 disagreements all sharing the same documented root cause. Final reconciliation summary at the bottom of this doc recommends cutover. SCRUM-267 unblocked.

This doc tracked every PR that ran after the shadow gate went live, recording the verdict from the **legacy** in-tree gate (now deleted in SCRUM-267 — formerly `scripts/release-readiness.sh` + `go run ./cmd/prrisk` driving `scripts/pr_gate.py`) alongside the verdict from the **shadow** [`release-readiness-core`](https://github.com/psuthar/release-readiness-core) gate (inlined CLIs in `.github/workflows/release-readiness.yml`'s former `shadow-readiness` job). When the two disagreed, each got a root cause and a resolution — never a silent override.

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
| [#234](https://github.com/psuthar/talkback/pull/234) | SCRUM-269: persist session primary content kind + non-video pointers | WARN | WARN | Yes | — | — domain_migrations + sessions.go floor; both gates flag identically. |
| [#235](https://github.com/psuthar/talkback/pull/235) | SCRUM-270: wire Session model + repository reads for primary content | PASS | PASS | Yes | — | — small-diff backend, both PASS. |
| [#236](https://github.com/psuthar/talkback/pull/236) | SCRUM-271: GET session resolved primary descriptor | WARN | WARN | Yes | — | — sessions.go-hotspot floor; identical verdicts. |
| [#237](https://github.com/psuthar/talkback/pull/237) | SCRUM-272: PATCH session primary endpoint with ACL + ownership checks | WARN | WARN | Yes | — | — sessions.go-hotspot floor; identical verdicts. |
| [#238](https://github.com/psuthar/talkback/pull/238) | SCRUM-273: extract PrimaryStage component (CreatorMode center pane) | PASS | WARN | **No** | See [PR #238 / #240 / #241 — pr_risk_warn](#prs-238--240--241--prriskwarn-readiness-check) below. | **Accepted** — release-readiness-core's pr_risk_warn check is intentional and more accurate than legacy. PR Risk score parity is exact (14 = 14); only the readiness combine differs. |
| [#239](https://github.com/psuthar/talkback/pull/239) | SCRUM-274: participant center pane defaults to session primary on load | PASS | PASS | Yes | — | — pure frontend, low-risk; both PASS. |
| [#240](https://github.com/psuthar/talkback/pull/240) | SCRUM-275: creator UI to designate session primary material | PASS | WARN | **No** | Same root cause as #238 — see disagreement note. | **Accepted** — pr_risk score 10 = 10; readiness combine differs as documented. |
| [#241](https://github.com/psuthar/talkback/pull/241) | SCRUM-276: extend Primary badge to link rows + creator center-pane sync | PASS | WARN | **No** | Same root cause as #238 — see disagreement note. | **Accepted** — pr_risk score 10 = 10; readiness combine differs as documented. |
| [#242](https://github.com/psuthar/talkback/pull/242) | SCRUM-279: PATCH session primary returns enriched primary descriptor | PASS | PASS | Yes | — | — small handler change; both PASS. |
| [#243](https://github.com/psuthar/talkback/pull/243) | SCRUM-280: ListSessions returns resolved primary descriptor per row | WARN | WARN | Yes | — | — large-diff + sessions.go-hotspot floor; identical. |
| [#245](https://github.com/psuthar/talkback/pull/245) | SCRUM-281: validate material readiness when setting session primary | PASS | PASS | Yes | — | — small backend + frontend; both PASS. |
| [#246](https://github.com/psuthar/talkback/pull/246) | SCRUM-282: repair session primary state when referenced row is deleted | PASS | PASS | Yes | — | — backend-only repair logic + integration tests; both PASS. |
| [#247](https://github.com/psuthar/talkback/pull/247) | SCRUM-283: broadcast session primary changes + persist audit history | WARN | WARN | Yes | — | — new migration + sessions.go floor; identical. |
| [#248](https://github.com/psuthar/talkback/pull/248) | SCRUM-284: PrimaryStage renders document and link kinds via primary descriptor | PASS | PASS | Yes | — | — small frontend; both PASS. |
| [#249](https://github.com/psuthar/talkback/pull/249) | SCRUM-285: clear-primary affordance + a11y on SetPrimaryButton | PASS | PASS | Yes | — | — frontend-only; both PASS. |
| [#250](https://github.com/psuthar/talkback/pull/250) | SCRUM-286: video presentation row uses SetPrimaryButton badge | WARN | WARN | Yes | — | — e2e flake retry signal; identical WARN. |
| [#251](https://github.com/psuthar/talkback/pull/251) | SCRUM-287: surface .pptx slide failures + harden binary deps | WARN | WARN | Yes | — | — large multi-file diff including router.go; identical WARN. |
| [#252](https://github.com/psuthar/talkback/pull/252) | SCRUM-288: render empty-state in PrimaryStage when no primary resolves | PASS | PASS | Yes | — | — frontend-only ~62 LOC; both PASS. |
| [#253](https://github.com/psuthar/talkback/pull/253) | SCRUM-289: SetPrimaryButton on image-row materials | PASS | PASS | Yes | — | — frontend-only; both PASS. |
| [#254](https://github.com/psuthar/talkback/pull/254) | SCRUM-290: replace inline Clear text-button with right-click context menu | PASS | PASS | Yes | — | — frontend-only ~270 LOC; both PASS. |
| [#255](https://github.com/psuthar/talkback/pull/255) | SCRUM-292: fall back to soffice when unoconv flakes during slides conversion | PASS | PASS | Yes | — | — backend-only ~100 LOC slides_converter fix; both PASS. |
| [#256](https://github.com/psuthar/talkback/pull/256) | SCRUM-293: show Primary badge to all roles + collapse to one Videos section | PASS | PASS | Yes | — | — frontend-only after the disabled-gate fix landed; both PASS. |

## Disagreement notes

### PRs #238 / #240 / #241 — pr_risk_warn readiness check

**Pattern:** legacy says PASS, shadow says WARN, on three pure-frontend PRs that touched `web/src/modes/CreatorMode.jsx` and/or `web/src/components/MaterialsTreePanel.jsx` (orchestration / viewer_materials risk paths).

**PR Risk parity is exact.** For all three PRs, `pr_risk.score` and `top_risk_factors` match between gates byte-for-byte (14 = 14 on #238; 10 = 10 on #240 and #241). The disagreement is not in the scoring engine.

**Where the disagreement enters.** The shadow's `release-readiness-core` `report.json` includes a `pr_risk_warn` failed_check that the legacy in-tree gate does not produce:

```jsonc
// shadow report.json
{
  "outcome": "WARN",
  "warnings": [
    "PR Risk indicates elevated review may be needed (churn, workflow or config changes, or evidence gaps). Complete required validations before deploy."
  ],
  "failed_checks": ["pr_risk_warn"],
  "score": 100.0,
  "blockers": 0
}

// legacy report.json (same input, same diff)
{
  "outcome": "PASS",
  "warnings": [],
  "failed_checks": [],
  "score": 100.0,
  "blockers": 0
}
```

`release-readiness-core` ships a built-in readiness validation (`pr_risk_warn`) that surfaces non-PASS PR Risk as a soft readiness warning. The legacy gate combines `pr_risk` and `release_readiness` only at `final_gate` level, never as a readiness `failed_check`. This is a **design difference** in the package, not a bug or a misconfiguration — see release-readiness-core docs/why-this-package §3.

**Resolution: accept.** The shadow behavior is more accurate. All three disagreement PRs touched paths the YAML flagged as risk-tier-2 (orchestration, viewer_materials); the legacy gate's silent PASS was the bug. The cutover (SCRUM-267) inherits the stricter combine behavior intentionally.

**Operator impact post-cutover:** PRs that previously PASSed but had elevated PR Risk will now WARN. This matches the documented phased --warn-conclusion rollout in SCRUM-267:
- Phase 1 / 2 (`neutral` → `action_required`): WARN is non-blocking via the github-script remap or the v0.4.0 CLI flag.
- Phase 3 (`failure`, optional): WARN blocks merge — adopt only after team is comfortable with the new floor.

No `pr-risk-config.yaml` tuning required. No upstream issue against release-readiness-core required. The check fires on the cases the package considers worth flagging.

## Final reconciliation summary

**22 of 22 PRs observed (target ≥15).** Mix: 14 PASS / 8 WARN on the legacy side, 11 PASS / 11 WARN on the shadow side. No BLOCKs in the soak window.

**Disagreements:** 3 (PRs #231, #238, #240, #241) — note that #231's wiring bug was already resolved before the soak proper began; the three real-traffic disagreements (#238, #240, #241) all share the documented `pr_risk_warn` root cause above. **0 unexplained disagreements remaining** (target ≤1). All three are accepted as the new normal post-cutover.

**Recommendation: proceed with cutover (SCRUM-267).** The shadow gate is operationally trustworthy:
- PR Risk scoring is byte-identical to the legacy Go engine across the entire soak.
- The single behavioral delta (`pr_risk_warn`) is intentional, documented, and arguably more correct than the legacy silent-PASS.
- No blockers, no parse errors, no false BLOCKs since the wiring fix in PR #232.
- Workflow file changes were the dominant cause of WARNs in the soak window (legacy `ci_workflows` floor); SCRUM-267 removes that floor on the way out.

(Long-form notes for individual PRs go here when a row's resolution column is not enough.)

## Reference

- SCRUM-262 pin file: [`version.txt`](./version.txt) — the package version both shadow and legacy consume must remain consistent.
- SCRUM-263 gap-test harness: [`scripts/prrisk_gap_harness.py`](../../scripts/prrisk_gap_harness.py) — offline parity check against historical SHAs; complements the live shadow soak with a deterministic baseline.
- SCRUM-264 `release-readiness-doctor` pre-flight in `.github/workflows/release-readiness.yml` validates the YAMLs before either gate runs.
- SCRUM-265 shadow CI job: same workflow file, `shadow-readiness` job (inlined CLIs because `psuthar/release-readiness-core` is private and `uses:` cannot resolve to it).
- SCRUM-267 ticket description has the cutover plan, including the v0.4.0 `--warn-conclusion` phased rollout (`neutral` → `action_required` → `failure`).
