#!/usr/bin/env python3
"""SCRUM-542: pre-implementation orchestration for `implement SCRUM-XX FULL_AUTO`.

Collapses the front-half MCP/Bash sequence into one structured call.
Today's flow per ticket costs ~3000 tokens of MCP echo (full ADF
description + the 5-transition payload + bash output). This script
issues those calls via the Atlassian REST API and returns one tight
JSON dump.

Steps performed:

1. **Fetch ticket** — REST GET ``/issue/<KEY>``; ADF description
   converted to Markdown via :mod:`scripts.full_auto.lib.adf`.
2. **Lint** — write the converted Markdown to a temp file and invoke
   ``scripts/jira_ticket_lint.py`` with ``--issue-type <TYPE>``.
3. **Auto-fix patch loop** — on exit 2 with the ``agent-authored``
   label, rewrite the description with a minimal section patch and
   re-lint once. SCRUM-491 owns the policy; this script implements
   the small patch in Python rather than depending on the skill text.
4. **Transition to In Progress** — resolved by name via
   ``resolve_transition_id_by_name`` so the script stays portable.
5. **Branch** — ``git fetch origin --prune`` + idempotent checkout of
   ``feat/<KEY>`` from ``origin/main``.

Returns a :class:`StartResult` dataclass; ``main()`` prints it as JSON.
The last entry of ``actions_taken`` is always the SCRUM-536 grep-able
summary line (``start.py succeeded:`` / ``aborted:`` / ``dry-run:``).
"""

from __future__ import annotations

import argparse
import json
import re
import subprocess
import sys
import tempfile
from dataclasses import asdict, dataclass, field
from pathlib import Path

from .lib import auth
from .lib import git_ops
from .lib.adf import adf_to_md
from .lib.jira import HttpJiraAPI, JiraAPI, resolve_transition_id_by_name

LINT_SCRIPT = Path("scripts/jira_ticket_lint.py")
AGENT_AUTHORED_LABEL = "agent-authored"
IN_PROGRESS_TRANSITION_NAME = "In Progress"

# Lint status enum values surfaced in CloseResult-style JSON output.
LINT_STATUS_PASS = "pass"
LINT_STATUS_PATCHED_THEN_PASS = "patched_then_pass"
LINT_STATUS_HALTED_GAPS = "halted_gaps"
LINT_STATUS_HALTED_UNFIXABLE = "halted_unfixable"


@dataclass
class StartResult:
    ticket: str
    dry_run: bool
    summary: str = ""
    issue_type: str = ""
    labels: list[str] = field(default_factory=list)
    description_md: str = ""
    lint_status: str = LINT_STATUS_PASS
    lint_gaps: list[dict] = field(default_factory=list)
    jira_transitioned: bool = False
    branch_name: str | None = None
    actions_taken: list[str] = field(default_factory=list)
    aborted_reason: str | None = None


def _act(result: StartResult, msg: str) -> None:
    result.actions_taken.append(msg)


def _summarize(result: StartResult) -> None:
    n = len(result.actions_taken)
    if result.aborted_reason:
        msg = f"start.py aborted: {result.aborted_reason}"
    elif result.dry_run:
        msg = f"start.py dry-run: {n} actions previewed"
    else:
        msg = f"start.py succeeded: {n} actions, no aborts"
    result.actions_taken.append(msg)


def _run_lint(description_md: str, issue_type: str, ticket: str) -> tuple[int, dict]:
    """Write the Markdown to a temp file and call jira_ticket_lint.py.
    Returns (exit_code, parsed_stdout_json)."""
    with tempfile.NamedTemporaryFile(
        mode="w", suffix=f"-{ticket}.md", delete=False
    ) as f:
        f.write(description_md)
        tmp_path = f.name
    try:
        proc = subprocess.run(
            [
                sys.executable,
                str(LINT_SCRIPT),
                "--description-file",
                tmp_path,
                "--issue-type",
                issue_type,
                "--ticket",
                ticket,
            ],
            capture_output=True,
            text=True,
            check=False,
        )
        try:
            out = json.loads(proc.stdout)
        except json.JSONDecodeError:
            out = {"raw": proc.stdout}
        return proc.returncode, out
    finally:
        try:
            Path(tmp_path).unlink()
        except OSError:
            pass


_HEADING_RE = re.compile(r"^(\s*#+\s+)(.*?)(\s*)$")


def _patch_description(description_md: str, gaps: list[dict]) -> str:
    """SCRUM-542 / SCRUM-543: minimal section-patch for agent-authored exit-2 gaps.

    Mirrors the algorithm documented in ``.claude/skills/jira-ticket-lint``:
    for each gap whose ``rule_id`` is one of the known fixable shapes,
    add or repair the section. Returns the patched Markdown.

    Supported repairs (matching the lint rules — see ``docs/agent/ticket-lint.md``):

    Jira-ticket rules:
    * ``AC.present`` / ``AC.min_count`` — ensure ``## Acceptance criteria``
      with at least one (or three for Story) checkboxes.
    * ``BUG.repro`` — ensure non-empty ``## Reproduction``.
    * ``EPIC.goal`` / ``EPIC.scope_present`` / ``EPIC.success_criteria`` —
      ensure the named section; success-criteria gets two checkboxes.

    PR-body rules (SCRUM-543; ``review.py`` reuses this helper):
    * ``PR.summary`` — ensure ``## Summary`` with at least one bullet.
    * ``PR.test_plan`` — ensure ``## Test plan`` with at least one checkbox.

    Other rules fall through unpatched (the lint will fail a second time
    and the loop halts). ``PR.jira_link`` is intentionally not patched —
    the fix requires the ticket key, which this helper doesn't carry;
    the agent's PR-body template should include the Jira section to begin
    with, so a missing link is a real authoring gap that warrants halt.
    """
    text = description_md.rstrip() + "\n"
    for gap in gaps:
        section = gap.get("section", "")
        rule_id = gap.get("rule_id", "")
        if not section:
            continue
        header = f"## {section}\n"
        if not _section_exists(text, section):
            text = text.rstrip() + "\n\n" + header
        # Ensure the section has the minimum content the rule wants.
        if rule_id in ("AC.present", "AC.min_count"):
            need = 3 if rule_id == "AC.min_count" else 1
            text = _ensure_checkboxes(text, section, need)
        elif rule_id == "EPIC.success_criteria":
            text = _ensure_checkboxes(text, section, 2)
        elif rule_id in ("BUG.repro", "EPIC.goal", "EPIC.scope_present"):
            text = _ensure_section_body(text, section, "(fill in)")
        elif rule_id == "PR.test_plan":
            text = _ensure_checkboxes(text, section, 1)
        elif rule_id == "PR.summary":
            text = _ensure_bullets(text, section, 1)
    return text


def _section_exists(text: str, section: str) -> bool:
    target = section.lower()
    for line in text.splitlines():
        m = _HEADING_RE.match(line)
        if m and m.group(2).strip().rstrip(":").strip().lower() == target:
            return True
    return False


def _ensure_section_body(text: str, section: str, placeholder: str) -> str:
    """If the named section exists but is empty, insert ``placeholder``."""
    lines = text.splitlines()
    target = section.lower()
    for i, line in enumerate(lines):
        m = _HEADING_RE.match(line)
        if not m or m.group(2).strip().rstrip(":").strip().lower() != target:
            continue
        # Check whether the next non-blank line is another heading.
        j = i + 1
        while j < len(lines) and not lines[j].strip():
            j += 1
        if j >= len(lines) or _HEADING_RE.match(lines[j]):
            # empty section — insert placeholder after the heading line
            lines.insert(i + 1, "")
            lines.insert(i + 2, placeholder)
            break
    return "\n".join(lines) + "\n"


_CHECKBOX_RE = re.compile(r"^\s*-\s+\[[ xX]\]\s+\S")
_BULLET_RE = re.compile(r"^\s*-\s+\S")  # any bullet (including checkboxes)


def _ensure_bullets(text: str, section: str, need: int) -> str:
    """SCRUM-543: PR-mode helper. Ensure the named section has at least
    ``need`` non-empty bullets. Used for ``PR.summary``."""
    lines = text.splitlines()
    target = section.lower()
    for i, line in enumerate(lines):
        m = _HEADING_RE.match(line)
        if not m or m.group(2).strip().rstrip(":").strip().lower() != target:
            continue
        count = 0
        end = i + 1
        for j in range(i + 1, len(lines)):
            if _HEADING_RE.match(lines[j]):
                end = j
                break
            if _BULLET_RE.match(lines[j]):
                count += 1
            end = j + 1
        if count < need:
            missing = need - count
            insert_at = end
            extra = ["- (fill in)" for _ in range(missing)]
            if insert_at > 0 and not lines[insert_at - 1].strip():
                lines[insert_at:insert_at] = extra
            else:
                lines[insert_at:insert_at] = [""] + extra
        break
    return "\n".join(lines) + "\n"


def _ensure_checkboxes(text: str, section: str, need: int) -> str:
    lines = text.splitlines()
    target = section.lower()
    for i, line in enumerate(lines):
        m = _HEADING_RE.match(line)
        if not m or m.group(2).strip().rstrip(":").strip().lower() != target:
            continue
        # Count existing checkboxes within the section.
        count = 0
        end = i + 1
        for j in range(i + 1, len(lines)):
            if _HEADING_RE.match(lines[j]):
                end = j
                break
            if _CHECKBOX_RE.match(lines[j]):
                count += 1
            end = j + 1
        # Append missing checkboxes just before the next heading.
        if count < need:
            missing = need - count
            insert_at = end
            extra = ["- [ ] (fill in)" for _ in range(missing)]
            # Pad with a blank line before the new items if the previous
            # non-blank line is the section heading itself.
            if insert_at > 0 and not lines[insert_at - 1].strip():
                lines[insert_at:insert_at] = extra
            else:
                lines[insert_at:insert_at] = [""] + extra
        break
    return "\n".join(lines) + "\n"


def start(
    ticket: str,
    *,
    dry_run: bool = False,
    jira_api: JiraAPI | None = None,
    repo_root: Path | None = None,
) -> StartResult:
    """SCRUM-542: orchestrate the pre-implementation steps.

    ``jira_api`` is injectable for tests (mirrors close.py's pattern).
    Defaults to ``HttpJiraAPI`` configured from :func:`lib.auth.jira_auth`.
    """
    repo_root = repo_root or Path.cwd()
    if jira_api is None:
        email, token = auth.jira_auth()
        jira_api = HttpJiraAPI(auth.atlassian_base_url(), email, token)

    result = StartResult(ticket=ticket, dry_run=dry_run)

    # Step 1: fetch ticket
    issue = jira_api.get_issue(ticket)
    result.summary = issue["summary"]
    result.issue_type = issue["issuetype"]
    result.labels = issue["labels"]
    result.description_md = adf_to_md(issue["description"]).rstrip() + "\n"
    _act(
        result,
        f"fetched {ticket}: type={result.issue_type} labels={result.labels}",
    )

    # Step 2: lint
    exit_code, lint_out = _run_lint(result.description_md, result.issue_type, ticket)
    if exit_code == 0:
        result.lint_status = LINT_STATUS_PASS
        _act(result, f"lint pass (issue_type={result.issue_type})")
    elif exit_code == 1:
        result.lint_status = LINT_STATUS_HALTED_UNFIXABLE
        result.lint_gaps = lint_out.get("gaps", [])
        result.aborted_reason = f"lint exit 1 (unfixable): {result.lint_gaps}"
        _act(result, f"aborted: {result.aborted_reason}")
        _summarize(result)
        return result
    elif exit_code == 2:
        gaps = lint_out.get("gaps", [])
        if AGENT_AUTHORED_LABEL not in result.labels:
            # Human-authored ticket with fixable gaps — halt per skill policy.
            result.lint_status = LINT_STATUS_HALTED_GAPS
            result.lint_gaps = gaps
            result.aborted_reason = (
                f"lint exit 2 with gaps {[g.get('rule_id') for g in gaps]} "
                f"(human-authored; auto-fix forbidden)"
            )
            _act(result, f"aborted: {result.aborted_reason}")
            _summarize(result)
            return result
        # Agent-authored: patch + retry once.
        patched = _patch_description(result.description_md, gaps)
        if dry_run:
            _act(result, f"would patch description and update {ticket}")
        else:
            jira_api.update_issue(ticket, {"description": patched})
            _act(result, f"patched description on {ticket} (rule_ids={[g.get('rule_id') for g in gaps]})")
        # Re-fetch + re-lint.
        result.description_md = patched
        exit_code, lint_out = _run_lint(patched, result.issue_type, ticket)
        if exit_code == 0:
            result.lint_status = LINT_STATUS_PATCHED_THEN_PASS
            _act(result, "lint pass after patch")
        else:
            result.lint_status = LINT_STATUS_HALTED_GAPS
            result.lint_gaps = lint_out.get("gaps", [])
            result.aborted_reason = f"lint exit {exit_code} after patch retry"
            _act(result, f"aborted: {result.aborted_reason}")
            _summarize(result)
            return result

    # Step 3: transition to In Progress
    if dry_run:
        _act(result, f"would transition {ticket} to {IN_PROGRESS_TRANSITION_NAME}")
    else:
        tid = resolve_transition_id_by_name(jira_api, ticket, IN_PROGRESS_TRANSITION_NAME)
        jira_api.transition(ticket, tid)
        result.jira_transitioned = True
        _act(result, f"transitioned {ticket} to {IN_PROGRESS_TRANSITION_NAME} (id={tid})")

    # Step 4: branch
    branch = f"feat/{ticket}"
    result.branch_name = branch
    if dry_run:
        _act(result, f"would fetch + checkout {branch} from origin/main")
    else:
        git_ops.fetch_main(cwd=repo_root)
        # Idempotent: if the branch exists locally, just check it out.
        try:
            subprocess.run(
                ["git", "rev-parse", "--verify", branch],
                cwd=str(repo_root),
                capture_output=True,
                check=True,
            )
            subprocess.run(
                ["git", "checkout", branch],
                cwd=str(repo_root),
                capture_output=True,
                text=True,
                check=True,
            )
            _act(result, f"checked out existing {branch}")
        except subprocess.CalledProcessError:
            subprocess.run(
                ["git", "checkout", "-b", branch, "origin/main"],
                cwd=str(repo_root),
                capture_output=True,
                text=True,
                check=True,
            )
            _act(result, f"created {branch} from origin/main")

    _summarize(result)
    return result


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("ticket", help="Jira ticket key, e.g. SCRUM-542")
    parser.add_argument(
        "--dry-run",
        action="store_true",
        help="Preview; no Jira or git mutations.",
    )
    args = parser.parse_args(argv)
    result = start(args.ticket, dry_run=args.dry_run)
    print(json.dumps(asdict(result), indent=2))
    return 1 if result.aborted_reason else 0


if __name__ == "__main__":
    sys.exit(main())
