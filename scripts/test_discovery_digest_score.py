#!/usr/bin/env python3
"""Unit tests for scripts/discovery_digest_score.py (SCRUM-499)."""

from __future__ import annotations

import io
import json
import sys
import tempfile
import unittest
from contextlib import redirect_stdout
from pathlib import Path

_REPO_ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(_REPO_ROOT / "scripts"))

import discovery_digest_score as score  # noqa: E402


SIGNAL_BODY_RED = """\
Triggered by status=RED

Result rows:
1. WebTransaction/Go/POST /api/sessions/{id}/orchestration/recommendations/sync  p95_ms=4521
"""

SIGNAL_BODY_RED_SAME = """\
Triggered by status=RED

Result rows:
1. WebTransaction/Go/POST /api/sessions/{id}/orchestration/recommendations/sync  p95_ms=5012
"""

TEMPLATE_ONLY_YELLOW = """\
Triggered by status=YELLOW

**NRQL:**
```
SELECT count(*) FROM Transaction WHERE name LIKE '/api/auth/login'
FACET name
```

No anomalies in window.
"""


def _fixture_issues():
    return [
        {"number": 307, "createdAt": "2026-05-08T12:00:00Z", "body": SIGNAL_BODY_RED},
        {"number": 310, "createdAt": "2026-05-09T12:00:00Z", "body": SIGNAL_BODY_RED_SAME},
        {"number": 279, "createdAt": "2026-05-06T12:00:00Z", "body": TEMPLATE_ONLY_YELLOW},
    ]


class TestParseIssues(unittest.TestCase):
    def test_parses_list_form(self):
        issues = score._parse_issues(_fixture_issues())
        self.assertEqual(len(issues), 3)
        self.assertEqual({i.number for i in issues}, {307, 310, 279})

    def test_parses_object_with_issues_key(self):
        issues = score._parse_issues({"issues": _fixture_issues()})
        self.assertEqual(len(issues), 3)

    def test_parses_object_with_data_key(self):
        issues = score._parse_issues({"data": _fixture_issues()})
        self.assertEqual(len(issues), 3)

    def test_strips_zulu_suffix_for_iso(self):
        issues = score._parse_issues(
            [
                {
                    "number": 1,
                    "createdAt": "2026-05-08T12:00:00Z",
                    "body": SIGNAL_BODY_RED,
                }
            ]
        )
        self.assertEqual(len(issues), 1)
        self.assertEqual(issues[0].created_at.year, 2026)

    def test_skips_invalid_entries(self):
        issues = score._parse_issues(
            [
                {"number": 1, "createdAt": "2026-05-08T12:00:00Z", "body": ""},
                {"createdAt": "2026-05-09T12:00:00Z"},  # no number
                {"number": 2},  # no createdAt
                {"number": 3, "createdAt": "not-a-date", "body": ""},
            ]
        )
        self.assertEqual(len(issues), 1)


class TestBuildReport(unittest.TestCase):
    def test_report_shape(self):
        report = score.build_report(score._parse_issues(_fixture_issues()))
        self.assertEqual(report["version"], "v2")
        self.assertEqual(report["total_issues"], 3)
        # Expected: 1 multi (307 + 310 share endpoint) + 1 singleton (279).
        self.assertEqual(report["multi_member_clusters"], 1)
        self.assertEqual(report["singleton_clusters"], 1)

    def test_multi_member_cluster_membership(self):
        report = score.build_report(score._parse_issues(_fixture_issues()))
        multi = [c for c in report["clusters"] if c["size"] >= 2]
        self.assertEqual(len(multi), 1)
        self.assertEqual(set(multi[0]["members"]), {307, 310})
        self.assertEqual(multi[0]["status_colors"], ["RED"])
        self.assertIn(
            "/api/sessions/{id}/orchestration/recommendations/sync",
            multi[0]["shared_endpoints"],
        )

    def test_template_only_does_not_cluster(self):
        report = score.build_report(score._parse_issues(_fixture_issues()))
        singletons = [c for c in report["clusters"] if c["size"] == 1]
        self.assertEqual(len(singletons), 1)
        self.assertEqual(singletons[0]["members"], [279])


class TestRenderMarkdown(unittest.TestCase):
    def test_markdown_contains_required_sections(self):
        report = score.build_report(score._parse_issues(_fixture_issues()))
        md = score.render_markdown(report)
        self.assertIn("# Discovery-digest v2 cluster report", md)
        self.assertIn("## Multi-member clusters", md)
        self.assertIn("## Singletons", md)
        self.assertIn("RED", md)
        self.assertIn("#279", md)

    def test_empty_report_renders_none(self):
        report = score.build_report([])
        md = score.render_markdown(report)
        self.assertIn("_(none)_", md)


class TestCli(unittest.TestCase):
    def test_main_json_output(self):
        with tempfile.TemporaryDirectory() as tmp:
            issues_path = Path(tmp) / "issues.json"
            issues_path.write_text(json.dumps(_fixture_issues()))
            buf = io.StringIO()
            with redirect_stdout(buf):
                rc = score.main(["--issues", str(issues_path), "--format", "json"])
            self.assertEqual(rc, 0)
            report = json.loads(buf.getvalue())
            self.assertEqual(report["total_issues"], 3)
            self.assertEqual(report["multi_member_clusters"], 1)

    def test_main_markdown_output(self):
        with tempfile.TemporaryDirectory() as tmp:
            issues_path = Path(tmp) / "issues.json"
            issues_path.write_text(json.dumps(_fixture_issues()))
            buf = io.StringIO()
            with redirect_stdout(buf):
                rc = score.main(
                    ["--issues", str(issues_path), "--format", "markdown"]
                )
            self.assertEqual(rc, 0)
            self.assertIn(
                "# Discovery-digest v2 cluster report", buf.getvalue()
            )

    def test_main_max_days_flag_changes_behaviour(self):
        # Set max_days=0 to force every cluster pair below threshold.
        with tempfile.TemporaryDirectory() as tmp:
            issues_path = Path(tmp) / "issues.json"
            issues_path.write_text(json.dumps(_fixture_issues()))
            buf = io.StringIO()
            with redirect_stdout(buf):
                rc = score.main(
                    ["--issues", str(issues_path), "--max-days", "0"]
                )
            self.assertEqual(rc, 0)
            report = json.loads(buf.getvalue())
            # max_days=0 means only same-day issues cluster; the two RED
            # signal issues are 1 day apart → singletons.
            self.assertEqual(report["multi_member_clusters"], 0)
            self.assertEqual(report["singleton_clusters"], 3)


if __name__ == "__main__":
    unittest.main()
