#!/usr/bin/env python3
"""SCRUM-543: post-implementation orchestration for `implement SCRUM-XX FULL_AUTO`.

Companion of ``close.py`` and ``start.py`` covering the steps between
"tests pass + push" and "begin polling": open the PR, lint the PR
body, post the Jira completion comment, transition the ticket to In
Review. Today's MCP flow costs ~4000 tokens of echo per ticket
(``create_pull_request`` response + ``add_comment`` body echo + the
transitions list echoed twice when both start.py and review.py have
to fetch it). This script collapses everything into one REST call
each.

The agent owns the **content** (PR title, PR body, completion-comment
text). The script passes them through; it never composes prose. Same
constraint ``close.py`` respects with its closure templates.

Steps:

1. **Branch-mismatch guard** — ``git rev-parse --abbrev-ref HEAD`` must
   match ``feat/<TICKET>`` (prefix match tolerates worktree branches
   like ``feat/<TICKET>-worktree``). Aborts if not.
2. **Create PR** via REST POST ``/repos/<repo>/pulls`` from
   ``feat/<TICKET>`` into ``main``.
3. **PR-body lint** — write the body to a temp file, invoke
   ``jira_ticket_lint.py --issue-type PR``. On agent-authored exit-2,
   apply the same section-patch loop ``start.py`` uses (reused via
   :mod:`scripts.full_auto.start`) + PATCH the PR body via REST, then
   re-lint once. On second exit-2 or any exit-1, halt.
4. **Post completion comment** via REST POST
   ``/issue/<key>/comment``. Body is passed-through text; Atlassian
   converts to ADF on receipt.
5. **Transition to In Review** via ``resolve_transition_id_by_name``.

Returns a :class:`ReviewResult` dataclass; ``main()`` prints JSON.
"""

from __future__ import annotations

import argparse
import json
import subprocess
import sys
import tempfile
from dataclasses import asdict, dataclass, field
from pathlib import Path

from .lib import auth
from .lib.github import GitHubAPI, HttpGitHubAPI
from .lib.jira import HttpJiraAPI, JiraAPI, resolve_transition_id_by_name
from .start import (
    AGENT_AUTHORED_LABEL,
    LINT_SCRIPT,
    LINT_STATUS_HALTED_GAPS,
    LINT_STATUS_HALTED_UNFIXABLE,
    LINT_STATUS_PASS,
    LINT_STATUS_PATCHED_THEN_PASS,
    _patch_description,
)

DEFAULT_REPO = "psuthar/talkback"
DEFAULT_BASE = "main"
IN_REVIEW_TRANSITION_NAME = "In Review"


@dataclass
class ReviewResult:
    ticket: str
    dry_run: bool
    pr_number: int | None = None
    pr_url: str | None = None
    pr_body_lint_status: str = LINT_STATUS_PASS
    pr_body_lint_gaps: list[dict] = field(default_factory=list)
    comment_id: int | None = None
    jira_transitioned: bool = False
    actions_taken: list[str] = field(default_factory=list)
    aborted_reason: str | None = None


def _act(result: ReviewResult, msg: str) -> None:
    result.actions_taken.append(msg)


def _summarize(result: ReviewResult) -> None:
    n = len(result.actions_taken)
    if result.aborted_reason:
        msg = f"review.py aborted: {result.aborted_reason}"
    elif result.dry_run:
        msg = f"review.py dry-run: {n} actions previewed"
    else:
        msg = f"review.py succeeded: {n} actions, no aborts"
    result.actions_taken.append(msg)


def _current_branch(cwd: Path) -> str:
    return subprocess.run(
        ["git", "rev-parse", "--abbrev-ref", "HEAD"],
        cwd=str(cwd),
        capture_output=True,
        text=True,
        check=True,
    ).stdout.strip()


def _run_pr_body_lint(body: str, ticket: str) -> tuple[int, dict]:
    with tempfile.NamedTemporaryFile(
        mode="w", suffix=f"-{ticket}-pr.md", delete=False
    ) as f:
        f.write(body)
        tmp = f.name
    try:
        proc = subprocess.run(
            [
                sys.executable,
                str(LINT_SCRIPT),
                "--description-file",
                tmp,
                "--issue-type",
                "PR",
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
            Path(tmp).unlink()
        except OSError:
            pass


def review(
    ticket: str,
    *,
    title: str,
    pr_body: str,
    completion_comment: str,
    dry_run: bool = False,
    repo: str = DEFAULT_REPO,
    base: str = DEFAULT_BASE,
    github_api: GitHubAPI | None = None,
    jira_api: JiraAPI | None = None,
    repo_root: Path | None = None,
    ticket_labels: list[str] | None = None,
) -> ReviewResult:
    """SCRUM-543: open PR + lint body + post Jira comment + transition.

    ``ticket_labels`` lets the caller skip a second ``get_issue`` round-trip
    when the labels are already known (e.g. start.py just fetched them).
    When ``None``, the script re-fetches via ``jira_api.get_issue``.
    """
    repo_root = repo_root or Path.cwd()
    if github_api is None:
        github_api = HttpGitHubAPI(auth.github_token())
    if jira_api is None:
        email, token = auth.jira_auth()
        jira_api = HttpJiraAPI(auth.atlassian_base_url(), email, token)

    result = ReviewResult(ticket=ticket, dry_run=dry_run)

    # Step 1: branch-mismatch guard
    expected_prefix = f"feat/{ticket}"
    current = _current_branch(repo_root)
    if not current.startswith(expected_prefix):
        result.aborted_reason = (
            f"current branch {current!r} does not start with {expected_prefix!r}"
        )
        _act(result, f"aborted: {result.aborted_reason}")
        _summarize(result)
        return result
    _act(result, f"branch guard ok: on {current}")

    # Step 2: create PR
    if dry_run:
        result.pr_number = 0
        result.pr_url = "<dry-run>"
        _act(result, f"would create PR ({len(pr_body)} char body) → {repo}:{base}")
    else:
        pr_number, pr_url = github_api.create_pr(
            repo, title=title, head=current, base=base, body=pr_body
        )
        result.pr_number = pr_number
        result.pr_url = pr_url
        _act(result, f"opened PR #{pr_number}: {pr_url}")

    # Step 3: PR-body lint + auto-fix
    if ticket_labels is None:
        ticket_labels = jira_api.get_issue(ticket).get("labels", [])
    exit_code, lint_out = _run_pr_body_lint(pr_body, ticket)
    if exit_code == 0:
        result.pr_body_lint_status = LINT_STATUS_PASS
        _act(result, "PR-body lint pass")
    elif exit_code == 1:
        result.pr_body_lint_status = LINT_STATUS_HALTED_UNFIXABLE
        result.pr_body_lint_gaps = lint_out.get("gaps", [])
        result.aborted_reason = f"PR-body lint exit 1 (unfixable): {result.pr_body_lint_gaps}"
        _act(result, f"aborted: {result.aborted_reason}")
        _summarize(result)
        return result
    elif exit_code == 2:
        gaps = lint_out.get("gaps", [])
        if AGENT_AUTHORED_LABEL not in ticket_labels:
            result.pr_body_lint_status = LINT_STATUS_HALTED_GAPS
            result.pr_body_lint_gaps = gaps
            result.aborted_reason = (
                f"PR-body lint exit 2 with gaps {[g.get('rule_id') for g in gaps]} "
                f"(linked Jira ticket not agent-authored)"
            )
            _act(result, f"aborted: {result.aborted_reason}")
            _summarize(result)
            return result
        patched = _patch_description(pr_body, gaps)
        if dry_run:
            _act(result, "would patch PR body and re-lint")
        else:
            github_api.update_pr_body(repo, result.pr_number or 0, patched)
            _act(
                result,
                f"patched PR body (rule_ids={[g.get('rule_id') for g in gaps]})",
            )
        exit_code, lint_out = _run_pr_body_lint(patched, ticket)
        if exit_code == 0:
            result.pr_body_lint_status = LINT_STATUS_PATCHED_THEN_PASS
            _act(result, "PR-body lint pass after patch")
        else:
            result.pr_body_lint_status = LINT_STATUS_HALTED_GAPS
            result.pr_body_lint_gaps = lint_out.get("gaps", [])
            result.aborted_reason = f"PR-body lint exit {exit_code} after patch retry"
            _act(result, f"aborted: {result.aborted_reason}")
            _summarize(result)
            return result

    # Step 4: post completion comment
    if dry_run:
        _act(result, f"would post completion comment ({len(completion_comment)} chars)")
    else:
        result.comment_id = jira_api.add_comment(ticket, completion_comment)
        _act(result, f"posted completion comment id={result.comment_id}")

    # Step 5: transition to In Review
    if dry_run:
        _act(result, f"would transition {ticket} to {IN_REVIEW_TRANSITION_NAME}")
    else:
        tid = resolve_transition_id_by_name(jira_api, ticket, IN_REVIEW_TRANSITION_NAME)
        jira_api.transition(ticket, tid)
        result.jira_transitioned = True
        _act(result, f"transitioned {ticket} to {IN_REVIEW_TRANSITION_NAME} (id={tid})")

    _summarize(result)
    return result


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("ticket", help="Jira ticket key, e.g. SCRUM-543")
    parser.add_argument("--title", required=True, help="PR title")
    parser.add_argument(
        "--body-file",
        required=True,
        help="Path to a file containing the PR body (Markdown).",
    )
    parser.add_argument(
        "--completion-comment-file",
        required=True,
        help="Path to a file containing the Jira completion-comment text.",
    )
    parser.add_argument("--repo", default=DEFAULT_REPO)
    parser.add_argument("--base", default=DEFAULT_BASE)
    parser.add_argument("--dry-run", action="store_true")
    args = parser.parse_args(argv)

    pr_body = Path(args.body_file).read_text()
    comment = Path(args.completion_comment_file).read_text()

    result = review(
        args.ticket,
        title=args.title,
        pr_body=pr_body,
        completion_comment=comment,
        dry_run=args.dry_run,
        repo=args.repo,
        base=args.base,
    )
    print(json.dumps(asdict(result), indent=2))
    return 1 if result.aborted_reason else 0


if __name__ == "__main__":
    sys.exit(main())
