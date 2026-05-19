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
P95_MS_RE = re.compile(r"p95_ms=([0-9]+(?:\.[0-9]+)?)")

DEFAULT_MAX_DAYS = 7
DEFAULT_MIN_P95_MS = 100.0  # SCRUM-501: endpoints below this don't cluster.


def extract_signal_endpoints(body: str) -> set[tuple[str, float]]:
    """Return ``(endpoint, p95_ms)`` pairs found in result-row context.

    v4 (SCRUM-501): returns the **endpoint + measured p95 latency** so the
    cluster algorithm can filter by threshold. v3's set-of-strings return
    couldn't distinguish a top-N baseline endpoint (always present at a
    few ms) from an actually-slow incident endpoint, yielding 33% precision
    on the live obs-agent corpus. v4 captures the p95 value alongside the
    endpoint; ``cluster()`` then ignores endpoints below the threshold.

    Excludes:
    - Lines containing NRQL keywords ``SELECT `` or ``FACET ``.
    - Lines that don't parse a ``p95_ms=N`` numeric value (no p95 →
      the endpoint mention can't be threshold-filtered, so it's
      conservatively dropped).

    Includes endpoints from lines that either:
    - Contain a signal marker (``p95_ms=``, ``count=``, ``error_rate=``,
      ``request.uri=``, ``endpoint_id=``), or
    - Begin with a numbered-list marker (``1. ``, ``2. ``, ...).
    """
    found: set[tuple[str, float]] = set()
    for line in body.splitlines():
        if any(tok in line for tok in NRQL_TEMPLATE_TOKENS):
            continue
        if any(marker in line for marker in SIGNAL_MARKERS) or NUMBERED_LIST_RE.match(line):
            p95_match = P95_MS_RE.search(line)
            if p95_match is None:
                continue
            try:
                p95 = float(p95_match.group(1))
            except ValueError:
                continue
            for endpoint in ENDPOINT_RE.findall(line):
                found.add((endpoint, p95))
    return found


def endpoints_above_threshold(
    pairs: set[tuple[str, float]], min_p95_ms: float
) -> set[str]:
    """Return just the endpoint names whose p95 meets the threshold."""
    return {endpoint for endpoint, p95 in pairs if p95 >= min_p95_ms}


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
    def endpoints(self) -> set[tuple[str, float]]:
        """SCRUM-501: returns ``(endpoint, p95_ms)`` pairs (was ``set[str]`` pre-v4)."""
        return extract_signal_endpoints(self.body)

    @property
    def color(self) -> str | None:
        return status_color(self.body)


def are_eligible_to_cluster(
    a: ObsIssue,
    b: ObsIssue,
    max_days: int = DEFAULT_MAX_DAYS,
    min_p95_ms: float = DEFAULT_MIN_P95_MS,
) -> bool:
    """Return ``True`` if two issues pass all v4 gates and share a *slow* endpoint.

    v4 (SCRUM-501): adds a ``min_p95_ms`` threshold. Endpoints below the
    threshold are baseline noise (top-N rankings include them whether or
    not they're actually slow) and do not contribute to eligibility.
    Default threshold: 100 ms.
    """
    if a.color is None or b.color is None or a.color != b.color:
        return False
    delta = abs((a.created_at - b.created_at).days)
    if delta > max_days:
        return False
    a_slow = endpoints_above_threshold(a.endpoints, min_p95_ms)
    b_slow = endpoints_above_threshold(b.endpoints, min_p95_ms)
    return bool(a_slow & b_slow)


def cluster(
    issues: list[ObsIssue],
    max_days: int = DEFAULT_MAX_DAYS,
    min_p95_ms: float = DEFAULT_MIN_P95_MS,
) -> list[list[int]]:
    """Build clusters via union-find on the eligibility relation.

    v4: clusters only on endpoints whose p95 meets ``min_p95_ms``. See
    ``are_eligible_to_cluster`` for the gate sequence.

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
            if are_eligible_to_cluster(
                issues[i], issues[j], max_days=max_days, min_p95_ms=min_p95_ms
            ):
                union(i, j)

    groups: dict[int, list[int]] = {}
    for i in range(n):
        groups.setdefault(find(i), []).append(issues[i].number)
    return sorted(groups.values(), key=lambda g: (-len(g), g[0]))
