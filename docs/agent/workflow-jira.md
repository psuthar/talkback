# Jira Ticket Implementation Workflow

Source of truth: This file owns the standard Jira ticket implementation lifecycle and completion/reporting requirements.

## Invocation Modes

- `implement SCRUM-XX`: standard mode (through PR creation + Jira In Review; stop there).
- `implement SCRUM-XX FULL_AUTO`: run standard mode first, then follow FULL_AUTO merge automation rules in `docs/agent/workflow-full-auto.md`.

## Status Management

- Before code edits/tests/implementation commits: transition ticket to **In Progress**.
- After implementation + validation + PR creation: transition ticket to **In Review**.
- FULL_AUTO-only, on successful merge: transition ticket to **Done**.

## Mandatory Sequence

### Step 0.5: Ticket-lint gate (mandatory; runs before step 1)

Run the structural lint against the Jira description **before** any transition to In Progress. The agent fetches the description via `mcp__atlassian__jira_get_issue`, converts ADF → Markdown (per `.claude/skills/jira-ticket-lint/SKILL.md`), and invokes:

```bash
python3 scripts/jira_ticket_lint.py \
  --description-file <tmp.md> --issue-type <type> --ticket <KEY>
```

| Lint exit | With `agent-authored` label | Action |
|---|---|---|
| `0` | n/a | Proceed to step 1. |
| `2` | yes | Auto-fix loop (`.claude/skills/jira-ticket-lint/SKILL.md`, max 1 retry). On second exit-2 → halt with `halt_category: "spec_missing"`; do not transition. |
| `2` | no | Halt immediately with Jira comment listing gaps; do not transition. Never mutate human-authored prose. |
| `1` | any | Halt regardless of label; structural failure (empty body, bad issue type) needs human attention. |

Rule set and rollback trigger are owned by `docs/agent/ticket-lint.md` (SCRUM-490).

**Rollout:** warn-only for the first calendar week after this gate lands — the lint runs and logs results, but does **not** block transition. From week 2 onward the gate is enforced. The flip date is recorded by inserting a row tagged `mode: enforce` into `ops/define-kpis/lint-runs.log` (manual marker before flipping). A 20-ticket sample with ≥ 80% pass rate must be observed before flipping; if the sample falls short, extend warn-only.

### Numbered sequence

1. Transition issue to In Progress (only after step 0.5 passes, or — during warn-only week — after step 0.5 records its outcome regardless of exit code).
2. Create and checkout `feat/<ticket-number>` from `main`.
3. Implement + validate on that feature branch only.
4. Push branch and create PR.
4.5. **PR-body lint gate (SCRUM-504, mandatory; same warn-only → enforce rollout as step 0.5).** Run the same `scripts/jira_ticket_lint.py` against the PR body with `--issue-type PR`; agent fetches the body via `mcp__github__pull_request_read (method: get)`. Three rules apply: `PR.jira_link` (body references `SCRUM-N`), `PR.summary` (≥ 1 bullet), `PR.test_plan` (≥ 1 checkbox). On exit 2 with `agent-authored` label on the linked Jira ticket → invoke the auto-fix loop in `.claude/skills/jira-ticket-lint/SKILL.md` (PR mode). On exit 2 without the label OR exit 1 → halt with a PR comment listing gaps; do not mutate the body. The lint runs against the same `ops/define-kpis/lint-runs.log` as Jira-ticket lint (each row carries `issue_type: "PR"` so the KPI snapshot can separate them).
5. Transition issue to In Review.
6. Post structured Jira completion comment.
7. FULL_AUTO only: continue with post-PR automation rules.

Hard stops:

- No In Progress transition before step 0.5 lint exits `0` (from week 2 onward; week 1 is warn-only).
- No In Review transition before step 4.5 PR lint exits `0` (from week 2 onward; week 1 is warn-only).
- No product-code edits/tests/PR finalization before step 1.
- No implementation commits on `main`.
- No In Review transition before PR exists.

## Branching

- Branch naming: `feat/<ticket-number>` (for example, `feat/SCRUM-12`)
- Order after In Progress: `git fetch origin`, `git checkout main`, `git pull`, `git checkout -b feat/<ticket-number>`

## Scope and Change Style

- Read and understand the ticket first.
- Implement only requested scope unless additional correctness fixes are required.
- Keep changes minimal and avoid unrelated refactors.

## Testing Policy Boundary

Testing requirements, hard stops, and validation gates are owned by `docs/agent/testing-validation.md`.
This workflow references that policy and does not redefine test minimums.

## Jira Completion Comment (Mandatory)

Post a regular issue comment (not only transition comments) using `jira_add_comment` with `body`.
Never use `comment` for this tool call; it causes API rejection and format drift retries.

Required structure:

1. Opening line with ticket complete statement + full PR URL.
2. Delivered outcomes.
3. Validation commands and outcomes.
4. Risks/deployment notes.
5. Optional follow-up items.

Mandatory formatting rule:

- Do not post freeform prose completion comments.
- If an API call fails, retry with corrected parameters but preserve this exact sectioned structure.
- Do not continue with FULL_AUTO handoff until the structured-format comment is confirmed posted.

Copy/paste template:

```text
<TICKET> implementation complete. PR: <full-pr-url>

Delivered outcomes
- <concrete file/module outcome>
- <concrete file/module outcome>

Validation commands and outcomes
- <command> -> PASS/FAIL
- <command> -> PASS/FAIL

Risks / deployment notes
- <risk, limitation, or dependency>

Optional follow-up
- <optional next action>
```

Minimum detail expectations:

- Include concrete file/module outcomes (not just "done").
- Include exact validation commands run and pass/fail outcome.
- Include any known risk, limitation, or dependency for follow-up.

Hard stop before FULL_AUTO merge handoff:

- Do not proceed to FULL_AUTO polling/merge steps until this structured Jira comment has been posted on the ticket.
- If missing, post the comment first, then continue.

## Commit and PR Requirements

- Commit message prefix: ticket key (for example, `SCRUM-12: ...`).
- Style-only frontend commits (pure cosmetic in `web/` only) must include `Style-only: <brief description>` line.
- Push branch and create PR targeting `main`.

PR body format:

1. Plan (executed)
2. Summary of changes
3. Validation
4. Acceptance criteria coverage
5. Refs

Completion output must include branch, validations, PR URL, Jira transition confirmations, completion-comment confirmation, summary, follow-ups, and FULL_AUTO state where applicable.

