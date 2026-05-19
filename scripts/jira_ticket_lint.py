#!/usr/bin/env python3
"""SCRUM-490: ticket lint for Jira issue descriptions.

Validates a Jira issue description against the rule set documented in
``docs/agent/ticket-lint.md``. The script is invoked by the agent
(typically before transitioning a ticket to In Progress per
``docs/agent/workflow-jira.md``) and by the ``jira-ticket-lint`` skill
(SCRUM-491) for the auto-fix loop.

Exit codes:
  0 — pass
  2 — gaps found, fixable (stdout is JSON with the gap list)
  1 — gaps found, unfixable / structural (e.g. empty description)

The script is pure aggregation: it does NOT call Jira. The agent
pre-fetches the description via the Atlassian MCP and passes it on disk
via ``--description-file`` (or inline via ``--description``). The
``--ticket`` flag is informational and used only when writing the
``ops/define-kpis/lint-runs.log`` row.
"""

from __future__ import annotations

import argparse
import json
import re
import sys
from dataclasses import asdict, dataclass
from datetime import datetime, timezone
from pathlib import Path
from typing import Callable


@dataclass
class Gap:
    rule_id: str
    section: str
    message: str


@dataclass
class LintResult:
    exit_code: int  # 0 pass, 2 fixable, 1 unfixable
    gaps: list[Gap]
    fixable: bool


VALID_ISSUE_TYPES = ("Epic", "Story", "Task", "Bug")


_HEADING_PATTERNS = (
    # Markdown ATX: # Heading, ## Heading, etc.
    re.compile(r"^\s*#+\s+(.*?)\s*$"),
    # Bold-only "heading": **Heading**
    re.compile(r"^\s*\*\*(.+?)\*\*\s*:?\s*$"),
)


def _heading_text(line: str) -> str | None:
    for pat in _HEADING_PATTERNS:
        m = pat.match(line)
        if m:
            return m.group(1).strip().rstrip(":").strip()
    return None


def _find_section(text: str, section_name: str) -> str | None:
    """Find a section by name (case-insensitive). Return body or None."""
    target = section_name.lower()
    in_section = False
    body: list[str] = []
    for line in text.split("\n"):
        heading = _heading_text(line)
        if heading is not None:
            if in_section:
                break
            if heading.lower() == target:
                in_section = True
                continue
        if in_section:
            body.append(line)
    if not in_section:
        return None
    return "\n".join(body).strip()


_CHECKBOX_RE = re.compile(r"^\s*-\s+\[[ xX]\]\s+\S")


def _count_checkboxes(section_body: str) -> int:
    if not section_body:
        return 0
    return sum(1 for line in section_body.split("\n") if _CHECKBOX_RE.match(line))


# ---- Rule functions ----


def rule_ac_present(description: str, issue_type: str) -> list[Gap]:
    # Bug uses Observed / Expected / Reproduction / Impact instead of Acceptance
    # criteria — see jira-ticket-authoring template. BUG.repro is the structural
    # check for Bug; AC.present applies only to Story and Task.
    if issue_type not in ("Story", "Task"):
        return []
    body = _find_section(description, "Acceptance criteria")
    if body is None:
        return [
            Gap(
                "AC.present",
                "Acceptance criteria",
                "missing 'Acceptance criteria' section",
            )
        ]
    if _count_checkboxes(body) < 1:
        return [
            Gap(
                "AC.present",
                "Acceptance criteria",
                "Acceptance criteria section has no checkbox items",
            )
        ]
    return []


def rule_ac_min_count(description: str, issue_type: str) -> list[Gap]:
    if issue_type != "Story":
        return []
    body = _find_section(description, "Acceptance criteria") or ""
    count = _count_checkboxes(body)
    if count < 3:
        return [
            Gap(
                "AC.min_count",
                "Acceptance criteria",
                f"Story requires >=3 Acceptance criteria checkboxes, found {count}",
            )
        ]
    return []


def rule_bug_repro(description: str, issue_type: str) -> list[Gap]:
    if issue_type != "Bug":
        return []
    body = _find_section(description, "Reproduction")
    if not body:
        return [
            Gap(
                "BUG.repro",
                "Reproduction",
                "Bug requires a non-empty 'Reproduction' section",
            )
        ]
    return []


def rule_epic_goal(description: str, issue_type: str) -> list[Gap]:
    if issue_type != "Epic":
        return []
    body = _find_section(description, "Goal")
    if not body:
        return [Gap("EPIC.goal", "Goal", "Epic requires a non-empty 'Goal' section")]
    return []


def rule_epic_scope_present(description: str, issue_type: str) -> list[Gap]:
    if issue_type != "Epic":
        return []
    body = _find_section(description, "Scope")
    if not body:
        return [
            Gap(
                "EPIC.scope_present",
                "Scope",
                "Epic requires a non-empty 'Scope' section",
            )
        ]
    return []


def rule_epic_success_criteria(description: str, issue_type: str) -> list[Gap]:
    if issue_type != "Epic":
        return []
    body = _find_section(description, "Success criteria")
    if body is None:
        return [
            Gap(
                "EPIC.success_criteria",
                "Success criteria",
                "Epic requires a 'Success criteria' section",
            )
        ]
    if _count_checkboxes(body) < 2:
        return [
            Gap(
                "EPIC.success_criteria",
                "Success criteria",
                "Epic Success criteria requires >=2 checkbox items",
            )
        ]
    return []


RULES: list[Callable[[str, str], list[Gap]]] = [
    rule_ac_present,
    rule_ac_min_count,
    rule_bug_repro,
    rule_epic_goal,
    rule_epic_scope_present,
    rule_epic_success_criteria,
]


def lint(description: str, issue_type: str) -> LintResult:
    if not description or not description.strip():
        return LintResult(
            exit_code=1,
            gaps=[Gap("STRUCT.empty", "(whole body)", "description is empty")],
            fixable=False,
        )
    if issue_type not in VALID_ISSUE_TYPES:
        return LintResult(
            exit_code=1,
            gaps=[
                Gap(
                    "STRUCT.bad_type",
                    "(meta)",
                    f"issue_type {issue_type!r} not in {VALID_ISSUE_TYPES}",
                )
            ],
            fixable=False,
        )
    gaps: list[Gap] = []
    for rule in RULES:
        gaps.extend(rule(description, issue_type))
    if not gaps:
        return LintResult(exit_code=0, gaps=[], fixable=True)
    return LintResult(exit_code=2, gaps=gaps, fixable=True)


def _log_run(
    log_path: Path | None,
    ticket: str | None,
    issue_type: str,
    result: LintResult,
) -> None:
    if log_path is None:
        return
    log_path.parent.mkdir(parents=True, exist_ok=True)
    row = {
        "ts": datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"),
        "ticket": ticket,
        "issue_type": issue_type,
        "exit": result.exit_code,
        "gaps": [asdict(g) for g in result.gaps],
    }
    with log_path.open("a") as f:
        f.write(json.dumps(row) + "\n")


def main(argv: list[str] | None = None) -> int:
    p = argparse.ArgumentParser(
        description="Lint Jira issue descriptions for structural completeness.",
    )
    src = p.add_mutually_exclusive_group(required=True)
    src.add_argument(
        "--description-file",
        type=Path,
        help="Path to file containing the issue description (UTF-8).",
    )
    src.add_argument(
        "--description",
        help="Inline description text (use --description-file for non-trivial bodies).",
    )
    p.add_argument(
        "--issue-type",
        required=True,
        choices=list(VALID_ISSUE_TYPES),
        help="Jira issue type. Drives which rules apply.",
    )
    p.add_argument(
        "--ticket",
        default=None,
        help="SCRUM-XX (informational; used for log row).",
    )
    def _log_type(s: str):
        return Path(s) if s else None

    p.add_argument(
        "--log",
        type=_log_type,
        default=Path("ops/define-kpis/lint-runs.log"),
        help="Append-only JSONL log. Pass empty string ('--log=') to disable.",
    )
    p.add_argument(
        "--max-retries",
        type=int,
        default=1,
        help=(
            "Maximum auto-fix retries (informational; cap lives here, not in skill "
            "prose). The skill calls lint once, the auto-fix loop calls it up to "
            "max-retries additional times after a patch."
        ),
    )
    args = p.parse_args(argv)

    if args.description is not None:
        description = args.description
    else:
        description = args.description_file.read_text()

    result = lint(description, args.issue_type)
    _log_run(args.log, args.ticket, args.issue_type, result)

    out: dict
    if result.exit_code == 0:
        out = {
            "pass": True,
            "issue_type": args.issue_type,
            "ticket": args.ticket,
        }
    else:
        out = {
            "gaps": [asdict(g) for g in result.gaps],
            "fixable": result.fixable,
            "issue_type": args.issue_type,
            "ticket": args.ticket,
        }
    print(json.dumps(out))
    return result.exit_code


if __name__ == "__main__":
    sys.exit(main())
