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
    """Return ``True`` if two issues pass all gates and share a slow endpoint.

    v5 (SCRUM-502): retained for backward compatibility and for callers that
    want a pairwise eligibility check. The new ``cluster()`` no longer uses
    this — it groups per-endpoint instead. See ``cluster()`` docstring.
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
    """Build clusters via **per-endpoint grouping** (v5 — SCRUM-502).

    Replaces the v4 union-find algorithm, which over-grouped issues into
    transitive chains without a common above-threshold endpoint (SCRUM-501
    found a 3-member cluster #307+#219+#158 where #307 ↔ #158 had no shared
    slow endpoint — only the bridge #219 connected them).

    Algorithm:
      1. For each above-threshold endpoint, collect all issues that contain
         it (with their status colour).
      2. Within each (endpoint, colour) group, run union-find on pairwise
         date proximity (≤ ``max_days``). Each connected sub-group with ≥ 2
         members becomes a cluster anchored on that endpoint.
      3. Deduplicate by member set: if two endpoints group the exact same
         set of issues (e.g. #307+#219 share both /api/sessions/ and
         /api/me above threshold), emit only one cluster.
      4. Singletons are issues that don't appear in any multi-member
         cluster — emitted as one-element lists.

    A "bridge" issue (multiple above-threshold endpoints) may appear in
    multiple multi-member clusters, one per slow endpoint. This is
    intentional: each cluster has a concrete anchor endpoint and the
    proposed Jira tickets render with non-empty shared_endpoints.

    Returned clusters are sorted: multi-member first (size desc, then
    lowest member asc), singletons after (by issue number asc).
    """
    # Step 1: index above-threshold endpoint → list of issues with that color.
    endpoint_to_color_issues: dict[str, dict[str, list[ObsIssue]]] = {}
    for issue in issues:
        if issue.color is None:
            continue
        slow = endpoints_above_threshold(issue.endpoints, min_p95_ms)
        for ep in slow:
            endpoint_to_color_issues.setdefault(ep, {}).setdefault(
                issue.color, []
            ).append(issue)

    # Step 2: within each (endpoint, color) group, build date-proximity
    # connected components via union-find.
    multi_member: list[list[int]] = []
    seen_member_sets: set[frozenset[int]] = set()
    issues_in_multi: set[int] = set()

    for endpoint, by_color in endpoint_to_color_issues.items():
        for color, color_issues in by_color.items():
            n = len(color_issues)
            if n < 2:
                continue
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
                    days = abs(
                        (color_issues[i].created_at - color_issues[j].created_at).days
                    )
                    if days <= max_days:
                        union(i, j)

            groups: dict[int, list[int]] = {}
            for i in range(n):
                groups.setdefault(find(i), []).append(color_issues[i].number)

            for group in groups.values():
                if len(group) < 2:
                    continue
                key = frozenset(group)
                if key in seen_member_sets:
                    continue
                seen_member_sets.add(key)
                multi_member.append(sorted(group))
                issues_in_multi.update(group)

    # Step 3: singletons — issues not in any multi-member cluster.
    singletons: list[list[int]] = [
        [i.number] for i in issues if i.number not in issues_in_multi
    ]

    multi_member.sort(key=lambda g: (-len(g), g[0]))
    singletons.sort(key=lambda g: g[0])

    return multi_member + singletons
