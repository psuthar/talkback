# Skill: jira-ticket-lint

Policy source: `docs/agent/workflow-jira.md` (hard-stop wiring, SCRUM-492) and `docs/agent/ticket-lint.md` (rule list).
Script: `scripts/jira_ticket_lint.py` (SCRUM-490).
This skill covers the agent-side auto-fix loop and halt-with-comment policy. It does not own the rule set or the hard-stop policy.

## Purpose

Run the structural lint against a Jira ticket description before transitioning it to In Progress, and — for agent-authored tickets only — automatically patch the description on fixable failures. Never silently mutate human-authored prose.

## Invocation

```
lint jira ticket SCRUM-XX
```

This skill is also invoked implicitly by `docs/agent/workflow-jira.md` step 0.5 (SCRUM-492) before any `jira_transition_issue` to In Progress.

---

## Algorithm

### 1. Fetch the description

```python
mcp__atlassian__jira_get_issue(issueKey="SCRUM-XX")
```

The MCP returns Atlassian Document Format (ADF). Convert to a Markdown string for the lint script. A reasonable conversion:

- ADF `heading` → `#` × `attrs.level` + text
- ADF `paragraph` → plain text + blank line
- ADF `bulletList` → `- ` lines
- ADF `taskList` → `- [ ]` or `- [x]` lines based on `attrs.state`
- ADF `text` → plain text (preserve `marks` only as Markdown emphasis when needed)

The conversion does not need to be bidirectional or perfect — the lint only inspects section headers and checkbox lines, both of which the rough conversion preserves faithfully. Write the converted Markdown to a temp file under `/tmp/scrum-lint-<TICKET>-<ts>.md`.

### 2. Run the lint script

```bash
python3 scripts/jira_ticket_lint.py \
  --description-file /tmp/scrum-lint-<TICKET>-<ts>.md \
  --issue-type <Epic|Story|Task|Bug> \
  --ticket SCRUM-XX \
  --max-retries 1
```

Parse the exit code:

| Exit | Stdout shape | Action |
|---|---|---|
| `0` | `{"pass": true, ...}` | Proceed — transition the ticket to In Progress per workflow-jira step 1. |
| `2` | `{"gaps": [...], "fixable": true, ...}` | Branch on `agent-authored` label (see step 3). |
| `1` | `{"gaps": [...], "fixable": false, ...}` | Halt regardless of label (see step 4). |

### 3. Exit 2 — branch on `agent-authored` label

Check `issue.fields.labels` (already fetched in step 1) for `agent-authored`.

**With label** (agent-authored ticket — `.claude/skills/jira-ticket-authoring/SKILL.md` applies it on every create):

1. Read structured gaps from stdout JSON.
2. Patch the description section-by-section using `jira_update_issue`. **Never overwrite the full `description` field**: build a patched Markdown body that preserves untouched sections verbatim and only modifies the sections named in `gaps[].section`.
3. Re-run the lint script (counts against `--max-retries=1`).
4. If the re-run returns exit 0 → proceed.
5. If the re-run returns exit 2 again → halt per step 4 with `halt_category: "spec_missing"`. Do NOT loop further; one retry is the maximum.

**Without label** (human-authored ticket):

- Halt immediately (step 4). Do not patch.

### 4. Halt with comment

When halting (exit 1, or exit 2 without label, or exit 2 after retry):

1. Post a Jira comment listing the gaps:

   ```
   Lint failure — cannot transition to In Progress.

   Gaps:
   - <rule_id>: <message>
   - <rule_id>: <message>

   Fix the description (see docs/agent/ticket-lint.md for the rule list) and re-run `implement SCRUM-XX` or `continue epic SCRUM-XX`.
   ```

2. Do NOT call `jira_transition_issue` — the ticket stays in its current state.
3. If running inside `epic-run`, write `halt_category: "spec_missing"` to `.epic-run/<EPIC>.json` and stop the run.
4. If running standalone (`implement SCRUM-XX`), return to the user with the gap list.

---

## What auto-fix is allowed to change

**Allowed:**
- Add a missing section (e.g. `## Acceptance criteria` for a Story that lacks one).
- Add missing checkboxes to bring a section to the minimum count (`AC.min_count` for Story).
- Fill an empty section that has only the header.

**Not allowed:**
- Rewriting existing prose the user / authoring skill produced.
- Changing the title.
- Modifying labels.
- Adding sections beyond what the lint rule requires (no "while we're here" expansions).

The principle: every change must be defensible from a single failing rule_id. If a change would not be undone by removing that one rule, the change is out of scope.

---

## ADF conversion — minimal pattern

Pseudocode:

```python
def adf_to_md(adf: dict) -> str:
    parts = []
    for node in adf.get("content", []):
        t = node.get("type")
        if t == "heading":
            level = node.get("attrs", {}).get("level", 1)
            parts.append("#" * level + " " + _text(node))
        elif t == "paragraph":
            parts.append(_text(node))
        elif t == "bulletList":
            parts.extend("- " + _text(li) for li in node.get("content", []))
        elif t == "taskList":
            for item in node.get("content", []):
                state = item.get("attrs", {}).get("state", "TODO")
                marker = "[x]" if state == "DONE" else "[ ]"
                parts.append(f"- {marker} " + _text(item))
        parts.append("")  # blank line between blocks
    return "\n".join(parts)
```

The lint script reads ATX (`#`/`##`/`###`) headers and `- [ ]` checkboxes. The conversion above covers both.

---

## Constraints

- **Single retry cap.** The `--max-retries=1` flag on the lint script is the source of truth; the skill MUST NOT call lint a third time after a single patched re-run.
- **Section-only patches.** Auto-fix patches one section at a time via `jira_update_issue`. If two sections need patching (multi-gap), apply both edits in one `jira_update_issue` call but keep each section's edit minimal.
- **`agent-authored` is required for auto-fix.** Absent label → halt; never silently mutate human prose. The `jira-ticket-authoring` skill (SCRUM-491 side) applies the label on every `jira_create_issue`.
- **No auto-fix for exit 1.** Structural failures (empty body, bad issue type) always halt — humans must address.
- **No write before transition.** The lint runs before `jira_transition_issue`; the auto-fix updates description but does not transition. The caller (`workflow-jira` step 0.5) transitions only after lint returns exit 0.
- **Log every invocation.** The lint script writes a JSONL row to `ops/define-kpis/lint-runs.log` on every call (including auto-fix retries). Do not pass `--log=` to disable except in tests.

---

## PR-body lint (SCRUM-504)

The same script lints PR descriptions via `--issue-type PR`. Three rules apply:

| Rule | Constraint |
|---|---|
| `PR.jira_link` | body matches `SCRUM-\d+` somewhere |
| `PR.summary` | `## Summary` section present + ≥ 1 non-empty bullet |
| `PR.test_plan` | `## Test plan` section present + ≥ 1 checkbox |

### When to run

After `mcp__github__create_pull_request` returns the PR number, or against the prepared body before creation. The agent runtime invokes the lint as part of `docs/agent/workflow-jira.md` step 4.5 (warn-only week 1 → enforce week 2+, mirroring the step 0.5 rollout).

### Algorithm

1. Fetch the PR body via `mcp__github__pull_request_read (method: get)`; capture `body` field.
2. Write the body to a temp file (e.g. `/tmp/pr-<N>-body.md`).
3. Resolve the linked Jira ticket from the body (`SCRUM-\d+` regex match — the same pattern the lint enforces).
4. Fetch the Jira ticket's labels via `mcp__atlassian__jira_get_issue`. Cache the labels alongside the PR check so this is one MCP call per PR per session.
5. Run `python3 scripts/jira_ticket_lint.py --description-file /tmp/pr-<N>-body.md --issue-type PR --ticket SCRUM-XX`.
6. Branch on exit code:
   - `0` → proceed (transition Jira to In Review, post completion comment, FULL_AUTO polling per existing flow).
   - `2` with `agent-authored` label on the linked Jira ticket → run the auto-fix patch loop (below) once; halt if second exit-2.
   - `2` without `agent-authored` label → halt; post a PR comment listing gaps; do NOT mutate the body.
   - `1` → halt regardless of label.

### Auto-fix patch loop (PR mode)

1. Read structured gaps from stdout JSON.
2. Build a patched body by inserting/repairing only the named sections. Never overwrite the full body — preserve the human-written or human-edited prose.
3. Apply via `mcp__github__update_pull_request` (or equivalent body-update path). Same section-only patching rule as Jira tickets.
4. Re-run lint with `--max-retries=1` semantics (the cap lives in the lint script; the skill MUST NOT call lint a third time after a single patched re-run).
5. On second exit-2 → halt with `halt_category: spec_missing` (epic context) or post a PR comment + halt the implement flow.

### What auto-fix is allowed to change

Same scope guard as Jira-ticket auto-fix:
- **Allowed:** add a missing `## Jira` line with the SCRUM-N reference; add a missing `## Summary` section with a placeholder bullet (e.g. `- See commit message.`); add a missing `## Test plan` section with a single `- [ ]` placeholder ("(fill in)").
- **Not allowed:** rewriting existing Summary/Test plan content, changing the PR title, modifying labels, restructuring sections beyond the failing rule's scope.

### Authorship signal — why we check the Jira ticket, not the PR author

The PR author is usually the human under whose GitHub account the agent acts (the same misleading signal as Jira `creator`). The linked Jira ticket's `agent-authored` label is the only reliable authorship signal. This is consistent with the Jira-ticket-mode loop. The agent runtime resolves the label once per PR per session and caches it.

If a PR body has no Jira reference at all (`PR.jira_link` failed), the auto-fix loop CANNOT determine authorship and must halt with a comment regardless of any inferred label.
