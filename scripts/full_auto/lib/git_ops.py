#!/usr/bin/env python3
"""SCRUM-530: local-git helpers for close.py.

Thin subprocess wrappers. Tests mock ``subprocess.run`` directly.
"""

from __future__ import annotations

import subprocess
from pathlib import Path


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


__all__ = ["checkout_and_pull_main", "delete_branch", "fetch_main"]
