# Epic-run authoring phase — validation walkthrough

Source: SCRUM-495 (Phase 2 of Epic SCRUM-486, DEFINE-domain uplift).

This document captures the dry-run validation of the authoring phase introduced by SCRUM-493 + SCRUM-494. It walks through every scenario the AC requires (default threshold, Epic override, below-floor rejection, > 8-children halt) using fixture state files and expected proposal output. The matching simulator test (`scripts/test_epic_run_authoring_dryrun.py`) exercises the schema validator against each fixture so regressions land in CI.

**Live-against-real-Epic follow-up:** the AC's "pick a real upcoming Epic with no children" step is the natural next exercise when a fresh Epic surfaces. Re-run this walkthrough against it; if findings diverge from the expected output below, file a ticket to update the heuristic or the doc.

## Pre-flight checklist

Before invoking `run epic SCRUM-X` against an empty Epic, confirm:

- [ ] `scripts/jira_ticket_lint.py` runs cleanly (Phase 1 hard-stop will gate every authored child).
- [ ] `scripts/epic_run_state_schema.py` recognizes the current `.epic-run/<EPIC>.json` shape (`status` enum and `max_estimated_loc` field).
- [ ] The target Epic has zero non-Done children (`parent = SCRUM-X AND statusCategory != Done`).
- [ ] If overriding the LOC threshold, pre-write `.epic-run/SCRUM-X.json` with `"max_estimated_loc": <N>`; the authoring phase reads this on first transition into `"authoring"`.

## Scenario A — default threshold (400), 3-child Epic

**Setup**

```json
{
  "epic": "SCRUM-XYZ",
  "run_id": "<ISO>",
  "status": "authoring",
  "max_estimated_loc": null,
  "tickets": [],
  "next_pending": []
}
```

**Agent action.** `run epic SCRUM-XYZ` → Jira returns zero non-Done children → enter authoring → invoke `jira-work-decomposition` on the Epic description → estimate per-child LOC using the heuristic in that skill.

**Expected proposal output** (rendered in chat, before any `jira_create_issue` call):

```
Current LOC threshold: 400 (default)

Proposed children (3):
  1. SCRUM-A  Backend handler for thing                      ~140 LOC
  2. SCRUM-B  Frontend page for thing                        ~180 LOC
  3. SCRUM-C  DB migration + seed data for thing             ~120 LOC

Approve to create tickets (y/n)?
```

**On y:** state transitions `authoring → awaiting_approval → running`; three `jira_create_issue` calls fire (each carrying `agent-authored`); each new ticket runs through `jira_ticket_lint.py` (exit 0 expected because authoring writes lint-clean descriptions); next_pending is populated; the existing execution loop drains the children.

**On n:** halt with `halt_category: "human_requested_halt"`; no Jira state written.

## Scenario B — Epic override `max_estimated_loc: 100`

**Setup**

```json
{
  "epic": "SCRUM-PQR",
  "run_id": "<ISO>",
  "status": "authoring",
  "max_estimated_loc": 100,
  "tickets": [],
  "next_pending": []
}
```

**Agent action.** Same as A, but the LOC threshold passed to `jira-work-decomposition` is `100`, so the heuristic flags more children as "split candidate".

**Expected proposal output**

```
Current LOC threshold: 100 (Epic override)

Proposed children (3):
  1. SCRUM-D  Backend handler for thing                      ~140 LOC  (split candidate: > threshold)
  2. SCRUM-E  Frontend page for thing                        ~180 LOC  (split candidate: > threshold)
  3. SCRUM-F  DB migration + seed data for thing             ~120 LOC  (split candidate: > threshold)

Three split candidates — review before approving.

Approve to create tickets as-is (y), or halt to split further (n)?
```

**Significance:** at the lower threshold, every child crosses the line. The annotation does not auto-split; the human reviewer decides. Typical action is **n** (halt and refine the decomposition manually) for a 100-threshold Epic — which is exactly the value the lower bound provides: it forces human attention on size.

## Scenario C — below-floor override `max_estimated_loc: 50` → rejected

**Setup**

```json
{
  "epic": "SCRUM-MNO",
  "max_estimated_loc": 50,
  ...
}
```

**Agent action.** `run epic SCRUM-MNO` → `scripts/epic_run_state_schema.py` validates the state file → `validate_max_estimated_loc(50)` returns the out-of-range error → the agent refuses to proceed.

**Expected stdout / Jira halt comment**

```
Halt: state file rejected — max_estimated_loc 50 out of valid range [100, 800]

To use an out-of-range threshold for this Epic, edit .epic-run/SCRUM-MNO.json
directly AND add a paragraph to the Epic description explaining why (e.g.
"This is a security audit Epic — every PR must be reviewable in 50 LOC for
careful inspection. See <link to security review policy>.")
```

State file is left as-written; no `status` transition occurs.

## Scenario D — > 8 children proposed → spec_missing halt

**Setup**

An Epic whose description, when fed through `jira-work-decomposition`, yields 9+ children at the current threshold. E.g. a large refactor Epic at `max_estimated_loc: 100` that the heuristic splits aggressively.

**Agent action.** Decomposition produces 9 candidate children → cap check fires before any `jira_create_issue` call → halt.

**Expected state-file mutation**

```json
{
  "epic": "SCRUM-LARGE",
  "status": "halted",
  "halt_reason": "decomposition produced 9 children — Epic needs human re-scoping",
  "halt_category": "spec_missing",
  "max_estimated_loc": 100,
  "tickets": [],
  "next_pending": []
}
```

**Expected Jira halt comment on the Epic**

> Halt: authoring proposed 9 children at threshold 100 (Epic override). The > 8-children cap (workflow-epic-run.md, Authoring phase) requires human re-scoping before automation can help. Either: split the Epic into two sibling Epics, raise the threshold (default 400 typically yields fewer splits), or reduce the Epic's scope. Resume with `continue epic SCRUM-LARGE` after re-scoping.

The Phase 4 rule-effectiveness review uses these halts as evidence of where the > 8 cap fires; if it fires repeatedly in normal flow, the cap can be raised (or the decomposition heuristic refined).

## Cleanup procedure

After every authoring run, regardless of outcome:

1. **On y-approved successful drain:** standard epic-run Finish step (skill SCRUM-489+493) transitions Epic to Done, posts summary comment, marks `status: "complete"`. No additional cleanup needed.
2. **On halt:** state file retains `status: "halted"` + `halt_category` + `halt_reason`. Resume via `continue epic SCRUM-X` after fixing the cause.
3. **On rejected state-file load (Scenario C):** no state change; correct the file and re-invoke.

## Live-execution checklist (for the eventual real-Epic dry-run)

When a real upcoming Epic with no non-Done children is ready:

- [ ] Pre-write `.epic-run/<EPIC>.json` if testing an override threshold; leave absent for default.
- [ ] Invoke `run epic <EPIC>`.
- [ ] Capture the proposal output verbatim into a Jira comment on this validation ticket (or on a follow-up).
- [ ] Diff against Scenario A or B expected output above. Flag any divergence.
- [ ] On approval: confirm every child created carries `agent-authored` label (Atlassian MCP search: `labels = "agent-authored"`).
- [ ] After drain: capture estimated-vs-actual LOC for each child (from PR `additions`) into the Phase 4 governance log for the next rule-effectiveness review.
