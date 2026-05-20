#!/usr/bin/env python3
"""Unit tests for scripts/reviewer/calibration_summary.py (SCRUM-516)."""

from __future__ import annotations

import sys
import unittest
from pathlib import Path

_REPO_ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(_REPO_ROOT / "scripts"))

from reviewer.calibration_summary import (  # noqa: E402
    _gate_decision,
    render,
    summarise,
)


def _row(bucket: str, tokens: int = 1000, **extras) -> dict:
    base = {
        "pr": "100",
        "date": "2026-05-20",
        "bucket": bucket,
        "false_positives": "0",
        "tokens_used": str(tokens),
        "response_seconds": "30",
        "notes": "",
    }
    base.update(extras)
    return base


class SummariseTest(unittest.TestCase):
    def test_empty_csv_returns_zero_total(self):
        s = summarise([])
        self.assertEqual(s["total"], 0)
        self.assertEqual(s["tokens"], 0)

    def test_counts_each_bucket(self):
        rows = [
            _row("useful"),
            _row("useful"),
            _row("noisy"),
            _row("harmless"),
        ]
        s = summarise(rows)
        self.assertEqual(s["total"], 4)
        self.assertEqual(s["counts"]["useful"], 2)
        self.assertAlmostEqual(s["pct"]["useful"], 50.0)
        self.assertAlmostEqual(s["pct"]["noisy"], 25.0)
        self.assertAlmostEqual(s["pct"]["harmless"], 25.0)
        self.assertAlmostEqual(s["pct"]["harmful"], 0.0)

    def test_tokens_sum(self):
        rows = [_row("useful", tokens=1500), _row("useful", tokens=2500)]
        self.assertEqual(summarise(rows)["tokens"], 4000)

    def test_invalid_bucket_goes_to_skipped(self):
        rows = [_row("useful"), _row("", tokens=999), _row("wat")]
        s = summarise(rows)
        self.assertEqual(s["total"], 1)
        self.assertEqual(len(s["skipped_rows"]), 2)

    def test_token_parse_failure_does_not_crash(self):
        rows = [_row("useful", tokens=1000), _row("useful")]
        rows[1]["tokens_used"] = "not_a_number"
        s = summarise(rows)
        self.assertEqual(s["tokens"], 1000)


class GateDecisionTest(unittest.TestCase):
    def test_proceed_above_70(self):
        d = _gate_decision({"useful": 75.0}, {"useful": 15, "noisy": 5, "harmful": 0})
        self.assertIn("Phase 2 can proceed", d)

    def test_revise_between_50_and_70(self):
        d = _gate_decision({"useful": 60.0}, {"useful": 6, "noisy": 4, "harmful": 0})
        self.assertIn("Revise", d)

    def test_reframe_under_50(self):
        d = _gate_decision({"useful": 40.0}, {"useful": 4, "noisy": 6, "harmful": 0})
        self.assertIn("Reframe", d)

    def test_any_harmful_halts(self):
        # Even with high useful %, one harmful triggers HALT.
        d = _gate_decision({"useful": 90.0}, {"useful": 9, "harmful": 1})
        self.assertIn("HALT", d)


class RenderTest(unittest.TestCase):
    def test_empty_message_when_no_rows(self):
        out = render(summarise([]))
        self.assertIn("No calibration rows yet", out)

    def test_render_shows_gate_decision(self):
        rows = [_row("useful") for _ in range(10)]
        out = render(summarise(rows))
        self.assertIn("Phase 2 gate: Phase 2 can proceed", out)
        self.assertIn("100.0%", out)

    def test_render_warns_on_skipped_rows(self):
        rows = [_row("useful"), _row("not_a_bucket")]
        out = render(summarise(rows))
        self.assertIn("WARNING", out)


if __name__ == "__main__":
    unittest.main()
