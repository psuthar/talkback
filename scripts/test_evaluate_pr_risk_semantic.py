#!/usr/bin/env python3
"""Unit tests for PR Risk semantic evaluation (CI gate mapping)."""

import json
import unittest
from pathlib import Path

from evaluate_pr_risk_semantic import build_semantic_record, normalize_rec


class TestNormalizeRec(unittest.TestCase):
    def test_uppercase(self):
        self.assertEqual(normalize_rec("warn"), "WARN")

    def test_invalid(self):
        self.assertIsNone(normalize_rec("maybe"))
        self.assertIsNone(normalize_rec(None))


class TestBuildSemanticRecord(unittest.TestCase):
    def test_pass(self):
        r = build_semantic_record(
            generator_outcome="success",
            pr_risk_path=Path("artifacts/pr-risk.json"),
            pr_risk_raw={
                "report_version": "v2.6",
                "score": 10.0,
                "band": "low",
                "merge_recommendation": "pass",  # normalized from generator JSON
                "required_validations": ["ci"],
                "top_risk_factors": ["a"],
            },
        )
        self.assertEqual(r["check_conclusion"], "success")
        self.assertEqual(r["semantic_conclusion"], "success")
        self.assertFalse(r["workflow_should_fail"])
        self.assertIn("PR Risk: PASS", r["title"])

    def test_warn(self):
        r = build_semantic_record(
            generator_outcome="success",
            pr_risk_path=Path("artifacts/pr-risk.json"),
            pr_risk_raw={
                "score": 28.0,
                "band": "medium",
                "merge_recommendation": "WARN",
                "required_validations": ["ci", "config", "test", "process"],
                "top_risk_factors": ["large diff"],
            },
        )
        self.assertEqual(r["check_conclusion"], "neutral")
        self.assertEqual(r["semantic_conclusion"], "neutral")
        self.assertFalse(r["workflow_should_fail"])

    def test_block(self):
        r = build_semantic_record(
            generator_outcome="success",
            pr_risk_path=Path("artifacts/pr-risk.json"),
            pr_risk_raw={
                "merge_recommendation": "BLOCK",
                "score": 80.0,
                "band": "high",
            },
        )
        self.assertEqual(r["check_conclusion"], "failure")
        self.assertTrue(r["workflow_should_fail"])

    def test_generator_crash(self):
        r = build_semantic_record(
            generator_outcome="failure",
            pr_risk_path=Path("artifacts/pr-risk.json"),
            pr_risk_raw=None,
        )
        self.assertEqual(r["check_conclusion"], "failure")
        self.assertTrue(r["workflow_should_fail"])

    def test_malformed_json_payload(self):
        r = build_semantic_record(
            generator_outcome="success",
            pr_risk_path=Path("artifacts/pr-risk.json"),
            pr_risk_raw={"merge_recommendation": "INVALID"},
        )
        self.assertEqual(r["check_conclusion"], "failure")
        self.assertTrue(r["workflow_should_fail"])


if __name__ == "__main__":
    unittest.main()
