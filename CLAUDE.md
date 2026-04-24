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

## Global Non-negotiables

1. Follow Jira sequence: In Progress before implementation work, In Review after PR, Done only on successful FULL_AUTO merge.
2. Work from `feat/<ticket-number>` branches; do not implement directly on `main`.
3. Keep changes minimal, scoped, and backward-compatible unless ticket scope requires otherwise.
4. Do not skip required tests; apply repository testing policy before commit and before completion.
5. For FULL_AUTO, obey TalkBack PR Gate PASS (`conclusion: success`) and `mergeable_state` clean before merge; **stop polling** when the gate completes non-PASS (see `docs/agent/workflow-full-auto.md`).
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
8. Transition Jira to In Review and post structured completion comment.
9. If FULL_AUTO was requested, execute `docs/agent/workflow-full-auto.md`.

## Planning Mode Reminder

If the user asks to plan (for example, `Plan SCRUM-13`), do not implement. Produce scope, risks, impacted systems, and test strategy first.

## Maintenance Convention

- Keep this file under roughly 80-140 lines.
- Put detailed policy text in `docs/agent/*` and link to it here.
- Avoid duplicating long procedural instructions across multiple files.
