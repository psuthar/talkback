#!/usr/bin/env python3
"""SCRUM-530: local-git helpers for close.py.

Thin subprocess wrappers. Tests mock ``subprocess.run`` directly.
"""

from __future__ import annotations

import subprocess
from pathlib import Path

# SCRUM-534: path the PR-body lint (step 4.5 of workflow-jira.md) appends to
# after the feature commit. close.py auto-stashes this file around the main
# checkout when it is the only dirty tracked file, so the row survives to be
# committed on a future PR. Keep in sync with the default ``--log`` argument
# in ``scripts/jira_ticket_lint.py``.
LINT_LOG_PATH = "ops/define-kpis/lint-runs.log"


def _run(args: list[str], cwd: Path | None = None) -> str:
    result = subprocess.run(
        args,
        cwd=str(cwd) if cwd else None,
        capture_output=True,
        text=True,
        check=False,
    )
    if result.returncode != 0:
        raise RuntimeError(
            f"git {' '.join(args[1:])} failed (exit {result.returncode}): {result.stderr.strip()}"
        )
    return result.stdout.strip()


def fetch_main(cwd: Path | None = None) -> None:
    _run(["git", "fetch", "origin", "--prune"], cwd=cwd)


def lint_log_only_dirty(cwd: Path | None = None) -> bool:
    """SCRUM-534: True iff ``ops/define-kpis/lint-runs.log`` is the only
    tracked file with uncommitted changes.

    Untracked files are ignored — ``git checkout`` doesn't care about them.
    If any *other* tracked path is dirty, returns False so the caller falls
    through to the normal ``git checkout`` failure path.
    """
    # Don't reuse ``_run`` here: it ``strip()``s stdout, which would eat the
    # leading space that ``git status --porcelain=v1`` puts before the
    # working-tree status code (e.g. " M path"). We need byte-accurate
    # output to parse the XY positional codes.
    result = subprocess.run(
        ["git", "status", "--porcelain=v1"],
        cwd=str(cwd) if cwd else None,
        capture_output=True,
        text=True,
        check=False,
    )
    if result.returncode != 0:
        raise RuntimeError(
            f"git status --porcelain=v1 failed (exit {result.returncode}): {result.stderr.strip()}"
        )
    log_dirty = False
    for line in result.stdout.splitlines():
        if not line:
            continue
        if line.startswith("??"):
            continue
        # porcelain v1 format: ``XY <path>`` — two status chars + space.
        path = line[3:].rstrip()
        if path == LINT_LOG_PATH:
            log_dirty = True
        else:
            return False
    return log_dirty


def stash_lint_log(cwd: Path | None = None) -> None:
    """SCRUM-534: stash the lint log path only.

    Caller MUST have already confirmed via :func:`lint_log_only_dirty` that
    the lint log is the sole dirty tracked file; passing a pathspec to
    ``git stash push`` with other dirty files leaves them unstashed but
    still blocks the subsequent checkout.
    """
    _run(
        [
            "git",
            "stash",
            "push",
            "-m",
            "close.py: lint-runs.log (SCRUM-534)",
            "--",
            LINT_LOG_PATH,
        ],
        cwd=cwd,
    )


def pop_stash(cwd: Path | None = None) -> None:
    """SCRUM-534: pop the most recent stash entry.

    Errors propagate — a failed pop means manual reconciliation is needed.
    """
    _run(["git", "stash", "pop"], cwd=cwd)


def checkout_and_pull_main(cwd: Path | None = None) -> str:
    """Checkout main and fast-forward. Returns the new HEAD SHA."""
    _run(["git", "checkout", "main"], cwd=cwd)
    _run(["git", "pull", "--ff-only", "origin", "main"], cwd=cwd)
    return _run(["git", "rev-parse", "HEAD"], cwd=cwd)


def delete_branch(name: str, cwd: Path | None = None) -> None:
    """Delete a local branch if it exists. No-op if the branch is missing."""
    show = subprocess.run(
        ["git", "show-ref", "--verify", f"refs/heads/{name}"],
        cwd=str(cwd) if cwd else None,
        capture_output=True,
        text=True,
        check=False,
    )
    if show.returncode != 0:
        return  # branch doesn't exist locally; nothing to do
    _run(["git", "branch", "-D", name], cwd=cwd)


__all__ = [
    "LINT_LOG_PATH",
    "checkout_and_pull_main",
    "delete_branch",
    "fetch_main",
    "lint_log_only_dirty",
    "pop_stash",
    "stash_lint_log",
]
