#!/usr/bin/env python3
"""SCRUM-498: clustering-rule simulator for discovery-digest.

These tests encode the v2 clustering rules documented in
``.claude/skills/discovery-digest/SKILL.md`` Section 3 and verified against
the corpus described in
``docs/agent/discovery-digest-calibration-2026-05.md``. The tests do not
talk to GitHub or Jira — they exercise the rule logic against fixture text.

The implementations here mirror the skill prose; if the agent runtime
diverges from this logic, the skill prose is the source of truth and these
tests must be updated.
"""

from __future__ import annotations

import re
import unittest
from dataclasses import dataclass
from datetime import datetime, timedelta


# ---- Rule implementation (mirrors SKILL.md Section 3) ----


SIGNAL_MARKERS = ("p95_ms=", "count=", "error_rate=", "request.uri=", "endpoint_id=")
NRQL_TEMPLATE_TOKENS = ("SELECT ", "FACET ")
ENDPOINT_RE = re.compile(r"(?:WebTransaction/Go/(?:GET|POST|PUT|PATCH|DELETE|OPTIONS) )?(/api/[A-Za-z0-9/_{}.-]+)")
NUMBERED_LIST_RE = re.compile(r"^\s*\d+\.\s+")
CODE_FENCE_RE = re.compile(r"^```")


def extract_signal_endpoints(body: str) -> set[str]:
    """Return endpoint identifiers found in result-row context only."""
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
    """Extract 'RED' or 'YELLOW' from a 'Triggered by status=' line."""
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


def are_eligible_to_cluster(a: ObsIssue, b: ObsIssue, max_days: int = 7) -> bool:
    """Return True if two issues pass all v2 gates and share a result-row endpoint."""
    if a.color is None or b.color is None or a.color != b.color:
        return False
    delta = abs((a.created_at - b.created_at).days)
    if delta > max_days:
        return False
    return bool(a.endpoints & b.endpoints)


def cluster(issues: list[ObsIssue]) -> list[list[int]]:
    """Build clusters via union-find on the eligibility relation."""
    n = len(issues)
    parent = list(range(n))

    def find(i):
        while parent[i] != i:
            parent[i] = parent[parent[i]]
            i = parent[i]
        return i

    def union(i, j):
        ri, rj = find(i), find(j)
        if ri != rj:
            parent[ri] = rj

    for i in range(n):
        for j in range(i + 1, n):
            if are_eligible_to_cluster(issues[i], issues[j]):
                union(i, j)

    groups: dict[int, list[int]] = {}
    for i in range(n):
        groups.setdefault(find(i), []).append(issues[i].number)
    return sorted(groups.values(), key=lambda g: (-len(g), g[0]))


# ---- Fixtures ----


NRQL_TEMPLATE_AUTH_LOGIN = """\
Triggered by status=YELLOW

### Latency By Txn P95

**NRQL:**
```
SELECT percentile(duration, 95) * 1000 AS 'p95_ms' FROM Transaction
WHERE appName = 'Talkback-NewRelic' AND name LIKE 'WebTransaction/%/POST /api/auth/login'
FACET name LIMIT 20 SINCE 30 minutes ago
```

No anomalies in window.
"""

ISSUE_WITH_RESULT_ROW_RED = """\
Triggered by status=RED

### Latency By Txn P95

**NRQL:**
```
SELECT percentile(duration, 95) * 1000 AS 'p95_ms' FROM Transaction
WHERE appName = 'Talkback-NewRelic' FACET name LIMIT 20 SINCE 30 minutes ago
```

Result rows:
1. WebTransaction/Go/POST /api/sessions/{id}/orchestration/recommendations/sync  p95_ms=4521
2. WebTransaction/Go/GET /api/me  p95_ms=145
"""

ISSUE_SAME_ENDPOINT_RED_NEXT_DAY = """\
Triggered by status=RED

Result rows:
1. WebTransaction/Go/POST /api/sessions/{id}/orchestration/recommendations/sync  p95_ms=5012
"""

ISSUE_SAME_ENDPOINT_YELLOW = """\
Triggered by status=YELLOW

Result rows:
1. WebTransaction/Go/POST /api/sessions/{id}/orchestration/recommendations/sync  p95_ms=2300
"""

ISSUE_DIFFERENT_ENDPOINT_RED = """\
Triggered by status=RED

Result rows:
1. WebTransaction/Go/GET /api/users  p95_ms=890  error_rate=0.05
"""


class TestEndpointExtraction(unittest.TestCase):
    def test_template_literal_endpoints_are_excluded(self):
        # Fixture #279 from calibration — only /api/auth/login mention is in NRQL.
        endpoints = extract_signal_endpoints(NRQL_TEMPLATE_AUTH_LOGIN)
        self.assertEqual(endpoints, set(), f"v1 false-cluster fixture leaked: {endpoints}")

    def test_result_row_endpoints_are_extracted(self):
        endpoints = extract_signal_endpoints(ISSUE_WITH_RESULT_ROW_RED)
        self.assertIn("/api/sessions/{id}/orchestration/recommendations/sync", endpoints)
        self.assertIn("/api/me", endpoints)

    def test_numbered_list_without_signal_marker_still_extracts(self):
        body = "1. WebTransaction/Go/POST /api/foo  some annotation\n"
        endpoints = extract_signal_endpoints(body)
        self.assertIn("/api/foo", endpoints)

    def test_nrql_line_does_not_leak_even_without_code_fence(self):
        body = "SELECT count(*) FROM Transaction WHERE name LIKE '/api/leak'\n"
        endpoints = extract_signal_endpoints(body)
        self.assertEqual(endpoints, set())

    def test_code_fence_blocks_excluded(self):
        body = "```\n/api/inside_fence p95_ms=999\n```\n1. WebTransaction/Go/POST /api/outside  p95_ms=42\n"
        endpoints = extract_signal_endpoints(body)
        self.assertIn("/api/outside", endpoints)
        self.assertNotIn("/api/inside_fence", endpoints)


class TestStatusColour(unittest.TestCase):
    def test_red_extracted(self):
        self.assertEqual(status_color(ISSUE_WITH_RESULT_ROW_RED), "RED")

    def test_yellow_extracted(self):
        self.assertEqual(status_color(NRQL_TEMPLATE_AUTH_LOGIN), "YELLOW")

    def test_missing_returns_none(self):
        self.assertIsNone(status_color("nothing here"))


class TestEligibility(unittest.TestCase):
    def _issue(self, n, day_offset, body):
        return ObsIssue(
            number=n,
            created_at=datetime(2026, 5, 8) + timedelta(days=day_offset),
            body=body,
        )

    def test_same_endpoint_same_color_within_7_days_clusters(self):
        a = self._issue(307, 0, ISSUE_WITH_RESULT_ROW_RED)
        b = self._issue(310, 1, ISSUE_SAME_ENDPOINT_RED_NEXT_DAY)
        self.assertTrue(are_eligible_to_cluster(a, b))

    def test_color_mismatch_rejected(self):
        a = self._issue(307, 0, ISSUE_WITH_RESULT_ROW_RED)
        b = self._issue(311, 0, ISSUE_SAME_ENDPOINT_YELLOW)
        self.assertFalse(are_eligible_to_cluster(a, b))

    def test_date_outside_7_days_rejected(self):
        a = self._issue(307, 0, ISSUE_WITH_RESULT_ROW_RED)
        b = self._issue(320, 8, ISSUE_SAME_ENDPOINT_RED_NEXT_DAY)
        self.assertFalse(are_eligible_to_cluster(a, b))

    def test_different_endpoints_rejected(self):
        a = self._issue(307, 0, ISSUE_WITH_RESULT_ROW_RED)
        b = self._issue(312, 0, ISSUE_DIFFERENT_ENDPOINT_RED)
        self.assertFalse(are_eligible_to_cluster(a, b))

    def test_template_only_issue_clusters_with_nothing(self):
        a = self._issue(279, 0, NRQL_TEMPLATE_AUTH_LOGIN)
        b = self._issue(307, 0, ISSUE_WITH_RESULT_ROW_RED)
        self.assertFalse(
            are_eligible_to_cluster(a, b),
            "template-only issue must NEVER cluster — this is the v1 false-positive fix",
        )


class TestClusterFormation(unittest.TestCase):
    def _issue(self, n, day_offset, body):
        return ObsIssue(
            number=n,
            created_at=datetime(2026, 5, 8) + timedelta(days=day_offset),
            body=body,
        )

    def test_template_only_corpus_yields_singletons(self):
        """v1 would cluster these all together; v2 must produce singletons."""
        issues = [
            self._issue(279, -2, NRQL_TEMPLATE_AUTH_LOGIN),
            self._issue(280, -1, NRQL_TEMPLATE_AUTH_LOGIN),
            self._issue(281, 0, NRQL_TEMPLATE_AUTH_LOGIN),
        ]
        clusters = cluster(issues)
        self.assertEqual([len(c) for c in clusters], [1, 1, 1])

    def test_genuine_incident_pair_clusters(self):
        issues = [
            self._issue(307, 0, ISSUE_WITH_RESULT_ROW_RED),
            self._issue(310, 1, ISSUE_SAME_ENDPOINT_RED_NEXT_DAY),
        ]
        clusters = cluster(issues)
        self.assertEqual(len(clusters), 1)
        self.assertEqual(set(clusters[0]), {307, 310})

    def test_mixed_template_and_genuine_yields_two_clusters(self):
        issues = [
            self._issue(279, 0, NRQL_TEMPLATE_AUTH_LOGIN),  # singleton (template-only)
            self._issue(307, 1, ISSUE_WITH_RESULT_ROW_RED),  # joins 310
            self._issue(310, 2, ISSUE_SAME_ENDPOINT_RED_NEXT_DAY),  # joins 307
        ]
        clusters = cluster(issues)
        # 1 cluster of size 2 + 1 singleton.
        sizes = sorted(len(c) for c in clusters)
        self.assertEqual(sizes, [1, 2])


if __name__ == "__main__":
    unittest.main()
