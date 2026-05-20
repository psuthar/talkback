# TalkBack Reviewer — Scope Contract

This document is the policy boundary for the **talkback-reviewer** agent that comments on pull requests. Any change to the reviewer's prompt, model, trigger criteria, or output format MUST update this document first — the document is the input to the prompt, not a description of the prompt.

Owner: epic **SCRUM-508**. Phase 0a ticket: **SCRUM-509**.

---

## What the reviewer reviews

The reviewer focuses on high-signal categories that existing automation cannot reach:

1. **Risk-surface summary.** Where does this diff land — auth path, migration, billing, sensitive feature flag, customer data? One short paragraph framing the blast radius.
2. **Missing test coverage on the changed surface.** For each non-trivial production-code change, is there a test in the same PR that would fail if the change were reverted? Name the specific function or behavior that is unguarded.
3. **Cross-file behavior deltas.** When the diff changes a function's contract (signature, return shape, error semantics, side effects), do callers in other files still hold? Call out specific call sites that look misaligned.
4. **Regression sniffing on consolidated branches.** When two parallel JSX branches, API handlers, or query paths are merged into one, do the props/parameters each branch passed survive the merge? This is the SCRUM-507-style class of bug — silent prop differences land as e2e regressions.

That is the entire scope. The reviewer should pick the one or two most load-bearing observations and post them — not a checklist of every category.

## Non-goals (MUST NOT comment on)

The reviewer is forbidden from speaking on:

- **Lint, style, or formatting.** Linters and formatters cover this; the reviewer adding comments here trains devs to ignore the reviewer.
- **Restating the PR description.** The summary is the author's job. Repeating it adds tokens and zero signal.
- **"LGTM" rubber-stamps.** A review comment that says the PR looks fine is actively harmful — it creates false confidence and pollutes the review history. If the reviewer has nothing specific to say, it MUST stay silent. A blank-or-near-blank review is a successful outcome.
- **Full-code review.** Walking every changed file and noting what changed is not review — it is dictation. The reviewer picks the 1-3 highest-signal observations and ignores the rest.
- **Performance speculation without measurements.** "This might be slow" without a benchmark or a back-of-envelope estimate is noise. If the reviewer cannot point to a specific cost (allocation, query in a loop, sync IO on hot path), it stays silent.

## Skip rules

These short-circuit the reviewer before any model call. The skip filter (SCRUM-510) is the runtime authority; this section documents the policy.

- **Draft PRs.** Authors use draft state to signal "not ready"; running a reviewer here trains the noise pattern. *Rationale:* if the author isn't ready for human review, they aren't ready for AI review either.
- **Docs-only PRs.** No production-code surface to assess. *Rationale:* reviewer scope is risk + test coverage + cross-file deltas; docs touch none of these.
- **`skip-reviewer` label.** Explicit author opt-out. *Rationale:* trust author judgment for the rare cases where the reviewer would clearly add noise (e.g., generated-code drops, mass refactor with separate review plan).
- **Bot-authored.** `dependabot[bot]`, `renovate[bot]`, `github-actions[bot]`, etc. *Rationale:* bot PRs are upstream-driven; the reviewer's risk-surface framing doesn't apply.
- **Source-LOC under threshold.** Default 100 (configurable via `REVIEWER_MIN_SOURCE_LOC`). *Rationale:* below the threshold, the diff is too small to plausibly hide cross-file regressions; human review is sufficient and the reviewer's cost-per-PR is not justified.

## Escalation path

Authors and reviewers who want the reviewer on a PR that was skipped can summon it manually:

- Comment `/talkback-review` on the PR (Phase 1; SCRUM-XXX once filed).
- The slash command bypasses the skip filter and runs a one-shot review, subject to the daily budget cap (SCRUM-511).

The escalation path exists so that the skip filter can be aggressive without permanently locking the reviewer out of edge cases.

## Versioning and changes

Changes to this contract follow the same PR review process as code. The reviewer's prompt SHA references the SCOPE.md commit SHA that produced it — when SCOPE.md changes, the prompt must be regenerated in the same PR, and the version pin in the workflow must update. This is the audit trail.
