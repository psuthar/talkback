#!/usr/bin/env python3
"""SCRUM-530: closure-comment templates for the three FULL_AUTO close-out paths.

The three path indicators match the closure comments Claude has been posting
verbatim throughout the session — see SCRUM-507, SCRUM-509, SCRUM-526 for
``polling``; SCRUM-510, SCRUM-514, SCRUM-515 for ``manual-override``; the
deployed claude.ai routine path for ``webhook``.

Tests assert that each template produces the same shape Claude has been
posting so closure comments don't drift across the cut-over.
"""

from __future__ import annotations

from dataclasses import dataclass

POLLING = "polling"
WEBHOOK = "webhook"
MANUAL_OVERRIDE = "manual-override"

VALID_PATHS = (POLLING, WEBHOOK, MANUAL_OVERRIDE)


@dataclass
class ClosureContext:
    ticket: str
    pr_number: int
    merged_sha: str
    main_sha_after: str
    final_gate_status: str  # "PASS" | "manual_override" | "webhook"
    branch_name: str


def render(path: str, ctx: ClosureContext) -> str:
    """Render the closure comment for the given path indicator and context."""
    if path not in VALID_PATHS:
        raise ValueError(f"unknown path indicator: {path!r}; want one of {VALID_PATHS}")
    if path == POLLING:
        return _polling(ctx)
    if path == MANUAL_OVERRIDE:
        return _manual_override(ctx)
    return _webhook(ctx)


def _polling(ctx: ClosureContext) -> str:
    return (
        f"FULL_AUTO complete — polling path (default). "
        f"PR #{ctx.pr_number} squash-merged at {ctx.merged_sha}. "
        f"Final Gate PASS (TalkBack PR Gate + release-readiness), mergeable_state "
        f"clean (verified via pre-merge guard re-read immediately before "
        f"merge_pull_request call).\n\n"
        f"Local cleanup done:\n"
        f"- git checkout main, git pull --ff-only origin main "
        f"(main now at {ctx.main_sha_after})\n"
        f"- git branch -D {ctx.branch_name}\n"
    )


def _manual_override(ctx: ClosureContext) -> str:
    return (
        f"Closure (user-override squash-merge path). "
        f"PR #{ctx.pr_number} squash-merged by user at {ctx.merged_sha}. "
        f"Did NOT call merge_pull_request (the user merged). "
        f"Local cleanup: {ctx.branch_name} deleted; main fast-forwarded to "
        f"{ctx.main_sha_after}. State file updated. "
        f"Treated as PASS-equivalent for the purpose of the epic.\n"
    )


def _webhook(ctx: ClosureContext) -> str:
    return (
        f"FULL_AUTO complete — webhook path (FULL_AUTO_WEBHOOK). "
        f"PR #{ctx.pr_number} squash-merged at {ctx.merged_sha}. "
        f"Final Gate PASS via deployed routine; mergeable clean. "
        f"Local cleanup skipped (no local checkout available in webhook context); "
        f"maintainer to fast-forward main + delete {ctx.branch_name} locally.\n"
    )


__all__ = [
    "ClosureContext",
    "MANUAL_OVERRIDE",
    "POLLING",
    "VALID_PATHS",
    "WEBHOOK",
    "render",
]
