#!/usr/bin/env python3
"""Unit tests for scripts/define_kpi_snapshot.py (SCRUM-488)."""

from __future__ import annotations

import json
import os
import sys
import tempfile
import unittest
from datetime import datetime, timezone
from pathlib import Path

_REPO_ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(_REPO_ROOT / "scripts"))

import define_kpi_snapshot as snap  # noqa: E402


def _write_json(p: Path, data) -> None:
    p.parent.mkdir(parents=True, exist_ok=True)
    p.write_text(json.dumps(data))


class TestKpiSnapshot(unittest.TestCase):
    def setUp(self) -> None:
        self.tmp = tempfile.TemporaryDirectory()
        self.root = Path(self.tmp.name)
        self.now = datetime(2026, 5, 19, 18, 38, 45, tzinfo=timezone.utc)

    def tearDown(self) -> None:
        self.tmp.cleanup()

    def _lint_log(self, lines):
        path = self.root / "lint-runs.log"
        if lines is None:
            return path
        path.write_text("\n".join(lines) + ("\n" if lines else ""))
        return path

    def test_schema_has_required_fields(self):
        out = snap.build_snapshot(
            issues=[], epic_run_states=[], lint_log=self._lint_log(None), now=self.now
        )
        for field in snap.REQUIRED_FIELDS:
            self.assertIn(field, out, f"missing field: {field}")
        self.assertIn("raw", out)
        self.assertEqual(out["timestamp"], "2026-05-19T18:38:45Z")

    def test_lint_pass_rate_missing_log(self):
        out = snap.build_snapshot(
            issues=[], epic_run_states=[], lint_log=self._lint_log(None), now=self.now
        )
        self.assertIsNone(out["lint_pass_rate"])
        self.assertEqual(out["raw"]["lint_runs_total"], 0)

    def test_lint_pass_rate_computes_ratio(self):
        lines = [
            json.dumps({"ticket": "SCRUM-1", "exit": 0}),
            json.dumps({"ticket": "SCRUM-2", "exit": 0}),
            json.dumps({"ticket": "SCRUM-3", "exit": 0}),
            json.dumps({"ticket": "SCRUM-4", "exit": 2}),
        ]
        out = snap.build_snapshot(
            issues=[], epic_run_states=[], lint_log=self._lint_log(lines), now=self.now
        )
        self.assertAlmostEqual(out["lint_pass_rate"], 0.75)
        self.assertEqual(out["raw"]["lint_passes"], 3)
        self.assertEqual(out["raw"]["lint_runs_total"], 4)

    def test_lint_log_tolerates_garbage(self):
        lines = ["not json", "", json.dumps({"exit": 0}), json.dumps({})]
        out = snap.build_snapshot(
            issues=[], epic_run_states=[], lint_log=self._lint_log(lines), now=self.now
        )
        self.assertEqual(out["raw"]["lint_runs_total"], 1)
        self.assertEqual(out["raw"]["lint_passes"], 1)

    def test_agent_authoring_pct(self):
        issues = [
            {"key": "S-1", "fields": {"labels": ["agent-authored", "phase-1"]}},
            {"key": "S-2", "fields": {"labels": ["agent-authored"]}},
            {"key": "S-3", "fields": {"labels": []}},
            {"key": "S-4", "fields": {"labels": ["other"]}},
        ]
        out = snap.build_snapshot(
            issues=issues, epic_run_states=[], lint_log=self._lint_log(None), now=self.now
        )
        self.assertAlmostEqual(out["agent_authoring_pct"], 0.5)
        self.assertEqual(out["raw"]["agent_authored_count"], 2)
        self.assertEqual(out["raw"]["total_tickets_sampled"], 4)

    def test_agent_authoring_pct_empty_returns_zero(self):
        out = snap.build_snapshot(
            issues=[], epic_run_states=[], lint_log=self._lint_log(None), now=self.now
        )
        self.assertEqual(out["agent_authoring_pct"], 0.0)
        self.assertEqual(out["raw"]["total_tickets_sampled"], 0)

    def test_source_obs_agent_count(self):
        issues = [
            {"fields": {"labels": ["source:obs-agent"]}},
            {"fields": {"labels": ["source:obs-agent", "phase-3"]}},
            {"fields": {"labels": []}},
        ]
        out = snap.build_snapshot(
            issues=issues, epic_run_states=[], lint_log=self._lint_log(None), now=self.now
        )
        self.assertEqual(out["source_obs_agent_count"], 2)

    def test_spec_halt_count_per_ticket(self):
        states = [
            {"epic": "SCRUM-1", "tickets": [
                {"key": "SCRUM-10", "halt_category": "spec_missing"},
                {"key": "SCRUM-11", "halt_category": "gate_warn"},
            ]},
            {"epic": "SCRUM-2", "tickets": [
                {"key": "SCRUM-20", "halt_category": "spec_missing"},
            ]},
            # Legacy state file without halt_category — must not crash.
            {"epic": "SCRUM-3", "tickets": [{"key": "SCRUM-30", "status": "done"}]},
        ]
        out = snap.build_snapshot(
            issues=[], epic_run_states=states, lint_log=self._lint_log(None), now=self.now
        )
        self.assertEqual(out["spec_halt_count"], 2)
        self.assertEqual(out["raw"]["halts_by_category"]["spec_missing"], 2)
        self.assertEqual(out["raw"]["halts_by_category"]["gate_warn"], 1)

    def test_spec_halt_count_root_level(self):
        states = [{"epic": "SCRUM-1", "halt_category": "spec_missing", "tickets": []}]
        out = snap.build_snapshot(
            issues=[], epic_run_states=states, lint_log=self._lint_log(None), now=self.now
        )
        self.assertEqual(out["spec_halt_count"], 1)

    def test_read_jira_issues_accepts_list_or_object(self):
        path_obj = self.root / "obj.json"
        _write_json(path_obj, {"issues": [{"key": "S-1"}]})
        self.assertEqual(snap._read_jira_issues(path_obj), [{"key": "S-1"}])

        path_list = self.root / "list.json"
        _write_json(path_list, [{"key": "S-2"}])
        self.assertEqual(snap._read_jira_issues(path_list), [{"key": "S-2"}])

    def test_resolve_output_path_collision_uses_timestamp(self):
        cwd = os.getcwd()
        try:
            os.chdir(self.tmp.name)
            (Path("ops/define-kpis")).mkdir(parents=True, exist_ok=True)
            Path("ops/define-kpis/snapshot-2026-05-19.json").write_text("{}")
            out = snap._resolve_output_path(None, self.now)
            self.assertNotEqual(out.name, "snapshot-2026-05-19.json")
            self.assertTrue(out.name.startswith("snapshot-2026-05-19T"))
        finally:
            os.chdir(cwd)


if __name__ == "__main__":
    unittest.main()
