# TalkBack Reviewer — Calibration Rubric

Owner: SCRUM-516 (Phase 1d of Epic SCRUM-512).

Phase 1 of the reviewer agent is a **4-week calibration period** during which engineers invoke `/talkback-review` manually on real PRs. Reviews are scored against this rubric. The aggregate score decides whether Phase 2 (auto-trigger on every PR) is safe to start.

## Why we calibrate before scaling

Auto-triggering an uncalibrated reviewer is the **unrecoverable failure mode** for this Epic. If engineers learn the reviewer is noisy *before* trust is established, they stop reading its comments — and we cannot recover that trust by tweaking the prompt later. Calibration is the gate that catches noise before it scales.

## Scoring categories

For each invocation, the maintainer records exactly one bucket:

| Bucket | Definition | Example |
|---|---|---|
| **useful** | The reviewer found something the author or human reviewers would have missed. | "Flagged that the auth path's new branch doesn't have a test; the author had forgotten." |
| **harmless** | The reviewer said something correct but obvious. Doesn't move the needle. | "Pointed at the new database column and noted it touches the migration path. True but the PR title says exactly that." |
| **noisy** | The reviewer said something irrelevant, redundant, or off-policy. The comment should not have been posted. | "Suggested adding a comment to the function. SCOPE.md non-goals explicitly forbid style-level feedback." |
| **harmful** | The reviewer said something wrong that would cause a regression if acted on. | "Flagged a non-bug as a bug; suggesting the 'fix' would break the existing behavior." |

A single PR may produce one review (one row). If the maintainer re-invokes (e.g. after a force-push), each invocation gets its own row.

## Phase 2 gate thresholds

Aggregated over the calibration corpus (target: 20-30 scored reviews):

| Result | Action |
|---|---|
| **>70% useful** AND **0 harmful** | Phase 2 can proceed. Open the Phase 2 Epic. |
| **50-70% useful** AND **0 harmful** | Revise. The SCOPE.md non-goals likely need sharpening, or the prompt is leaking outside the boundary. File a SCOPE.md or PROMPT.md revision; recalibrate with the next 10-15 invocations. |
| **<50% useful** | The reviewer is the wrong tool for the surface it's being aimed at. Revisit the program — maybe the scope should be narrower (e.g., only sensitive-paths), or the model is wrong, or AI review is not the right shape for this team. Do not proceed to Phase 2 on hope. |
| **Any harmful occurrence** | Halt immediately. Review the offending invocation against SCOPE.md; it is a policy violation by definition (the prompt is bounded by SCOPE.md and the model produced a finding outside that boundary). File a SCOPE.md or PROMPT.md fix in the same week. |

`noisy` and `harmless` are not gating individually, but together they paint the silence-bias picture: if `noisy > harmless`, the prompt should favor silence more aggressively.

## Where the data lives

- **Raw log:** `ops/define-kpis/reviewer-calibration.csv` — one row per invocation, filled in by the maintainer immediately after reading each review.
- **Aggregator:** `scripts/reviewer/calibration_summary.py` — reads the CSV, prints bucket percentages and total token cost. Run weekly.

CSV columns (header row already in the file):

| Column | Meaning |
|---|---|
| `pr` | PR number |
| `date` | ISO date of the invocation |
| `bucket` | One of `useful`, `harmless`, `noisy`, `harmful` |
| `false_positives` | Count of bullet points within the review comment that were wrong (only meaningful for `noisy` / `harmful`) |
| `tokens_used` | From the budget audit log; cross-check `ops/define-kpis/reviewer-budget.log` |
| `response_seconds` | Wall-clock from `/talkback-review` comment to the reviewer's finding comment |
| `notes` | Free-form short note for the gate review |

## Cadence

- **Per-invocation:** maintainer fills in the row within 24h while context is fresh.
- **Weekly:** run `python3 scripts/reviewer/calibration_summary.py` and post the aggregate on the SCRUM-512 epic as a Jira comment.
- **End of week 4:** apply the Phase 2 gate thresholds above; record the decision (proceed / revise / reframe) on SCRUM-512.

## What this is NOT

- Not a dashboard. CSV + a printf script is enough for ~30 rows. A real dashboard belongs to Phase 4 KPI work, not here.
- Not automation. The maintainer reads each review and assigns a bucket; that human judgment is the whole point.
- Not a substitute for SCOPE.md. The rubric measures whether SCOPE.md is the right contract — it does not encode the contract.
