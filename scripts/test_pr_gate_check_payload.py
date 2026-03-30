#!/usr/bin/env python3
"""Tests for pr_gate_check_payload.py."""

import json
import unittest
from pathlib import Path
import tempfile

import pr_gate
import pr_gate_check_payload as p


class TestConclusionMapping(unittest.TestCase):
    def test_pass(self):
        d = self._sample("PASS")
        out = p.build_payload_from_dict(d, run_url="https://example.com/run")
        self.assertEqual(out["check_conclusion"], "success")
        self.assertFalse(out["workflow_should_fail"])
        self.assertEqual(out["final_gate_status"], "PASS")
        self.assertIn("TalkBack PR Gate: PASS", out["title"])

    def test_warn(self):
        d = self._sample("WARN")
        out = p.build_payload_from_dict(d)
        self.assertEqual(out["check_conclusion"], "neutral")
        self.assertFalse(out["workflow_should_fail"])
        self.assertEqual(out["final_gate_status"], "WARN")

    def test_block(self):
        d = self._sample("BLOCK")
        out = p.build_payload_from_dict(d)
        self.assertEqual(out["check_conclusion"], "failure")
        self.assertTrue(out["workflow_should_fail"])

    def test_unknown_status(self):
        d = self._sample("PASS")
        d["final_gate"]["status"] = "nope"
        out = p.build_payload_from_dict(d)
        self.assertEqual(out["check_conclusion"], "failure")
        self.assertTrue(out["workflow_should_fail"])

    def test_error_payload(self):
        out = p.build_error_payload("missing file")
        self.assertEqual(out["check_conclusion"], "failure")
        self.assertTrue(out["workflow_should_fail"])
        self.assertIn("missing file", out["summary"])

    def _sample(self, gate_status: str) -> dict:
        return {
            "version": "v1",
            "pr_risk": {
                "status": "WARN",
                "label": "WARN",
                "score": 43.0,
                "band": "medium",
                "confidence": 63,
                "top_risk_factors": ["a", "b"],
            },
            "release_readiness": {
                "status": "WARN",
                "score": 90.0,
                "warnings": 2,
                "blockers": 0,
                "confidence": 80,
            },
            "final_gate": {
                "status": gate_status,
                "confidence": "moderate",
                "summary": pr_gate.GATE_SUMMARIES[gate_status],
            },
            "required_actions": ["CI checks must pass", "Review notes"],
        }


class TestWordingAlignment(unittest.TestCase):
    def test_summary_matches_gate_summaries(self):
        for st in ("PASS", "WARN", "BLOCK"):
            d = {
                "pr_risk": {"status": "PASS", "label": "PASS (low risk)", "score": 1.0, "band": "low"},
                "release_readiness": {
                    "status": "PASS",
                    "score": 100.0,
                    "warnings": 0,
                    "blockers": 0,
                    "confidence": 100,
                },
                "final_gate": {
                    "status": st,
                    "confidence": "high",
                    "summary": pr_gate.GATE_SUMMARIES[st],
                },
                "required_actions": [],
            }
            out = p.build_payload_from_dict(d)
            self.assertIn(pr_gate.GATE_SUMMARIES[st], out["summary"])

    def test_text_contains_signals_table(self):
        d = {
            "pr_risk": {
                "status": "WARN",
                "label": "WARN",
                "score": 43.0,
                "band": "medium",
                "confidence": 63,
                "top_risk_factors": ["large diff"],
            },
            "release_readiness": {
                "status": "WARN",
                "score": 90.0,
                "warnings": 2,
                "blockers": 0,
                "confidence": 85,
            },
            "final_gate": {
                "status": "WARN",
                "confidence": "moderate",
                "summary": pr_gate.GATE_SUMMARIES["WARN"],
            },
            "required_actions": ["CI checks must pass"],
        }
        out = p.build_payload_from_dict(d, run_url="https://github.com/r/a/actions/runs/1")
        self.assertIn("### Signals", out["text"])
        self.assertIn("PR Risk", out["text"])
        self.assertIn("Release Readiness", out["text"])
        self.assertIn("43", out["text"])
        self.assertIn("pr-gate-summary.md", out["text"])
        self.assertIn("https://github.com", out["text"])
        self.assertIn("### High priority", out["text"])
        self.assertIn(pr_gate.GATE_FOOTER_MARKDOWN, out["text"])


class TestCli(unittest.TestCase):
    def test_writes_file(self):
        with tempfile.TemporaryDirectory() as td:
            tdp = Path(td)
            gate = {
                "pr_risk": {"status": "PASS", "label": "PASS (low risk)", "score": 1.0, "band": "low"},
                "release_readiness": {
                    "status": "PASS",
                    "score": 100.0,
                    "warnings": 0,
                    "blockers": 0,
                    "confidence": 100,
                },
                "final_gate": {
                    "status": "PASS",
                    "confidence": "high",
                    "summary": pr_gate.GATE_SUMMARIES["PASS"],
                },
                "required_actions": [],
            }
            gj = tdp / "g.json"
            gj.write_text(json.dumps(gate), encoding="utf-8")
            out = tdp / "check.json"
            p.main(["--gate-json", str(gj), "--output", str(out)])
            loaded = json.loads(out.read_text(encoding="utf-8"))
            self.assertEqual(loaded["check_conclusion"], "success")


if __name__ == "__main__":
    unittest.main()
