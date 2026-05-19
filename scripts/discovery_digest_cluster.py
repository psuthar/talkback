#!/usr/bin/env python3
"""SCRUM-499: reusable v2 clustering module for discovery-digest.

Factored out of ``scripts/test_discovery_digest_clustering.py`` (SCRUM-498)
so the same logic can be exercised both from the test suite and from the
``scripts/discovery_digest_score.py`` CLI. The skill prose at
``.claude/skills/discovery-digest/SKILL.md`` Section 3 is the source of
truth; this module mirrors it.

Public surface:

- ``extract_signal_endpoints(body)`` — set of endpoint identifiers found
  in result-row context only.
- ``status_color(body)`` — ``"RED"`` / ``"YELLOW"`` / ``None``.
- ``ObsIssue`` — dataclass holding number, created_at, body.
- ``are_eligible_to_cluster(a, b)`` — bool, applies all v2 gates.
- ``cluster(issues)`` — list of clusters (each a list of issue numbers).

The rule implementation is pure-Python with stdlib only; no third-party
imports so the module works in any environment that already runs the
test suite.
"""

from __future__ import annotations

import re
from dataclasses import dataclass
from datetime import datetime


SIGNAL_MARKERS = (
    "p95_ms=",
    "count=",
    "error_rate=",
    "request.uri=",
    "endpoint_id=",
)
NRQL_TEMPLATE_TOKENS = ("SELECT ", "FACET ")
ENDPOINT_RE = re.compile(
    r"(?:WebTransaction/Go/(?:GET|POST|PUT|PATCH|DELETE|OPTIONS) )?"
    r"(/api/[A-Za-z0-9/_{}.-]+)"
)
NUMBERED_LIST_RE = re.compile(r"^\s*\d+\.\s+")
CODE_FENCE_RE = re.compile(r"^```")

DEFAULT_MAX_DAYS = 7


def extract_signal_endpoints(body: str) -> set[str]:
    """Return endpoint identifiers found in result-row context only.

    Excludes:
    - Lines inside fenced code blocks.
    - Lines containing NRQL keywords ``SELECT `` or ``FACET ``.

    Includes endpoints from lines that either:
    - Contain a signal marker (``p95_ms=``, ``count=``, ``error_rate=``,
      ``request.uri=``, ``endpoint_id=``), or
    - Begin with a numbered-list marker (``1. ``, ``2. ``, ...).
    """
    found: set[str] = set()
    in_code_fence = False
    for line in body.splitlines():
        if CODE_FENCE_RE.match(line):
            in_code_fence = not in_code_fence
            continue
        if in_code_fence:
            continue
        if any(tok in line for tok in NRQL_TEMPLATE_TOKENS):
            continue
        if any(marker in line for marker in SIGNAL_MARKERS) or NUMBERED_LIST_RE.match(line):
            for m in ENDPOINT_RE.findall(line):
                found.add(m)
    return found


def status_color(body: str) -> str | None:
    """Extract ``RED`` or ``YELLOW`` from a ``Triggered by status=`` line."""
    for line in body.splitlines():
        if "Triggered by status=" in line:
            for token in ("RED", "YELLOW"):
                if token in line:
                    return token
    return None


@dataclass
class ObsIssue:
    number: int
    created_at: datetime
    body: str

    @property
    def endpoints(self) -> set[str]:
        return extract_signal_endpoints(self.body)

    @property
    def color(self) -> str | None:
        return status_color(self.body)


def are_eligible_to_cluster(
    a: ObsIssue, b: ObsIssue, max_days: int = DEFAULT_MAX_DAYS
) -> bool:
    """Return ``True`` if two issues pass all v2 gates and share an endpoint."""
    if a.color is None or b.color is None or a.color != b.color:
        return False
    delta = abs((a.created_at - b.created_at).days)
    if delta > max_days:
        return False
    return bool(a.endpoints & b.endpoints)


def cluster(
    issues: list[ObsIssue], max_days: int = DEFAULT_MAX_DAYS
) -> list[list[int]]:
    """Build clusters via union-find on the eligibility relation.

    Returned clusters are sorted by size descending then by lowest issue
    number ascending. Singletons (issues that don't pair with any other)
    appear as one-element clusters.
    """
    n = len(issues)
    parent = list(range(n))

    def find(i: int) -> int:
        while parent[i] != i:
            parent[i] = parent[parent[i]]
            i = parent[i]
        return i

    def union(i: int, j: int) -> None:
        ri, rj = find(i), find(j)
        if ri != rj:
            parent[ri] = rj

    for i in range(n):
        for j in range(i + 1, n):
            if are_eligible_to_cluster(issues[i], issues[j], max_days=max_days):
                union(i, j)

    groups: dict[int, list[int]] = {}
    for i in range(n):
        groups.setdefault(find(i), []).append(issues[i].number)
    return sorted(groups.values(), key=lambda g: (-len(g), g[0]))
