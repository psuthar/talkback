#!/usr/bin/env python3
"""SCRUM-530: FULL_AUTO post-merge close-out orchestrator.

Replaces the ~14 individual tool calls Claude makes today in steps 11b→12
of the FULL_AUTO timeline with a single Python entry point.

  CLI:
    python -m scripts.full_auto.close SCRUM-XX --pr N \\
        [--path polling|webhook|manual-override] \\
        [--dry-run]

  Library:
    from scripts.full_auto.close import close
    result = close("SCRUM-XX", pr_number=N, path_indicator="polling")

Returns a ``CloseResult`` with structured fields and an ``actions_taken``
list useful for both Claude's chat surface and the future webhook listener.

Phase 1 ships the script. **Nothing calls it from CLAUDE.md yet** — Phase 2
validates via ``--dry-run`` and Phase 3 cuts over.
"""

from __future__ import annotations

import argparse
import json
import sys
from dataclasses import dataclass, field
from pathlib import Path

_REPO_ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(_REPO_ROOT / "scripts"))

from full_auto.lib import auth, git_ops, state, templates  # noqa: E402
from full_auto.lib.github import GitHubAPI, HttpGitHubAPI  # noqa: E402
from full_auto.lib.jira import (  # noqa: E402
    HttpJiraAPI,
    JiraAPI,
    resolve_done_transition_id,
)
from full_auto.lib.templates import (  # noqa: E402
    MANUAL_OVERRIDE,
    POLLING,
    VALID_PATHS,
    WEBHOOK,
    ClosureContext,
)

DEFAULT_REPO = "psuthar/talkback"


@dataclass
class CloseResult:
    ticket: str
    pr_number: int
    path_indicator: str
    dry_run: bool
    merged_sha: str | None = None
    main_sha_after: str | None = None
    jira_transitioned: bool = False
    branch_deleted: bool = False
    # SCRUM-538: tri-state replacing the old bool. Values:
    #   "updated"        — state.mark_done modified the file
    #   "already_done"   — file found, but the ticket entry was already done
    #                       (idempotent re-run)
    #   "no_state_file"  — no .epic-run/<EPIC>.json references this ticket
    #                       (standalone, non-epic ticket — the default case)
    #   "would_update"   — dry-run preview: the file would have been written
    # The earlier bool-typed `state_file_updated: bool = False` ambiguously
    # collapsed "no_state_file" and "already_done" into the same `false` value.
    state_file_status: str = "no_state_file"
    closure_comment_id: int | None = None
    actions_taken: list[str] = field(default_factory=list)
    aborted_reason: str | None = None


class _DryRunMarker:
    """Sentinel used by close() to short-circuit mutations in dry-run mode."""


def _act(result: CloseResult, msg: str) -> None:
    result.actions_taken.append(msg)


def close(
    ticket: str,
    pr_number: int,
    *,
    path_indicator: str = POLLING,
    dry_run: bool = False,
    repo: str = DEFAULT_REPO,
    github_api: GitHubAPI | None = None,
    jira_api: JiraAPI | None = None,
    repo_root: Path | None = None,
) -> CloseResult:
    """Run the FULL_AUTO post-merge close-out for ``ticket`` / ``pr_number``.

    Steps:
      1. Pre-merge guard: read PR state.
         - If not merged and not ``clean`` → abort.
         - If already merged (manual-override path): use existing merge SHA.
         - If mergeable + not merged: call ``merge_pr`` (squash).
      2. Local git: fetch + checkout main + pull --ff-only + branch -D.
      3. State file: update entry for ``ticket`` if found.
      4. Jira: transition to Done.
      5. Jira: post closure comment from template.

    In ``dry_run`` mode no remote/local mutations happen; ``actions_taken``
    enumerates the planned actions. Returns ``CloseResult`` with all fields
    populated based on the planned actions.
    """
    if path_indicator not in VALID_PATHS:
        raise ValueError(f"path_indicator must be one of {VALID_PATHS}")

    result = CloseResult(
        ticket=ticket, pr_number=pr_number, path_indicator=path_indicator, dry_run=dry_run
    )
    repo_root = repo_root or Path.cwd()

    # Construct clients lazily so tests inject mocks without ever calling
    # auth.* (which would raise if env vars aren't set).
    if github_api is None:
        github_api = HttpGitHubAPI(auth.github_token())
    if jira_api is None:
        jira_api = HttpJiraAPI(auth.atlassian_base_url(), *auth.jira_auth())

    # Step 1: pre-merge guard / determine merge SHA
    snap = github_api.read_pr(repo, pr_number)
    _act(result, f"read PR #{pr_number}: state={snap.state} merged={snap.merged} "
                 f"mergeable_state={snap.mergeable_state}")

    if snap.merged:
        # Manual-override path — PR is already merged at entry.
        if not snap.merge_commit_sha:
            raise RuntimeError(f"PR #{pr_number} is merged but merge_commit_sha missing")
        result.merged_sha = snap.merge_commit_sha
        _act(result, f"PR already merged at {snap.merge_commit_sha[:7]} (manual override)")
    else:
        # Polling / webhook path — must be clean to merge.
        if snap.mergeable_state != "clean":
            result.aborted_reason = f"mergeable_state was {snap.mergeable_state!r} not 'clean'"
            _act(result, f"aborted: {result.aborted_reason}")
            _summarize(result)
            return result
        if dry_run:
            result.merged_sha = "<dry-run-merge-sha>"
            _act(result, "would call merge_pr (squash) — dry run")
        else:
            sha = github_api.merge_pr(repo, pr_number)
            result.merged_sha = sha
            _act(result, f"merged PR #{pr_number} (squash) at {sha[:7]}")

    # Step 2: local git cleanup
    branch_name = snap.head_ref
    if dry_run:
        result.main_sha_after = "<dry-run-main-sha>"
        result.branch_deleted = True
        _act(result, f"would: fetch + checkout main + pull --ff-only + branch -D {branch_name}")
    else:
        git_ops.fetch_main(cwd=repo_root)
        # SCRUM-534: PR-mode lint at step 4.5 of workflow-jira.md appends a
        # row to ops/define-kpis/lint-runs.log AFTER the feature commit is
        # made. Auto-stash + restore it across the checkout so the audit row
        # survives on main. Any other dirty tracked file falls through to
        # the normal `git checkout` error.
        stashed_lint_log = git_ops.lint_log_only_dirty(cwd=repo_root)
        if stashed_lint_log:
            git_ops.stash_lint_log(cwd=repo_root)
        result.main_sha_after = git_ops.checkout_and_pull_main(cwd=repo_root)
        if stashed_lint_log:
            git_ops.pop_stash(cwd=repo_root)
            _act(
                result,
                "stashed and restored "
                + git_ops.LINT_LOG_PATH
                + " (SCRUM-534: PR-mode lint row preserved on main)",
            )
        git_ops.delete_branch(branch_name, cwd=repo_root)
        result.branch_deleted = True
        _act(result, f"git: main → {result.main_sha_after[:7]}, deleted {branch_name}")

    # Step 3: state file
    state_path = state.find_state_file(ticket, repo_root=repo_root)
    if state_path is not None:
        if dry_run:
            result.state_file_status = "would_update"
            _act(result, f"would update state file: {state_path}")
        else:
            updated = state.mark_done(
                state_path,
                ticket,
                pr_number=pr_number,
                merged_sha=result.merged_sha or "",
                final_gate=(
                    "manual_override" if path_indicator == MANUAL_OVERRIDE else "PASS"
                ),
            )
            result.state_file_status = "updated" if updated else "already_done"
            _act(result, f"state file {'updated' if updated else 'already current'}: {state_path}")
    else:
        result.state_file_status = "no_state_file"
        _act(result, f"no state file references {ticket}; skipping state update")

    # Step 4: Jira → Done
    if dry_run:
        result.jira_transitioned = True
        _act(result, f"would transition {ticket} to Done")
    else:
        transition_id = resolve_done_transition_id(jira_api, ticket)
        jira_api.transition(ticket, transition_id)
        result.jira_transitioned = True
        _act(result, f"transitioned {ticket} to Done (id={transition_id})")

    # Step 5: closure comment
    ctx = ClosureContext(
        ticket=ticket,
        pr_number=pr_number,
        merged_sha=result.merged_sha or "",
        main_sha_after=result.main_sha_after or "",
        final_gate_status=(
            "manual_override" if path_indicator == MANUAL_OVERRIDE else "PASS"
        ),
        branch_name=branch_name,
    )
    comment_body = templates.render(path_indicator, ctx)
    if dry_run:
        _act(result, f"would post closure comment ({len(comment_body)} chars)")
    else:
        result.closure_comment_id = jira_api.add_comment(ticket, comment_body)
        _act(result, f"posted closure comment id={result.closure_comment_id}")

    # SCRUM-536: single grep-able summary line at the end of actions_taken.
    # Count preceding entries before appending so N reflects the work done,
    # not the line itself.
    _summarize(result)
    return result


def _summarize(result: CloseResult) -> None:
    """SCRUM-536: append a one-line summary as the final actions_taken entry.

    Three shapes:
      - PASS (no abort, no dry-run): "close.py succeeded: N actions, no aborts"
      - dry-run (no abort): "close.py dry-run: N actions previewed"
      - abort: "close.py aborted: <reason>"

    The early-return abort path (pre-merge guard, mergeable_state != clean)
    also routes here via the explicit ``_summarize`` call it now makes
    before returning.
    """
    n = len(result.actions_taken)
    if result.aborted_reason:
        msg = f"close.py aborted: {result.aborted_reason}"
    elif result.dry_run:
        msg = f"close.py dry-run: {n} actions previewed"
    else:
        msg = f"close.py succeeded: {n} actions, no aborts"
    result.actions_taken.append(msg)


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("ticket", help="Jira ticket key, e.g. SCRUM-530")
    parser.add_argument("--pr", type=int, required=True)
    parser.add_argument("--path", default=POLLING, choices=VALID_PATHS)
    parser.add_argument("--repo", default=DEFAULT_REPO)
    parser.add_argument("--dry-run", action="store_true")
    args = parser.parse_args(argv)

    result = close(
        args.ticket,
        pr_number=args.pr,
        path_indicator=args.path,
        dry_run=args.dry_run,
        repo=args.repo,
    )
    summary = {
        "ticket": result.ticket,
        "pr_number": result.pr_number,
        "path_indicator": result.path_indicator,
        "dry_run": result.dry_run,
        "merged_sha": result.merged_sha,
        "main_sha_after": result.main_sha_after,
        "jira_transitioned": result.jira_transitioned,
        "branch_deleted": result.branch_deleted,
        "state_file_status": result.state_file_status,
        "closure_comment_id": result.closure_comment_id,
        "aborted_reason": result.aborted_reason,
        "actions_taken": result.actions_taken,
    }
    print(json.dumps(summary, indent=2))
    return 1 if result.aborted_reason else 0


if __name__ == "__main__":
    sys.exit(main())


__all__ = [
    "CloseResult",
    "DEFAULT_REPO",
    "MANUAL_OVERRIDE",
    "POLLING",
    "VALID_PATHS",
    "WEBHOOK",
    "close",
]
