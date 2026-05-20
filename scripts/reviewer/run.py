#!/usr/bin/env python3
"""SCRUM-514: reviewer orchestration module.

Called from the Phase 1c workflow with ``(pr_number, repo, source)``.
Fetches the PR diff, estimates tokens, gates on the daily budget, renders
the prompt against PROMPT.md, invokes the model, posts the review.

All I/O goes through injectable seams (``github_client``, ``model_client``,
``store``) so unit tests never touch the network.

Pin enforcement: PROMPT.md carries a ``SCOPE.md@<sha>`` line at the top.
Before running, we resolve the current ``SCOPE.md`` HEAD SHA in the repo
and compare. A mismatch raises ``StalePromptPinError`` — the workflow
fails fast rather than running a prompt against a stale policy.
"""

from __future__ import annotations

import os
import re
import subprocess
from dataclasses import dataclass
from pathlib import Path
from typing import Callable, Protocol

from reviewer.budget import (
    BUDGET_EXHAUSTED_COMMENT,
    BudgetStore,
    check_and_increment,
)

_REPO_ROOT = Path(__file__).resolve().parents[2]
PROMPT_PATH = _REPO_ROOT / ".github" / "talkback-reviewer" / "PROMPT.md"
SCOPE_PATH = _REPO_ROOT / ".github" / "talkback-reviewer" / "SCOPE.md"

DEFAULT_MAX_INPUT_TOKENS = 100_000
DEFAULT_MODEL = "claude-sonnet-4-6"

REFUSAL_TOKEN = "<reviewer-skip-no-content>"
TRUNCATION_NOTE = "\n\n[diff truncated by talkback-reviewer to stay under budget]"

_PIN_RE = re.compile(r"SCOPE\.md@([0-9a-f]{7,40})")


class StalePromptPinError(RuntimeError):
    """PROMPT.md's SCOPE.md pin does not match the current SCOPE.md HEAD."""


@dataclass
class PRContent:
    number: int
    title: str
    description: str
    diff: str
    changed_files: list[str]
    head_sha: str


@dataclass
class ReviewResult:
    posted: bool
    skipped: bool
    reason: str
    used_tokens: int = 0
    model: str = ""
    comment_id: int | None = None


class GitHubClient(Protocol):
    def fetch_pr(self, repo: str, pr_number: int) -> PRContent: ...
    def post_pr_comment(self, repo: str, pr_number: int, body: str) -> int: ...


ModelClient = Callable[[str, str, str], str]
"""(model, system_prompt, user_prompt) -> generated text."""


def _resolve_scope_sha(repo_root: Path = _REPO_ROOT) -> str:
    """Resolve current SCOPE.md commit SHA via git. Returns first 7 chars
    of the most recent commit that touched SCOPE.md.
    """
    result = subprocess.run(
        ["git", "log", "-1", "--format=%h", "--", str(SCOPE_PATH.relative_to(repo_root))],
        cwd=str(repo_root),
        capture_output=True,
        text=True,
        check=True,
    )
    return result.stdout.strip()


def load_prompt(prompt_path: Path = PROMPT_PATH, *, enforce_pin: bool = True) -> tuple[str, str]:
    """Read PROMPT.md and return ``(content, scope_sha_pin)``.

    Raises ``StalePromptPinError`` if ``enforce_pin`` is True and the
    pinned SCOPE.md SHA does not match the repo's current SCOPE.md HEAD.
    """
    content = prompt_path.read_text()
    match = _PIN_RE.search(content)
    if not match:
        raise StalePromptPinError(
            f"PROMPT.md at {prompt_path} is missing the required `SCOPE.md@<sha>` pin"
        )
    pinned_sha = match.group(1)
    if enforce_pin:
        current_sha = _resolve_scope_sha()
        if not current_sha.startswith(pinned_sha) and not pinned_sha.startswith(current_sha):
            raise StalePromptPinError(
                f"PROMPT.md pinned to SCOPE.md@{pinned_sha} but current SCOPE.md HEAD is "
                f"{current_sha}. Update PROMPT.md's pin in the same PR as the SCOPE.md change."
            )
    return content, pinned_sha


def estimate_tokens(diff: str, prompt_template: str) -> int:
    """Cheap heuristic: ~3 chars/token plus a fixed prompt overhead.

    Accuracy matters less than always-checking. The point is bounding
    cost via the daily budget; a 30% over-estimate just makes the cap
    more conservative.
    """
    return (len(diff) + len(prompt_template)) // 3 + 500


def _split_prompt(template: str) -> tuple[str, str]:
    """Split PROMPT.md into system + user halves at the Variables section.

    Sections up to and including "## Refusal" form the system prompt
    (instructions). Variable substitutions form the user message.
    """
    lower = template.lower()
    cut = lower.find("\n## variables")
    if cut == -1:
        return template, ""
    return template[:cut].rstrip(), template[cut:].lstrip()


def _render_user_message(pr: PRContent, scope_md: str, diff: str) -> str:
    return (
        f"PR title: {pr.title}\n\n"
        f"PR description (read-only context):\n{pr.description or '(none)'}\n\n"
        f"Changed files:\n" + "\n".join(pr.changed_files) + "\n\n"
        f"SCOPE.md (the policy boundary):\n{scope_md}\n\n"
        f"Diff:\n```diff\n{diff}\n```"
    )


def _truncate_diff(diff: str, max_input_tokens: int, prompt: str) -> str:
    overhead = estimate_tokens("", prompt)
    diff_budget = max(1000, max_input_tokens - overhead)
    max_chars = diff_budget * 3
    if len(diff) <= max_chars:
        return diff
    return diff[:max_chars] + TRUNCATION_NOTE


def _footer(scope_sha: str) -> str:
    return f"\n\n---\n_Reviewed by talkback-reviewer @ PROMPT.md@{scope_sha}_"


def run_review(
    pr_number: int,
    repo: str,
    *,
    github_client: GitHubClient,
    model_client: ModelClient,
    store: BudgetStore,
    source: str = "manual",
    model: str | None = None,
    max_input_tokens: int | None = None,
    audit_log: Path | None = None,
    enforce_pin: bool = True,
) -> ReviewResult:
    """Run one reviewer pass on ``pr_number`` in ``repo``.

    ``source="manual"`` bypasses the skip filter (per SCOPE.md escalation
    path). ``source="auto"`` is reserved for Phase 2; this Phase 1b
    implementation treats both the same way (no skip filter integration
    yet).
    """
    model = model or os.environ.get("REVIEWER_MODEL", DEFAULT_MODEL)
    max_input_tokens = max_input_tokens or int(
        os.environ.get("REVIEWER_MAX_INPUT_TOKENS", DEFAULT_MAX_INPUT_TOKENS)
    )

    prompt_template, scope_sha = load_prompt(enforce_pin=enforce_pin)
    pr = github_client.fetch_pr(repo, pr_number)

    if not pr.diff.strip():
        return ReviewResult(posted=False, skipped=True, reason="empty_diff", model=model)

    diff = _truncate_diff(pr.diff, max_input_tokens, prompt_template)
    estimated = estimate_tokens(diff, prompt_template)

    consume = check_and_increment(
        store, estimated, pr_number=pr_number, audit_log=audit_log
    )
    if not consume.allowed:
        cid = github_client.post_pr_comment(repo, pr_number, BUDGET_EXHAUSTED_COMMENT)
        return ReviewResult(
            posted=True,
            skipped=True,
            reason="budget_exhausted",
            used_tokens=estimated,
            model=model,
            comment_id=cid,
        )

    system_prompt, user_template = _split_prompt(prompt_template)
    scope_md = SCOPE_PATH.read_text()
    user_message = _render_user_message(pr, scope_md, diff)

    output = model_client(model, system_prompt, user_message).strip()
    if output == REFUSAL_TOKEN:
        return ReviewResult(
            posted=False,
            skipped=True,
            reason="reviewer_refused",
            used_tokens=estimated,
            model=model,
        )

    comment_body = output + _footer(scope_sha)
    comment_id = github_client.post_pr_comment(repo, pr_number, comment_body)
    return ReviewResult(
        posted=True,
        skipped=False,
        reason="",
        used_tokens=estimated,
        model=model,
        comment_id=comment_id,
    )


__all__ = [
    "DEFAULT_MAX_INPUT_TOKENS",
    "DEFAULT_MODEL",
    "GitHubClient",
    "ModelClient",
    "PRContent",
    "PROMPT_PATH",
    "REFUSAL_TOKEN",
    "ReviewResult",
    "SCOPE_PATH",
    "StalePromptPinError",
    "estimate_tokens",
    "load_prompt",
    "run_review",
]
