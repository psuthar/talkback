# Epic Run Workflow

Source of truth: This file owns epic automation contract, strict gate rules, and halt/resume behavior.

## Commands

Use `epic-run` skill (`.claude/skills/epic-run/SKILL.md`):

- `run epic SCRUM-XX`
- `continue epic SCRUM-XX`
- `continue epic run for SCRUM-XX`

## Contract

- Goal: every child issue is fully implemented, PR-gated, squash-merged to `main`, and transitioned to Done in order.
- In this repo, deployed means code merged to `main` with gate expectations met for that PR.
- A single `continue epic` should drain all remaining work unless halted by policy.

## Epic-vs-Standalone FULL_AUTO

- Standalone FULL_AUTO may merge on `mergeable_state: clean` + TalkBack PR Gate success.
- Epic mode is stricter: do not merge unless `mergeable_state: clean` and Final Gate is PASS.

## Parallel Marker Convention

Default execution is sequential. A ticket may run in parallel batch only when:

- Jira label `parallel-ok`, or
- Jira description contains `Parallel: yes`

Consecutive parallel-eligible tickets run as a batch and must resolve before moving on.

## Per-ticket flow with the webhook routine (default)

After SCRUM-386, per-ticket FULL_AUTO inside an epic uses the routine-driven path described in [`workflow-full-auto.md`](workflow-full-auto.md) and [`pr-gate-webhook.md`](pr-gate-webhook.md). For each child ticket:

1. Run `implement <KEY> FULL_AUTO` per the standard ticket workflow.
2. Push PR + transition Jira to **In Review**.
3. **Stop active polling.** The routine merges on PASS+clean and transitions the child's Jira ticket to Done, or posts a Jira halt comment on WARN/BLOCK.
4. Do one confirmation read after a brief wait (~5–10 minutes) — same single-check pattern as standalone FULL_AUTO. Then either advance to local cleanup (routine merged) or HALT the epic (routine posted a halt comment).
5. Move on to the next child only when the previous one is merged + Jira Done. Do not start `feat/<next>` until that's confirmed.

The polling-based merge path is the fallback when the routine can't fire — see [`workflow-full-auto.md`](workflow-full-auto.md) "Manual-merge fallback path."

## Halt and Resume

Automation HALTs when:

- The routine posts a Jira halt comment (WARN/BLOCK), or
- The fallback path's mergeability/gate polling does not resolve to PASS in budget, or
- Final Gate is WARN/BLOCK/missing/unreadable on the fallback path.

On resume (`continue epic ...`), agent must re-read Jira children (`statusCategory != Done`) as source of truth and reconcile:

- already Done: skip (routine likely handled it — verify merge SHA from the routine's completion comment).
- merged but Jira not Done: transition Jira + cleanup (rare; usually means routine merged but its Jira transition errored — Atlassian connector issue).
- open PR with routine halt comment (WARN/BLOCK): halt again at the epic level; do not start the next ticket.
- open PR with no routine activity at all: routine isn't firing — fall back to the polling path from `workflow-full-auto.md` and proceed.
- not started: run `implement <KEY> FULL_AUTO` under epic rules.

Git hygiene before next ticket: fetch/checkout/pull `main` so branch starts from current main. (The SCRUM-388 FF rule on the prior ticket's close-out should have already done this for the developer's primary tree, but verify before proceeding.)

Stale state file rule: if `.epic-run/SCRUM-XX.json` exists and not complete, use `continue epic` instead of `run epic`.

