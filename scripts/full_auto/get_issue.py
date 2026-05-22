#!/usr/bin/env python3
"""SCRUM-552: read a Jira ticket via REST, replacing ``mcp__atlassian__jira_get_issue``.

The MCP echo on ``jira_get_issue`` is ~800-1500 tokens (full ADF
description + status + statusCategory + iconUrls + avatars). This
script reuses ``lib/jira.py::HttpJiraAPI.get_issue`` (added in
SCRUM-542) which already returns a lean dict: ``summary``, ``labels``,
``issuetype``, ``status`` — plus the raw ADF ``description``.

The description is dropped from default output (large + ADF-shaped);
``--description`` includes it converted to Markdown via
:mod:`scripts.full_auto.lib.adf`. ``--field`` restricts output to one
or more named fields when the agent only wants a subset (e.g. just
the status during a poll-shaped investigation).

Use cases (the ad-hoc reads outside the implement-flow that start.py
handles):

* "What's the status of SCRUM-XXX" — single read, no implementation.
* "What's the parent epic" — read summary + status before deciding.
* "Let me check the description before filing a follow-up."
"""

from __future__ import annotations

import argparse
import json
import sys
from dataclasses import asdict, dataclass, field
from typing import Optional

from .lib import auth
from .lib.adf import adf_to_md
from .lib.jira import HttpJiraAPI, JiraAPI


@dataclass
class GetIssueResult:
    ticket: str
    summary: str = ""
    issuetype: str = ""
    status: str = ""
    labels: list[str] = field(default_factory=list)
    description_md: str | None = None
    actions_taken: list[str] = field(default_factory=list)
    aborted_reason: str | None = None


def _act(result: GetIssueResult, msg: str) -> None:
    result.actions_taken.append(msg)


def _summarize(result: GetIssueResult) -> None:
    n = len(result.actions_taken)
    if result.aborted_reason:
        msg = f"get_issue.py aborted: {result.aborted_reason}"
    else:
        msg = f"get_issue.py succeeded: {n} actions, no aborts"
    result.actions_taken.append(msg)


def get_issue(
    ticket: str,
    *,
    include_description: bool = False,
    jira_api: JiraAPI | None = None,
) -> GetIssueResult:
    """SCRUM-552: fetch a Jira ticket and project to the lean output shape.

    ``jira_api`` is injectable for tests. Defaults to ``HttpJiraAPI``
    configured from :func:`lib.auth.jira_auth`.
    """
    if jira_api is None:
        email, token = auth.jira_auth()
        jira_api = HttpJiraAPI(auth.atlassian_base_url(), email, token)

    result = GetIssueResult(ticket=ticket)
    try:
        issue = jira_api.get_issue(ticket)
    except RuntimeError as e:
        result.aborted_reason = str(e)
        _act(result, f"aborted: {result.aborted_reason}")
        _summarize(result)
        return result

    result.summary = issue.get("summary", "")
    result.issuetype = issue.get("issuetype", "")
    result.status = issue.get("status", "")
    result.labels = issue.get("labels", []) or []
    if include_description:
        result.description_md = adf_to_md(issue.get("description")).rstrip() + "\n"

    _act(
        result,
        f"fetched {ticket}: status={result.status} type={result.issuetype}",
    )
    _summarize(result)
    return result


def _project_to_fields(result: GetIssueResult, fields: Optional[list[str]]) -> dict:
    """If ``fields`` is provided, restrict the output dict to those keys.
    Always includes ``ticket`` + ``actions_taken`` for traceability."""
    full = asdict(result)
    if not fields:
        return full
    restricted = {"ticket": full["ticket"], "actions_taken": full["actions_taken"]}
    if full.get("aborted_reason") is not None:
        restricted["aborted_reason"] = full["aborted_reason"]
    for name in fields:
        if name in full:
            restricted[name] = full[name]
    return restricted


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("ticket", help="Jira ticket key, e.g. SCRUM-552")
    parser.add_argument(
        "--description",
        action="store_true",
        help="Include the description (ADF → Markdown). Dropped by default to keep output small.",
    )
    parser.add_argument(
        "--field",
        action="append",
        dest="fields",
        help="Restrict output to one named field. Repeatable; default: all lean fields.",
    )
    args = parser.parse_args(argv)

    result = get_issue(args.ticket, include_description=args.description)
    print(json.dumps(_project_to_fields(result, args.fields), indent=2))
    return 1 if result.aborted_reason else 0


if __name__ == "__main__":
    sys.exit(main())
