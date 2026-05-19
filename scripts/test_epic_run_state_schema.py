#!/usr/bin/env python3
"""Unit tests for scripts/epic_run_state_schema.py (SCRUM-489)."""

from __future__ import annotations

import sys
import unittest
from pathlib import Path

_REPO_ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(_REPO_ROOT / "scripts"))

from epic_run_state_schema import (  # noqa: E402
    HALT_CATEGORY_VALUES,
    MAX_ESTIMATED_LOC_MAX,
    MAX_ESTIMATED_LOC_MIN,
    VALID_STATUSES,
    validate_halt_category,
    validate_max_estimated_loc,
    validate_state,
    validate_status,
)


class TestHaltCategoryEnum(unittest.TestCase):
    def test_enum_has_exact_expected_values(self):
        expected = {
            "spec_missing",
            "gate_warn",
            "gate_block",
            "mergeable_blocked",
            "timeout",
            "human_requested_halt",
            "other",
        }
        self.assertEqual(HALT_CATEGORY_VALUES, expected)

    def test_validate_halt_category_accepts_every_enum_value(self):
        for v in HALT_CATEGORY_VALUES:
            self.assertIsNone(validate_halt_category(v), f"rejected {v!r}")

    def test_validate_halt_category_accepts_none(self):
        self.assertIsNone(validate_halt_category(None))

    def test_validate_halt_category_rejects_unknown_string(self):
        err = validate_halt_category("not_in_enum")
        self.assertIsNotNone(err)
        self.assertIn("not_in_enum", err)

    def test_validate_halt_category_rejects_wrong_type(self):
        self.assertIsNotNone(validate_halt_category(42))


class TestValidateState(unittest.TestCase):
    def test_legacy_state_no_halt_category_is_valid(self):
        state = {
            "epic": "SCRUM-100",
            "status": "complete",
            "halt_reason": None,
            "tickets": [
                {"key": "SCRUM-A", "status": "done", "pr": 1, "merged_sha": "abc"},
            ],
        }
        self.assertEqual(validate_state(state), [])

    def test_new_halt_with_category_is_valid(self):
        state = {
            "epic": "SCRUM-200",
            "halt_category": "gate_warn",
            "halt_reason": "WARN at 18:44Z",
            "tickets": [
                {"key": "SCRUM-B", "status": "halted", "halt_category": "gate_warn"},
            ],
        }
        self.assertEqual(validate_state(state), [])

    def test_root_unknown_category_reports_error(self):
        errors = validate_state({"halt_category": "invalid"})
        self.assertEqual(len(errors), 1)
        self.assertIn("root", errors[0])
        self.assertIn("invalid", errors[0])

    def test_ticket_unknown_category_reports_error(self):
        errors = validate_state(
            {"tickets": [{"key": "S-1", "halt_category": "nope"}]}
        )
        self.assertEqual(len(errors), 1)
        self.assertIn("tickets[0]", errors[0])
        self.assertIn("'S-1'", errors[0])

    def test_other_root_requires_halt_reason(self):
        errors = validate_state({"halt_category": "other"})
        self.assertTrue(
            any("halt_reason" in e and "other" in e for e in errors),
            f"missing other-requires-halt_reason error: {errors}",
        )

    def test_other_ticket_requires_halt_reason(self):
        state = {
            "tickets": [{"key": "S-X", "halt_category": "other"}],
        }
        errors = validate_state(state)
        self.assertTrue(
            any("tickets[0]" in e and "halt_reason" in e for e in errors),
            f"missing per-ticket other check: {errors}",
        )

    def test_other_with_halt_reason_is_valid(self):
        state = {
            "halt_category": "other",
            "halt_reason": "GitHub MCP returned 500 at 18:50Z",
            "tickets": [],
        }
        self.assertEqual(validate_state(state), [])

    def test_non_dict_state_reports_error(self):
        errors = validate_state(["not", "a", "dict"])
        self.assertEqual(len(errors), 1)
        self.assertIn("dict", errors[0])

    def test_non_list_tickets_reports_error(self):
        errors = validate_state({"tickets": "should be a list"})
        self.assertTrue(any("tickets must be a list" in e for e in errors))

    def test_non_dict_ticket_entry_reports_error(self):
        errors = validate_state({"tickets": [42]})
        self.assertTrue(any("tickets[0]" in e for e in errors))


class TestStatusEnum(unittest.TestCase):
    """SCRUM-493: extended status enum."""

    def test_valid_statuses(self):
        expected = {"authoring", "awaiting_approval", "running", "halted", "complete"}
        self.assertEqual(VALID_STATUSES, expected)

    def test_validate_status_accepts_each(self):
        for s in VALID_STATUSES:
            self.assertIsNone(validate_status(s))

    def test_validate_status_accepts_none(self):
        self.assertIsNone(validate_status(None))

    def test_validate_status_rejects_unknown(self):
        err = validate_status("draft")
        self.assertIsNotNone(err)
        self.assertIn("draft", err)


class TestMaxEstimatedLOC(unittest.TestCase):
    """SCRUM-493: max_estimated_loc bounds."""

    def test_constants(self):
        self.assertEqual(MAX_ESTIMATED_LOC_MIN, 100)
        self.assertEqual(MAX_ESTIMATED_LOC_MAX, 800)

    def test_none_is_valid(self):
        self.assertIsNone(validate_max_estimated_loc(None))

    def test_lower_bound_inclusive(self):
        self.assertIsNone(validate_max_estimated_loc(100))

    def test_below_lower_bound_rejected(self):
        err = validate_max_estimated_loc(99)
        self.assertIsNotNone(err)
        self.assertIn("99", err)

    def test_upper_bound_inclusive(self):
        self.assertIsNone(validate_max_estimated_loc(800))

    def test_above_upper_bound_rejected(self):
        err = validate_max_estimated_loc(801)
        self.assertIsNotNone(err)
        self.assertIn("801", err)

    def test_default_in_range(self):
        self.assertIsNone(validate_max_estimated_loc(400))

    def test_string_rejected(self):
        err = validate_max_estimated_loc("400")
        self.assertIsNotNone(err)
        self.assertIn("must be int", err)

    def test_bool_rejected(self):
        # bool is an int subclass in Python; explicitly excluded.
        err = validate_max_estimated_loc(True)
        self.assertIsNotNone(err)
        self.assertIn("must be int", err)

    def test_validate_state_includes_max_loc(self):
        # In-range value passes through validate_state.
        state = {"status": "running", "max_estimated_loc": 200, "tickets": []}
        self.assertEqual(validate_state(state), [])

    def test_validate_state_rejects_out_of_range_max_loc(self):
        state = {"max_estimated_loc": 50, "tickets": []}
        errors = validate_state(state)
        self.assertTrue(any("max_estimated_loc" in e for e in errors))


class TestRealRepoStateFiles(unittest.TestCase):
    """Sanity: the 17 existing .epic-run/*.json files must all validate as legacy."""

    def test_existing_state_files_validate(self):
        import json
        epic_run_dir = _REPO_ROOT / ".epic-run"
        if not epic_run_dir.is_dir():
            self.skipTest(".epic-run/ not present")
        files = list(epic_run_dir.glob("*.json"))
        self.assertGreater(len(files), 0, "expected at least one .epic-run file")
        for f in files:
            try:
                state = json.loads(f.read_text())
            except json.JSONDecodeError as exc:
                self.fail(f"{f.name}: invalid JSON: {exc}")
            errors = validate_state(state)
            self.assertEqual(
                errors, [], f"{f.name}: legacy file failed validation: {errors}"
            )


if __name__ == "__main__":
    unittest.main()
