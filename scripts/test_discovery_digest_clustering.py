#!/usr/bin/env python3
"""SCRUM-498: clustering-rule simulator for discovery-digest.

These tests encode the v2 clustering rules documented in
``.claude/skills/discovery-digest/SKILL.md`` Section 3 and verified against
the corpus described in
``docs/agent/discovery-digest-calibration-2026-05.md``. The tests do not
talk to GitHub or Jira — they exercise the rule logic against fixture text.

The rule implementation lives in ``scripts/discovery_digest_cluster.py``
(refactored in SCRUM-499 so the same module is used by both the test
suite and the ``discovery_digest_score.py`` CLI). The skill prose is the
source of truth; if runtime diverges, update the prose and these tests.
"""

from __future__ import annotations

import sys
import unittest
from datetime import datetime, timedelta
from pathlib import Path

_REPO_ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(_REPO_ROOT / "scripts"))

from discovery_digest_cluster import (  # noqa: E402
    DEFAULT_MIN_P95_MS,
    ObsIssue,
    are_eligible_to_cluster,
    cluster,
    endpoints_above_threshold,
    extract_signal_endpoints,
    status_color,
)


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
        """SCRUM-501: v4 returns (endpoint, p95_ms) tuples instead of strings."""
        endpoints = extract_signal_endpoints(ISSUE_WITH_RESULT_ROW_RED)
        # Tuple form: ("/api/sessions/{id}/orchestration/recommendations/sync", 4521.0)
        self.assertIn(
            ("/api/sessions/{id}/orchestration/recommendations/sync", 4521.0),
            endpoints,
        )
        self.assertIn(("/api/me", 145.0), endpoints)

    def test_v4_requires_p95_ms_for_extraction(self):
        """SCRUM-501: numbered-list line WITHOUT p95_ms doesn't extract.
        v3 extracted any numbered-list endpoint; v4 needs the value to
        compare against the threshold.
        """
        body = "1. WebTransaction/Go/POST /api/foo  some annotation\n"
        endpoints = extract_signal_endpoints(body)
        self.assertEqual(endpoints, set())

    def test_v4_numbered_list_with_p95_extracts(self):
        body = "1. WebTransaction/Go/POST /api/foo  p95_ms=42\n"
        endpoints = extract_signal_endpoints(body)
        self.assertIn(("/api/foo", 42.0), endpoints)

    def test_nrql_line_does_not_leak_even_without_code_fence(self):
        body = "SELECT count(*) FROM Transaction WHERE name LIKE '/api/leak'\n"
        endpoints = extract_signal_endpoints(body)
        self.assertEqual(endpoints, set())

    def test_fenced_signal_lines_now_extract_v3(self):
        """SCRUM-500: v3 removed the fence-exclusion. Signal-bearing lines
        INSIDE fenced code blocks must now extract their endpoints, as long
        as they pass the SELECT/FACET filter. SCRUM-501: assertions updated
        to v4 tuple form.
        """
        body = (
            "```\n"
            "/api/inside_fence p95_ms=999\n"
            "1. WebTransaction/Go/GET /api/inside_numbered  p95_ms=42\n"
            "```\n"
            "1. WebTransaction/Go/POST /api/outside  p95_ms=42\n"
        )
        endpoints = extract_signal_endpoints(body)
        self.assertIn(
            ("/api/inside_fence", 999.0),
            endpoints,
            "v4 must extract signal-marker lines even inside fences",
        )
        self.assertIn(
            ("/api/inside_numbered", 42.0),
            endpoints,
            "v4 must extract numbered-list lines even inside fences",
        )
        self.assertIn(("/api/outside", 42.0), endpoints)

    def test_fenced_nrql_still_excluded_v3(self):
        """SCRUM-500: under v3, NRQL queries inside fences are still excluded
        by the SELECT/FACET line-level filter — the rule that actually
        protects against template-literal false positives.
        """
        body = (
            "```\n"
            "SELECT count(*) FROM Transaction\n"
            "WHERE name LIKE '/api/auth/login' FACET name\n"
            "```\n"
        )
        endpoints = extract_signal_endpoints(body)
        self.assertEqual(
            endpoints,
            set(),
            "NRQL lines must be excluded regardless of fence presence",
        )


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


class TestV4ThresholdFilter(unittest.TestCase):
    """SCRUM-501: p95 threshold filter — endpoints below threshold are baseline noise."""

    def _issue(self, n, day_offset, body):
        return ObsIssue(
            number=n,
            created_at=datetime(2026, 5, 8) + timedelta(days=day_offset),
            body=body,
        )

    def test_default_threshold_constant(self):
        self.assertEqual(DEFAULT_MIN_P95_MS, 100.0)

    def test_endpoints_above_threshold_filters(self):
        pairs = {("/api/a", 50.0), ("/api/b", 200.0), ("/api/c", 100.0)}
        # Threshold is inclusive at 100.
        self.assertEqual(
            endpoints_above_threshold(pairs, 100.0),
            {"/api/b", "/api/c"},
        )

    def test_v4_p95_threshold_filters_baseline(self):
        """Two issues both with /api/me at p95=5ms (baseline) must NOT cluster
        under v4. Under v3 they would have shared endpoint /api/me.
        """
        baseline = (
            "Triggered by status=RED\n"
            "1. WebTransaction/Go/GET /api/me  p95_ms=5\n"
        )
        a = self._issue(1, 0, baseline)
        b = self._issue(2, 1, baseline)
        self.assertFalse(are_eligible_to_cluster(a, b))

    def test_v4_above_threshold_clusters(self):
        """Two issues with /api/foo at p95=200ms (above the 100ms threshold)
        DO cluster under v4.
        """
        slow = (
            "Triggered by status=RED\n"
            "1. WebTransaction/Go/GET /api/foo  p95_ms=200\n"
        )
        a = self._issue(1, 0, slow)
        b = self._issue(2, 1, slow)
        self.assertTrue(are_eligible_to_cluster(a, b))

    def test_v4_custom_threshold_override(self):
        """The threshold parameter can be overridden per-invocation."""
        body = (
            "Triggered by status=YELLOW\n"
            "1. WebTransaction/Go/GET /api/x  p95_ms=75\n"
        )
        a = self._issue(1, 0, body)
        b = self._issue(2, 0, body)
        # Default threshold (100): below → does NOT cluster.
        self.assertFalse(are_eligible_to_cluster(a, b))
        # Lower threshold (50): above → clusters.
        self.assertTrue(are_eligible_to_cluster(a, b, min_p95_ms=50.0))
        # Higher threshold (200): below → does NOT cluster.
        self.assertFalse(are_eligible_to_cluster(a, b, min_p95_ms=200.0))

    def test_v4_mixed_endpoints_only_slow_one_counts(self):
        """When issues share both a baseline and a slow endpoint, clustering
        is driven by the slow one only.
        """
        body_a = (
            "Triggered by status=RED\n"
            "1. WebTransaction/Go/GET /api/baseline  p95_ms=5\n"
            "2. WebTransaction/Go/GET /api/slow      p95_ms=300\n"
        )
        body_b = (
            "Triggered by status=RED\n"
            "1. WebTransaction/Go/GET /api/baseline  p95_ms=8\n"
            "2. WebTransaction/Go/GET /api/slow      p95_ms=400\n"
        )
        a = self._issue(1, 0, body_a)
        b = self._issue(2, 1, body_b)
        self.assertTrue(are_eligible_to_cluster(a, b))

    def test_v4_corpus_pattern_two_baseline_only_issues_dont_cluster(self):
        """End-to-end via cluster(): two issues sharing only baseline endpoints
        produce two singletons under v4 even though they share endpoint names.
        This is the SCRUM-500 corpus failure mode reproduced as a fixture.
        """
        body = (
            "Triggered by status=RED\n"
            "1. WebTransaction/Go/GET /api/me              p95_ms=7\n"
            "2. WebTransaction/Go/GET /api/teams/status    p95_ms=2\n"
            "3. WebTransaction/Go/GET /api/zoom/status     p95_ms=3\n"
        )
        a = self._issue(1, 0, body)
        b = self._issue(2, 1, body)
        clusters = cluster([a, b])
        self.assertEqual([len(c) for c in clusters], [1, 1])


class TestV5PerEndpointGrouping(unittest.TestCase):
    """SCRUM-502: cluster() rewritten from union-find to per-endpoint grouping.

    A "bridge" issue (multiple above-threshold endpoints) may now appear in
    multiple multi-member clusters — one per slow endpoint. Each cluster has
    a concrete anchor.
    """

    def _issue(self, n, day_offset, body):
        return ObsIssue(
            number=n,
            created_at=datetime(2026, 5, 8) + timedelta(days=day_offset),
            body=body,
        )

    def test_bridge_issue_appears_in_multiple_clusters(self):
        """SCRUM-501 v4 produced a 3-member chain (#307,#219,#158) with no
        common slow endpoint because #219 bridged two pairs that shared
        different endpoints. v5 must split this into two pairwise clusters,
        each with its own anchor endpoint, and #219 appears in both.
        """
        body_a = (
            "Triggered by status=RED\n"
            "1. WebTransaction/Go/POST /api/sessions/  p95_ms=470\n"
        )
        body_bridge = (
            "Triggered by status=RED\n"
            "1. WebTransaction/Go/POST /api/sessions/    p95_ms=480\n"
            "2. WebTransaction/Go/GET  /api/invitations/ p95_ms=341\n"
        )
        body_c = (
            "Triggered by status=RED\n"
            "1. WebTransaction/Go/GET /api/invitations/  p95_ms=444\n"
        )
        # Offsets chosen so the outer pair (#307, #158) is OUTSIDE the
        # 7-day gate but each adjacent pair through the bridge is INSIDE.
        # Tests that v5 splits the chain into two 2-member clusters.
        a = self._issue(307, 0, body_a)
        bridge = self._issue(219, -5, body_bridge)
        c = self._issue(158, -10, body_c)
        clusters = cluster([a, bridge, c])
        # Expected: two 2-member clusters + zero singletons
        # (#307+#219 on /api/sessions/, #219+#158 on /api/invitations/).
        multi = [g for g in clusters if len(g) >= 2]
        singletons = [g for g in clusters if len(g) == 1]
        self.assertEqual(len(multi), 2, f"expected 2 multi-member clusters, got {multi}")
        # Each multi-member cluster should be size 2.
        self.assertEqual(sorted(len(g) for g in multi), [2, 2])
        # #219 should appear in both multi-member clusters (bridge issue).
        bridge_count = sum(1 for g in multi if 219 in g)
        self.assertEqual(bridge_count, 2, "bridge issue must appear in both clusters")
        # Each pairwise cluster should contain the bridge + the right partner.
        all_pairs = sorted(tuple(g) for g in multi)
        self.assertEqual(all_pairs, [(158, 219), (219, 307)])
        # No singletons (every issue appears in at least one multi-member cluster).
        self.assertEqual(singletons, [])

    def test_dedup_same_pair_via_multiple_endpoints(self):
        """When two issues share TWO different above-threshold endpoints,
        they form ONE cluster (deduplicated by member set), not two.
        """
        body = (
            "Triggered by status=RED\n"
            "1. WebTransaction/Go/POST /api/foo  p95_ms=300\n"
            "2. WebTransaction/Go/GET  /api/bar  p95_ms=400\n"
        )
        a = self._issue(1, 0, body)
        b = self._issue(2, 1, body)
        clusters = cluster([a, b])
        multi = [g for g in clusters if len(g) >= 2]
        # Same pair found via /api/foo and via /api/bar — dedup to one cluster.
        self.assertEqual(len(multi), 1)
        self.assertEqual(sorted(multi[0]), [1, 2])

    def test_singletons_only_when_not_in_any_multi(self):
        """An issue not in any multi-member cluster appears as a singleton.
        An issue in a multi-member cluster does NOT also appear as a singleton.
        """
        slow = (
            "Triggered by status=RED\n"
            "1. WebTransaction/Go/POST /api/x  p95_ms=200\n"
        )
        baseline = (
            "Triggered by status=RED\n"
            "1. WebTransaction/Go/POST /api/y  p95_ms=5\n"
        )
        a = self._issue(1, 0, slow)
        b = self._issue(2, 1, slow)
        c = self._issue(3, 0, baseline)  # No above-threshold endpoint → singleton.
        clusters = cluster([a, b, c])
        multi = [g for g in clusters if len(g) >= 2]
        singletons = [g for g in clusters if len(g) == 1]
        self.assertEqual(len(multi), 1)
        self.assertEqual(sorted(multi[0]), [1, 2])
        self.assertEqual(singletons, [[3]])
        # Members of the multi-member cluster do NOT also appear as singletons.
        self.assertNotIn([1], singletons)
        self.assertNotIn([2], singletons)

    def test_color_mismatch_not_clustered_even_on_same_endpoint(self):
        """An issue with RED status doesn't cluster with one carrying YELLOW
        on the same slow endpoint. Same gate as v4.
        """
        body_template = (
            "Triggered by status={color}\n"
            "1. WebTransaction/Go/POST /api/x  p95_ms=300\n"
        )
        a = self._issue(1, 0, body_template.format(color="RED"))
        b = self._issue(2, 1, body_template.format(color="YELLOW"))
        clusters = cluster([a, b])
        multi = [g for g in clusters if len(g) >= 2]
        self.assertEqual(multi, [])

    def test_date_outside_window_not_clustered_even_on_same_endpoint(self):
        """v5 retains the ≤ max_days date-proximity gate (default 7)."""
        body = (
            "Triggered by status=RED\n"
            "1. WebTransaction/Go/POST /api/x  p95_ms=300\n"
        )
        a = self._issue(1, 0, body)
        b = self._issue(2, 8, body)  # 8 days apart
        clusters = cluster([a, b])
        multi = [g for g in clusters if len(g) >= 2]
        self.assertEqual(multi, [])

    def test_v4_residual_fp_eliminated_under_v5(self):
        """SCRUM-501 residual: 3-member chain #307+#219+#158 with no common
        endpoint. v5 must produce two 2-member clusters, neither containing
        all three together.
        """
        body_307 = (
            "Triggered by status=RED\n"
            "1. WebTransaction/Go/POST /api/sessions/   p95_ms=470\n"
        )
        body_219 = (
            "Triggered by status=RED\n"
            "1. WebTransaction/Go/POST /api/sessions/      p95_ms=480\n"
            "2. WebTransaction/Go/GET  /api/invitations/   p95_ms=341\n"
        )
        body_158 = (
            "Triggered by status=RED\n"
            "1. WebTransaction/Go/GET /api/invitations/  p95_ms=444\n"
        )
        # Same offsets as the bridge-issue test: 0, -5, -10 → outer pair
        # 10 days apart (outside gate), each adjacent pair 5 days (inside).
        a = self._issue(307, 0, body_307)
        b = self._issue(219, -5, body_219)
        c = self._issue(158, -10, body_158)
        clusters = cluster([a, b, c])
        multi = [g for g in clusters if len(g) >= 2]
        # No 3-member cluster — that was v4's structural FP.
        self.assertFalse(
            any(len(g) >= 3 for g in multi),
            f"v5 must NOT produce 3-member chain: {multi}",
        )


if __name__ == "__main__":
    unittest.main()
