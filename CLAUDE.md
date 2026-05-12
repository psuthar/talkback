# TalkBack — Claude Project Memory

This is the repository entrypoint for agent behavior. Keep this file concise and stable. Detailed policy and workflow rules are split under `docs/agent/`.

## Policy Navigation

- Overview and global principles: `docs/agent/overview.md`
- MCP servers and command reference: `docs/agent/mcp-servers.md`
- Jira implementation workflow (standard mode): `docs/agent/workflow-jira.md`
- FULL_AUTO post-PR merge automation: `docs/agent/workflow-full-auto.md`
- Epic automation rules: `docs/agent/workflow-epic-run.md`
- Testing and validation requirements: `docs/agent/testing-validation.md`
- Subagent and test routing: `docs/agent/subagent-routing.md`
- Rule ownership map: `docs/agent/rule-ownership.md`

## Feature Specs

- Session templates (v1) — schema, validation, authoring guide: `docs/specs/session-templates-v1.md` (canonical example: [psuthar/talkback-templates](https://github.com/psuthar/talkback-templates))

## Global Non-negotiables

1. Follow Jira sequence: In Progress before implementation work, In Review after PR, Done only on successful FULL_AUTO merge.
2. Work from `feat/<ticket-number>` branches; do not implement directly on `main`.
3. Keep changes minimal, scoped, and backward-compatible unless ticket scope requires otherwise.
4. Do not skip required tests; apply repository testing policy before commit and before completion.
5. For FULL_AUTO (default polling path), obey TalkBack PR Gate PASS (`conclusion: success`) and `mergeable_state` clean before merge; **stop polling immediately** when the gate completes non-PASS. The opt-in `FULL_AUTO_WEBHOOK` variant delegates merge to a deployed routine (see `docs/agent/workflow-full-auto.md` and `docs/agent/pr-gate-webhook.md`).
6. Use GitHub MCP for PR lifecycle automation; avoid shell-based PR creation/edit when MCP tools are available.

## Karpathy-style Guardrails

1. Think before coding: make assumptions explicit and surface ambiguity.
2. Simplicity first: prefer the smallest complete solution.
3. Surgical changes only: touch only what the request requires.
4. Goal-driven execution: define checks and verify before marking complete.

These guardrails prioritize correctness and low-regression changes over speed.

## Quick Start Checklist

When asked to implement a Jira ticket:

1. Read ticket and identify affected areas.
2. Transition Jira to In Progress.
3. Create `feat/<ticket-number>` from latest `main`.
4. Implement requested scope only.
5. Add/update required tests per `docs/agent/testing-validation.md`.
6. Run validations and resolve failures.
7. Push and create PR to `main`.
8. Transition Jira to In Review and post structured completion comment (delivered outcomes + exact validations + risks/follow-ups).
9. If FULL_AUTO was requested, follow `docs/agent/workflow-full-auto.md`. Two invocation keywords exist:
   - **`implement SCRUM-XX FULL_AUTO`** (default) — polling path. Agent polls the gate + `mergeable_state` every 30s on a 40-min budget, calls `merge_pull_request` on PASS+clean (with mandatory pre-merge guard re-read), posts the Jira completion comment, transitions Jira to Done, and runs local cleanup (worktree FF per SCRUM-388 + branch -D). Post a final closure Jira comment naming `"polling path (default)"` as the path indicator. **No claude.ai routine quota consumed.**
   - **`implement SCRUM-XX FULL_AUTO_WEBHOOK`** (opt-in) — webhook routine path. **Add the literal line `<!-- full-auto-webhook -->` to the PR body** (SCRUM-394) — without it `release-readiness.yml` does not apply the `pr-gate:*` label and the routine never fires. Then push + Jira In Review, stop active work, and let the deployed routine merge in the cloud. Agent does only local cleanup + closure comment naming `"webhook path (FULL_AUTO_WEBHOOK)"`. Each `pull_request.labeled` / `pull_request.closed` event consumes one of ~15 daily claude.ai routine runs — use only when quota allows. Never put the literal marker comment in a `FULL_AUTO` (non-webhook) PR body.

## Planning Mode Reminder

If the user asks to plan (for example, `Plan SCRUM-13`), do not implement. Produce scope, risks, impacted systems, and test strategy first.

## Maintenance Convention

- Keep this file under roughly 80-140 lines.
- Put detailed policy text in `docs/agent/*` and link to it here.
- Avoid duplicating long procedural instructions across multiple files.
