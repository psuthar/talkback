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

## Authoring phase (SCRUM-493/494)

When `run epic SCRUM-X` finds zero non-Done children (i.e. the Epic has not been decomposed yet), the skill transitions through a pre-running authoring phase before executing any work. The full algorithm lives in `.claude/skills/epic-run/SKILL.md` Start step 3; the contract this doc owns:

- **Single command.** Users invoke `run epic` only — there is no separate `kickoff epic`. The skill detects the empty-Epic case and enters authoring automatically.
- **Approval before creation.** The agent renders a decomposition proposal in chat (parent + children with summary, AC count, split rationale, estimated LOC, current threshold) and waits for user y/n. On **n** → halt with `halt_category: "human_requested_halt"`. On **y** → the skill calls `jira_create_issue` per child (each carrying `agent-authored` per the labels rule of `jira-ticket-authoring`) and runs `scripts/jira_ticket_lint.py` on each new ticket; any non-zero lint exit halts with `halt_category: "spec_missing"`.
- **Children-per-authoring cap: 8.** If `jira-work-decomposition` would propose more than 8 children, halt with `halt_category: "spec_missing"` and `halt_reason` describing the scope mismatch. No tickets are created. The Epic needs human re-scoping (`jira-work-decomposition` Splitting Oversized Work rules say so).
- **`max_estimated_loc` override.** Optional integer in `.epic-run/<EPIC>.json` (range `[100, 800]`, default `400`). Below 100 or above 800 is rejected at the proposal step — out-of-range overrides require direct state-file edit AND a paragraph in the Epic description explaining the deviation (e.g. a security-audit Epic running at 50 LOC per PR). The proposal renders the current threshold as `"400 (default)"` or `"<N> (Epic override)"`.

## Epic-vs-Standalone FULL_AUTO

- Standalone FULL_AUTO may merge on `mergeable_state: clean` + TalkBack PR Gate success.
- Epic mode is stricter: do not merge unless `mergeable_state: clean` and Final Gate is PASS.

## Parallel Marker Convention

Default execution is sequential. A ticket may run in parallel batch only when:

- Jira label `parallel-ok`, or
- Jira description contains `Parallel: yes`

Consecutive parallel-eligible tickets run as a batch and must resolve before moving on.

## Per-ticket flow (default — polling path)

Per SCRUM-392, per-ticket FULL_AUTO inside an epic uses the **polling path** described in [`workflow-full-auto.md`](workflow-full-auto.md). The agent drives merge + Jira Done from its own session; no cloud routine is involved by default. For each child ticket:

1. Run `implement <KEY> FULL_AUTO` per the standard ticket workflow.
2. Push PR + transition Jira to **In Review**.
3. **Poll** TalkBack PR Gate + `mergeable_state` every 30s on a 40-min budget. Merge via `merge_pull_request` when both PASS+clean (with the mandatory pre-merge guard re-read). Post the structured Jira completion comment and transition the child's Jira ticket to **Done**.
4. On WARN/BLOCK: stop polling immediately, post the structured Jira halt comment, leave PR open + Jira In Review, **HALT the epic** (do not start the next child).
5. Move on to the next child only when the previous one is merged + Jira Done. Do not start `feat/<next>` until that's confirmed.

The webhook routine path remains available as an opt-in alternative on a per-epic basis: `run epic SCRUM-XX FULL_AUTO_WEBHOOK` / `continue epic SCRUM-XX FULL_AUTO_WEBHOOK`. Each child ticket inside that epic runs as `implement <KEY> FULL_AUTO_WEBHOOK`, which delegates merge + Jira Done to the deployed routine (consumes quota per [`pr-gate-webhook.md`](pr-gate-webhook.md)). The polling-default is the right choice unless the operator explicitly wants the webhook behavior and has quota budget.

## Halt and Resume

Automation HALTs when:

- The agent's polling resolves to WARN/BLOCK (gate completes non-`success`), or
- The polling budget expires without `mergeable_state` reaching `clean` while the gate is PASS, or
- (Webhook path only) the routine posts a Jira halt comment.

On resume (`continue epic ...`), agent must re-read Jira children (`statusCategory != Done`) as source of truth and reconcile:

- already Done: skip (the prior ticket's close-out completed normally).
- merged but Jira not Done: transition Jira + run local cleanup (rare; usually a partial close-out from a prior interrupted session).
- open PR with halt comment (WARN/BLOCK): halt again at the epic level; do not start the next ticket. Same applies whether the halt comment came from the polling path (agent-posted) or the webhook path (routine-posted).
- open PR with no halt comment and gate still in progress: resume polling.
- not started: run `implement <KEY> FULL_AUTO` (or `FULL_AUTO_WEBHOOK` if the epic was invoked with that suffix) under epic rules.

Git hygiene before next ticket: fetch/checkout/pull `main` so branch starts from current main. (The SCRUM-388 FF rule on the prior ticket's close-out should have already done this for the developer's primary tree, but verify before proceeding.)

Stale state file rule: if `.epic-run/SCRUM-XX.json` exists and not complete, use `continue epic` instead of `run epic`.

