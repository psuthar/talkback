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

## Halt and Resume

Automation HALTs when:

- mergeability/gate polling does not resolve to required pass state in budget, or
- Final Gate is WARN/BLOCK/missing/unreadable.

On resume (`continue epic ...`), agent must re-read Jira children (`statusCategory != Done`) as source of truth and reconcile:

- already Done: skip
- merged but Jira not Done: transition Jira + cleanup
- open PR still WARN/BLOCK: halt again
- open PR now PASS: resume polling and merge
- not started: run `implement <KEY> FULL_AUTO` under epic rules

Git hygiene before next ticket: fetch/checkout/pull `main` so branch starts from current main.

Stale state file rule: if `.epic-run/SCRUM-XX.json` exists and not complete, use `continue epic` instead of `run epic`.

