#!/usr/bin/env python3
"""Unit tests for Q&A eval_cases / expected_scores validation."""

from __future__ import annotations

import sys
import tempfile
import unittest
from pathlib import Path

_REPO_ROOT = Path(__file__).resolve().parents[1]
if str(_REPO_ROOT) not in sys.path:
    sys.path.insert(0, str(_REPO_ROOT))

from scripts.qa_eval_datasets import (  # noqa: E402
    DEFAULT_EVAL_CASES,
    DEFAULT_EXPECTED_SCORES,
    DEFAULT_INVENTORY,
    SCHEMA_EVAL_CASES,
    SCHEMA_EXPECTED_SCORES,
    cross_check_eval_cases_inventory,
    cross_check_scores_vs_eval_cases,
    load_json,
    validate_against_schema,
    validate_eval_dataset_bundle,
)


class TestRepoDatasetsValidate(unittest.TestCase):
    def test_bundle_passes(self) -> None:
        errs = validate_eval_dataset_bundle()
        self.assertEqual(errs, [], "\n".join(errs))


class TestSchemaRejectsBadData(unittest.TestCase):
    def test_eval_cases_rejects_wrong_version(self) -> None:
        data = load_json(DEFAULT_EVAL_CASES)
        data["version"] = "2.0.0"
        errs = validate_against_schema(data, SCHEMA_EVAL_CASES)
        self.assertTrue(any("version" in m.lower() for m in errs))

    def test_expected_scores_rejects_too_few_targets(self) -> None:
        data = load_json(DEFAULT_EXPECTED_SCORES)
        data["case_targets"] = data["case_targets"][:10]
        errs = validate_against_schema(data, SCHEMA_EXPECTED_SCORES)
        self.assertTrue(errs)


class TestCrossChecks(unittest.TestCase):
    def test_inventory_mismatch_detected(self) -> None:
        eval_data = load_json(DEFAULT_EVAL_CASES)
        eval_data["cases"][0]["question"] = "tampered question"
        inv_data = load_json(DEFAULT_INVENTORY)
        errs = cross_check_eval_cases_inventory(eval_data, inv_data)
        self.assertTrue(any("question" in e for e in errs))

    def test_score_case_id_mismatch(self) -> None:
        eval_data = load_json(DEFAULT_EVAL_CASES)
        scores_data = load_json(DEFAULT_EXPECTED_SCORES)
        scores_data["case_targets"] = scores_data["case_targets"][:-1]
        errs = cross_check_scores_vs_eval_cases(scores_data, eval_data)
        self.assertTrue(errs)


class TestValidateBundleInventoryOptional(unittest.TestCase):
    def test_skips_inventory_when_disabled(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            inv = Path(tmp) / "inv.json"
            inv.write_text("not valid inventory", encoding="utf-8")
            errs = validate_eval_dataset_bundle(
                inventory_path=inv,
                require_inventory_alignment=False,
            )
            self.assertEqual(errs, [], errs)


if __name__ == "__main__":
    unittest.main()
