#!/usr/bin/env python3
"""SCRUM-550: post a Jira comment via REST, replacing ``mcp__atlassian__jira_add_comment``.

The ``jira_add_comment`` MCP echoes back the entire ADF body plus the
author + updateAuthor objects (each carrying 8 avatar URLs at 16/24/32/48
sizes) on every call — ~1500-2500 tokens of pure response noise. Routing
the same operation through REST and returning only the comment id costs
~200 tokens instead.

Use cases (non-completion-comment paths the existing ``review.py``
doesn't cover):

* Epic halt comments during a multi-PR drain.
* Epic-summary / Finish-step comments.
* Manual audit notes (e.g. override explanations).
* Ad-hoc one-offs.

Agent owns the comment content — the script reads it from a file and
passes it through verbatim. Atlassian converts Markdown → ADF on
receipt; the script does not.
"""

from __future__ import annotations

import argparse
import json
import sys
from dataclasses import asdict, dataclass, field
from pathlib import Path

from .lib import auth
from .lib.jira import HttpJiraAPI, JiraAPI


@dataclass
class CommentResult:
    ticket: str
    dry_run: bool
    body_chars: int = 0
    comment_id: int | None = None
    actions_taken: list[str] = field(default_factory=list)
    aborted_reason: str | None = None


def _act(result: CommentResult, msg: str) -> None:
    result.actions_taken.append(msg)


def _summarize(result: CommentResult) -> None:
    n = len(result.actions_taken)
    if result.aborted_reason:
        msg = f"comment.py aborted: {result.aborted_reason}"
    elif result.dry_run:
        msg = f"comment.py dry-run: {n} actions previewed"
    else:
        msg = f"comment.py succeeded: {n} actions, no aborts"
    result.actions_taken.append(msg)


def comment(
    ticket: str,
    *,
    body: str,
    dry_run: bool = False,
    jira_api: JiraAPI | None = None,
) -> CommentResult:
    """Post ``body`` as a comment on ``ticket`` via REST.

    ``jira_api`` is injectable for tests (mirrors close.py / start.py /
    review.py). Defaults to ``HttpJiraAPI`` configured from
    :func:`lib.auth.jira_auth`.
    """
    if jira_api is None:
        email, token = auth.jira_auth()
        jira_api = HttpJiraAPI(auth.atlassian_base_url(), email, token)

    result = CommentResult(ticket=ticket, dry_run=dry_run, body_chars=len(body))

    if not body.strip():
        result.aborted_reason = "comment body is empty or whitespace-only"
        _act(result, f"aborted: {result.aborted_reason}")
        _summarize(result)
        return result

    if dry_run:
        _act(result, f"would post comment ({len(body)} chars) on {ticket}")
    else:
        result.comment_id = jira_api.add_comment(ticket, body)
        _act(result, f"posted comment id={result.comment_id} ({len(body)} chars)")

    _summarize(result)
    return result


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("ticket", help="Jira ticket key, e.g. SCRUM-550")
    parser.add_argument(
        "--body-file",
        required=True,
        help="Path to a file containing the comment body (Markdown).",
    )
    parser.add_argument("--dry-run", action="store_true")
    args = parser.parse_args(argv)

    body = Path(args.body_file).read_text()
    result = comment(args.ticket, body=body, dry_run=args.dry_run)
    print(json.dumps(asdict(result), indent=2))
    return 1 if result.aborted_reason else 0


if __name__ == "__main__":
    sys.exit(main())
