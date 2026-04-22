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

1. Transition issue to In Progress.
2. Create and checkout `feat/<ticket-number>` from `main`.
3. Implement + validate on that feature branch only.
4. Push branch and create PR.
5. Transition issue to In Review.
6. Post structured Jira completion comment.
7. FULL_AUTO only: continue with post-PR automation rules.

Hard stops:

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

Required structure:

1. Opening line with ticket complete statement + full PR URL.
2. Delivered outcomes.
3. Validation commands and outcomes.
4. Risks/deployment notes.
5. Optional follow-up items.

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

