# Ticket-lint rules

Owner: this file holds the canonical rule list. The lint script `scripts/jira_ticket_lint.py` (SCRUM-490) is the executable mirror; the `jira-ticket-lint` skill (SCRUM-491) holds the auto-fix loop policy; `docs/agent/workflow-jira.md` (SCRUM-492) owns the hard-stop wiring. See `docs/agent/rule-ownership.md` for the cross-reference.

The lint runs during step 0.5 of `docs/agent/workflow-jira.md` — before any Jira transition to In Progress.

## Rule set

| Rule ID | Issue type | Section | Constraint | Exit |
|---|---|---|---|---|
| `AC.present` | Story, Task, Bug | Acceptance criteria | section present + ≥ 1 checkbox | 2 (fixable) |
| `AC.min_count` | Story | Acceptance criteria | ≥ 3 checkboxes | 2 (fixable) |
| `BUG.repro` | Bug | Reproduction | non-empty section | 2 (fixable) |
| `EPIC.goal` | Epic | Goal | non-empty section | 2 (fixable) |
| `EPIC.scope_present` | Epic | Scope | non-empty section | 2 (fixable) |
| `EPIC.success_criteria` | Epic | Success criteria | section present + ≥ 2 checkboxes | 2 (fixable) |
| `PR.jira_link` | PR | (whole body) | matches `SCRUM-\d+` regex anywhere | 2 (fixable) |
| `PR.summary` | PR | Summary | section present + ≥ 1 non-empty bullet | 2 (fixable) |
| `PR.test_plan` | PR | Test plan | section present + ≥ 1 checkbox | 2 (fixable) |
| `STRUCT.empty` | any | (whole body) | description non-empty | 1 (unfixable) |
| `STRUCT.bad_type` | any | (meta) | `issue_type ∈ {Epic, Story, Task, Bug, PR}` | 1 (unfixable) |

Section-header matching is case-insensitive and tolerates ATX (`#`/`##`/`###`) and bold-only (`**Heading**`) styles. The rule list is intentionally small for v1; new rules land via the Phase 4 rule-effectiveness review (every 2 sprints) when log evidence shows they would block real issues.

**PR-mode (SCRUM-504):** the same script lints PR bodies via `--issue-type PR`. Agent runtime invokes after `mcp__github__create_pull_request` (or against the prepared body before creation); rules mirror the canonical format documented in `docs/agent/workflow-jira.md` Jira Completion Comment section and surfaced at PR-creation time by `.github/PULL_REQUEST_TEMPLATE.md` (SCRUM-503). Auto-fix loop applies when the linked Jira ticket carries the `agent-authored` label — the lint script does not fetch the ticket itself; the agent runtime resolves the label before invoking the auto-fix patch.

## Exit codes

| Code | Meaning | Agent response |
|---|---|---|
| `0` | Pass | Proceed to transition to In Progress. |
| `2` | Fixable gaps | If ticket carries label `agent-authored` → run auto-fix loop (SCRUM-491). Otherwise → halt with `halt_category: spec_missing` and post the gap list as a Jira comment to the human author. |
| `1` | Unfixable | Always halt with `halt_category: spec_missing`. Do not attempt to auto-fix. |

## Invocation

```bash
# Jira ticket
python3 scripts/jira_ticket_lint.py \
  --description-file /tmp/SCRUM-XX.md \
  --issue-type Story \
  --ticket SCRUM-XX

# PR body (SCRUM-504)
python3 scripts/jira_ticket_lint.py \
  --description-file /tmp/pr-N-body.md \
  --issue-type PR \
  --ticket SCRUM-XX
```

The agent pre-fetches the description via the Atlassian MCP and writes it to disk. The script never calls Jira; it stays credential-free and dependency-free.

Exit-2 output on stdout (structured for the auto-fix loop to read):

```json
{
  "gaps": [
    {"rule_id": "AC.min_count", "section": "Acceptance criteria", "message": "Story requires >=3 Acceptance criteria checkboxes, found 2"}
  ],
  "fixable": true,
  "issue_type": "Story",
  "ticket": "SCRUM-XX"
}
```

Every invocation appends one JSONL row to `ops/define-kpis/lint-runs.log`. The KPI snapshot script (`scripts/define_kpi_snapshot.py`, SCRUM-488) reads this log to compute the `lint_pass_rate` field.

## Rollout

- **Week 1: warn-only.** The lint runs and logs results, but `workflow-jira.md` does NOT block transition on non-zero exit.
- **Week 2+: hard stop.** Transition blocked on non-zero exit unless the auto-fix loop (SCRUM-491) succeeds.

The transition date is recorded as the first non-zero `transitioned` row in `ops/define-kpis/lint-runs.log`.

## Rollback trigger

Soften enforcement to warn-only and open an investigation ticket if either:

- Lint pass-rate < 50% for 2 consecutive weeks (KPI source: `define_kpi_snapshot.py`)
- Median time-to-In-Progress jumps > 30% vs the pre-Phase-1 baseline snapshot

Operationalised by SCRUM-492 (the workflow-jira hard-stop ticket).

## Rule-effectiveness review (every 2 sprints — Phase 4 governance)

Read `ops/define-kpis/lint-runs.log`. For each rule:

- **Retire candidates** — rules that haven't blocked anything for 3 consecutive sprints. Cap retirement at 20% of rules per review to prevent runaway pruning.
- **Revise candidates** — rules that fire on tickets a human reviewer later marked valid. Adjust threshold or scope.
- **Promote candidates** — recurring patterns currently caught by `STRUCT` rules or by `other`-categorised halts; promote to first-class rules.

Findings land as a Jira comment on the parent Epic for the active uplift cycle, plus an edit to this file's rule table.
