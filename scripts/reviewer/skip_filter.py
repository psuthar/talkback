#!/usr/bin/env python3
"""SCRUM-510: skip filter for the talkback-reviewer agent.

Decides which PRs the reviewer auto-skips before any model call.
Policy source: ``.github/talkback-reviewer/SCOPE.md`` (SCRUM-509).

This module is pure logic — no network, no Jira/GitHub calls. Callers
(the Phase 1 slash command, Phase 2 auto-trigger workflow) construct a
``PRMetadata`` from pre-fetched data and ask ``should_skip(pr)`` for the
decision. Decoupling skip logic from invocation logic means: (a) the
same function powers dry-run analysis, (b) skip rules can evolve
without touching CI YAML, (c) every callsite gets the same answer for
the same PR.

Rules and rationale live in SCOPE.md's "Skip rules" section. This file
is the implementation; SCOPE.md is the policy.
"""

from __future__ import annotations

import fnmatch
import os
from dataclasses import dataclass, field
from typing import Iterable

DEFAULT_BOT_AUTHORS = frozenset(
    {
        "dependabot[bot]",
        "renovate[bot]",
        "github-actions[bot]",
        "claude[bot]",
    }
)

DOCS_PATTERNS = (
    "*.md",
    "docs/**",
    "CHANGELOG",
    "CHANGELOG.md",
    "LICENSE",
    "LICENSE.*",
    "*.txt",
)

# Paths that should not count toward "source LOC" when applying the
# under-threshold skip rule. Tests, docs, lockfiles, generated files.
NON_SOURCE_PATTERNS = (
    "*.md",
    "docs/**",
    "*.test.*",
    "*.spec.*",
    "**/test_*.py",
    "**/__tests__/**",
    "**/fixtures/**",
    "**/__fixtures__/**",
    "package-lock.json",
    "yarn.lock",
    "pnpm-lock.yaml",
    "go.sum",
)

SKIP_LABEL = "skip-reviewer"

DEFAULT_MIN_SOURCE_LOC = 100


@dataclass
class PRMetadata:
    number: int
    title: str
    author_login: str
    draft: bool
    labels: list[str] = field(default_factory=list)
    changed_files: list[str] = field(default_factory=list)
    additions: int = 0
    deletions: int = 0


def _path_matches_pattern(path: str, pattern: str) -> bool:
    """Match ``path`` against a glob ``pattern`` with ``**`` support.

    Handles three common forms:
      - "*.md" — basename glob; matches if the basename matches.
      - "docs/**" — directory prefix; matches any path under docs/.
      - "**/test_*.py" — suffix-anchored pattern at any depth.
    """
    if "**" in pattern:
        head, _, tail = pattern.partition("**")
        head = head.rstrip("/")
        tail = tail.lstrip("/")
        prefix_ok = head == "" or path.startswith(head + "/") or path == head
        if not prefix_ok:
            return False
        if tail == "":
            return True
        # tail may itself be a glob (e.g. "test_*.py")
        # match against any suffix segment of the remaining path
        remainder = path[len(head) :].lstrip("/")
        # check any subpath ending matches
        parts = remainder.split("/")
        for i in range(len(parts)):
            candidate = "/".join(parts[i:])
            if fnmatch.fnmatch(candidate, tail) or fnmatch.fnmatch(
                os.path.basename(candidate), tail
            ):
                return True
        return False
    if "/" in pattern:
        return fnmatch.fnmatch(path, pattern)
    return fnmatch.fnmatch(os.path.basename(path), pattern)


def _path_matches_any(path: str, patterns: Iterable[str]) -> bool:
    return any(_path_matches_pattern(path, p) for p in patterns)


def _is_docs_only(changed_files: list[str]) -> bool:
    if not changed_files:
        return False
    return all(_path_matches_any(p, DOCS_PATTERNS) for p in changed_files)


def _source_loc(changed_files: list[str], additions: int, deletions: int) -> int:
    """Approximate source-LOC by filtering non-source paths from the file list.

    We do not have per-file line counts in ``PRMetadata`` — GitHub's PR
    list API gives totals, not per-file deltas, without an extra round
    trip. We scale ``additions + deletions`` by the fraction of files
    that look like source. Cheap and good enough for a skip threshold;
    pathological cases (one massive lockfile + one tiny source change)
    are acceptable false-skips because the alternative is a per-file
    API call and we are trying to be cost-cheap by design.
    """
    if not changed_files:
        return 0
    total_files = len(changed_files)
    source_files = sum(
        1 for p in changed_files if not _path_matches_any(p, NON_SOURCE_PATTERNS)
    )
    if source_files == 0:
        return 0
    return int((additions + deletions) * (source_files / total_files))


def should_skip(
    pr: PRMetadata,
    *,
    min_source_loc: int | None = None,
    bot_authors: Iterable[str] | None = None,
) -> tuple[bool, str]:
    """Return ``(skip, reason)`` for ``pr``.

    Order matters: cheapest/most-definitive checks first. The first
    matching rule wins; ``reason`` is one of the five enumerated strings
    in SCOPE.md or empty when ``skip`` is False.

    ``min_source_loc`` defaults to ``REVIEWER_MIN_SOURCE_LOC`` env var
    or ``DEFAULT_MIN_SOURCE_LOC``. ``bot_authors`` defaults to
    ``DEFAULT_BOT_AUTHORS``.
    """
    if min_source_loc is None:
        min_source_loc = int(
            os.environ.get("REVIEWER_MIN_SOURCE_LOC", DEFAULT_MIN_SOURCE_LOC)
        )
    bots = frozenset(bot_authors) if bot_authors is not None else DEFAULT_BOT_AUTHORS

    if pr.draft:
        return True, "draft"
    if SKIP_LABEL in pr.labels:
        return True, "skip_label"
    if pr.author_login in bots:
        return True, "bot_author"
    if _is_docs_only(pr.changed_files):
        return True, "docs_only"
    if _source_loc(pr.changed_files, pr.additions, pr.deletions) < min_source_loc:
        return True, "under_loc_threshold"
    return False, ""


__all__ = [
    "PRMetadata",
    "should_skip",
    "DEFAULT_MIN_SOURCE_LOC",
    "DEFAULT_BOT_AUTHORS",
    "SKIP_LABEL",
    "DOCS_PATTERNS",
    "NON_SOURCE_PATTERNS",
]
