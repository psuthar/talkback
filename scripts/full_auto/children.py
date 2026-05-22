#!/usr/bin/env python3
"""SCRUM-551: lean-JSON wrapper around ``jira_search_issues``.

The MCP ``jira_search_issues`` returns each issue with full status +
statusCategory + iconUrl + issuetype + avatar payloads — ~800-1500
tokens per response on a small result set. This script issues the
same REST call and projects to the lean shape the agent actually uses:
``{key, summary, status, issuetype, priority, labels}`` strings only.

Common shape used by the ``epic-run`` skill on every drain:

    parent = SCRUM-XXX AND statusCategory != Done ORDER BY created ASC

is the default ``--epic SCRUM-XXX`` preset. Generic ``--jql`` is an
escape hatch with a soft validation guard (must reference the SCRUM
project / a SCRUM- parent) to prevent accidentally querying outside
the project.
"""

from __future__ import annotations

import argparse
import json
import re
import sys
from dataclasses import asdict, dataclass, field
from typing import Optional

from .lib import auth
from .lib.jira import HttpJiraAPI, JiraAPI

DEFAULT_MAX_RESULTS = 50

# Soft guard: a free-form JQL must mention either the SCRUM project or a
# SCRUM-N parent. Prevents an accidental "search everything" call.
_JQL_GUARD_RE = re.compile(
    r"\b(?:project\s*=\s*SCRUM|parent\s*=\s*SCRUM-\d+)\b",
    re.IGNORECASE,
)


@dataclass
class ChildrenResult:
    jql_used: str
    count: int = 0
    children: list[dict] = field(default_factory=list)
    actions_taken: list[str] = field(default_factory=list)
    aborted_reason: str | None = None


def _act(result: ChildrenResult, msg: str) -> None:
    result.actions_taken.append(msg)


def _summarize(result: ChildrenResult) -> None:
    n = len(result.actions_taken)
    if result.aborted_reason:
        msg = f"children.py aborted: {result.aborted_reason}"
    else:
        msg = f"children.py succeeded: {n} actions, no aborts"
    result.actions_taken.append(msg)


def _build_epic_jql(epic: str, include_done: bool) -> str:
    base = f"parent = {epic}"
    if not include_done:
        base += " AND statusCategory != Done"
    return f"{base} ORDER BY created ASC"


def _project(issue: dict) -> dict:
    """Lean projection — drop avatars, statusCategory objects, iconUrls."""
    fields = issue.get("fields", {}) or {}
    return {
        "key": issue.get("key", ""),
        "summary": fields.get("summary", ""),
        "status": (fields.get("status") or {}).get("name", ""),
        "issuetype": (fields.get("issuetype") or {}).get("name", ""),
        "priority": (fields.get("priority") or {}).get("name", ""),
        "labels": fields.get("labels", []) or [],
    }


def children(
    *,
    epic: Optional[str] = None,
    jql: Optional[str] = None,
    include_done: bool = False,
    max_results: int = DEFAULT_MAX_RESULTS,
    jira_api: JiraAPI | None = None,
) -> ChildrenResult:
    """SCRUM-551: query Jira for children of ``epic`` or arbitrary ``jql``.

    Exactly one of ``epic`` / ``jql`` must be set. ``include_done`` only
    applies to the ``epic`` preset (the ``jql`` passthrough doesn't add
    or remove filters).
    """
    if (epic is None) == (jql is None):
        result = ChildrenResult(jql_used="")
        result.aborted_reason = "exactly one of --epic / --jql is required"
        _act(result, f"aborted: {result.aborted_reason}")
        _summarize(result)
        return result

    if epic is not None:
        jql_built = _build_epic_jql(epic, include_done)
    else:
        jql_built = jql or ""
        if not _JQL_GUARD_RE.search(jql_built):
            result = ChildrenResult(jql_used=jql_built)
            result.aborted_reason = (
                "--jql must reference the SCRUM project ('project = SCRUM') "
                "or a SCRUM-N parent ('parent = SCRUM-XXX'); refusing to run "
                "a query that could reach outside the project."
            )
            _act(result, f"aborted: {result.aborted_reason}")
            _summarize(result)
            return result

    result = ChildrenResult(jql_used=jql_built)

    if jira_api is None:
        email, token = auth.jira_auth()
        jira_api = HttpJiraAPI(auth.atlassian_base_url(), email, token)

    raw = jira_api.search_issues(jql_built, max_results=max_results)
    result.children = [_project(i) for i in raw]
    result.count = len(result.children)
    _act(result, f"queried {result.count} issues: {jql_built!r}")
    _summarize(result)
    return result


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    src = parser.add_mutually_exclusive_group(required=True)
    src.add_argument("--epic", help="Epic key, e.g. SCRUM-549 — preset shape")
    src.add_argument("--jql", help="Arbitrary JQL passthrough (must reference SCRUM)")
    parser.add_argument(
        "--include-done",
        action="store_true",
        help="With --epic, include Done children (default is non-Done only).",
    )
    parser.add_argument(
        "--max-results", type=int, default=DEFAULT_MAX_RESULTS
    )
    args = parser.parse_args(argv)

    result = children(
        epic=args.epic,
        jql=args.jql,
        include_done=args.include_done,
        max_results=args.max_results,
    )
    print(json.dumps(asdict(result), indent=2))
    return 1 if result.aborted_reason else 0


if __name__ == "__main__":
    sys.exit(main())
