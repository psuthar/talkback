#!/usr/bin/env python3
"""Unit tests for Q&A fixture-fact inventory validation."""

from __future__ import annotations

import unittest
from pathlib import Path

from scripts.qa_fixture_inventory import load_inventory, validate_inventory


REPO_ROOT = Path(__file__).resolve().parents[1]
INVENTORY_PATH = REPO_ROOT / "eval" / "qa" / "fixture_fact_inventory.json"


class TestFixtureInventoryValidation(unittest.TestCase):
    def test_repo_inventory_is_valid(self) -> None:
        data = load_inventory(INVENTORY_PATH)
        errors = validate_inventory(data)
        self.assertEqual(errors, [], f"expected no validation errors, got: {errors}")

    def test_requires_minimum_case_count(self) -> None:
        data = {
            "cases": [
                {
                    "case_id": "FF-001",
                    "question": "Q1",
                    "expected_status": "answered",
                    "expected_keywords": ["x"],
                    "source_refs": [
                        {"source_file": "a.go", "evidence": "fact"},
                    ],
                }
            ]
        }
        errors = validate_inventory(data)
        self.assertTrue(any("at least 25" in err for err in errors))

    def test_answered_requires_keywords(self) -> None:
        base_case = {
            "case_id": "FF-001",
            "question": "Q1",
            "expected_status": "answered",
            "source_refs": [
                {"source_file": "a.go", "evidence": "fact"},
            ],
        }
        data = {"cases": [dict(base_case, case_id=f"FF-{i:03d}") for i in range(1, 26)]}
        errors = validate_inventory(data)
        self.assertTrue(
            any("expected_keywords" in err for err in errors),
            f"expected keyword validation error, got: {errors}",
        )


if __name__ == "__main__":
    unittest.main()
