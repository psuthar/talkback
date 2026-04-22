# Agent Rule Ownership Map

Source of truth: This file maps each rule family to a single owning policy file and the related execution skills.

## Ownership Model

- Policy files under `docs/agent/*` own normative rules.
- Skill files under `.claude/skills/*/SKILL.md` own execution tactics, examples, and checklists.
- `CLAUDE.md` remains a concise index and non-negotiable summary.

## Rule Families

| Rule family | Policy owner | Related skills (execution only) |
|---|---|---|
| Project context, architecture, global guardrails | `docs/agent/overview.md` | `repo-map`, `feature-plan` |
| Repository commands and MCP setup | `docs/agent/mcp-servers.md` | `repo-map`, `epic-run` |
| Jira implementation lifecycle | `docs/agent/workflow-jira.md` | `epic-run` |
| FULL_AUTO merge gating and cleanup | `docs/agent/workflow-full-auto.md` | `epic-run` |
| Epic orchestration policy | `docs/agent/workflow-epic-run.md` | `epic-run` |
| Test requirements and validation gates | `docs/agent/testing-validation.md` | `smoke-tests`, `e2e-tests` |
| Subagent and test routing policy | `docs/agent/subagent-routing.md` | `smoke-tests`, `e2e-tests`, `repo-map` |
| Ticket drafting templates/decomposition methods | N/A (skills own these methods) | `jira-ticket-authoring`, `jira-work-decomposition` |

## Usage Rules

1. If a rule applies across tasks, put it in `docs/agent/*`.
2. Skills should link to policy owners instead of restating policy text.
3. If policy and skill conflict, policy files win.

