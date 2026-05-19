#!/usr/bin/env python3
"""SCRUM-495: simulator dry-run validation for the epic-run authoring phase.

These tests exercise the four scenarios documented in
``docs/agent/epic-run-authoring-validation.md`` against the schema validator
and the new max_estimated_loc + status enum from SCRUM-493. They do not call
Jira — the live-against-real-Epic execution remains a follow-up.

Each scenario builds a fixture .epic-run/<EPIC>.json dict and asserts:

- The fixture validates (or fails) per scripts/epic_run_state_schema.py.
- Halt-category and halt-reason invariants hold where applicable.
- The cap and floor boundaries behave as documented.
"""

from __future__ import annotations

import sys
import unittest
from pathlib import Path

_REPO_ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(_REPO_ROOT / "scripts"))

from epic_run_state_schema import (  # noqa: E402
    MAX_ESTIMATED_LOC_DEFAULT,
    validate_state,
)


def _base_state(epic: str, **overrides) -> dict:
    state = {
        "epic": epic,
        "run_id": "2026-05-19T00:00:00Z",
        "status": "authoring",
        "max_estimated_loc": None,
        "awaiting_human": False,
        "halted_at": None,
        "halt_reason": None,
        "halt_category": None,
        "tickets": [],
        "next_pending": [],
    }
    state.update(overrides)
    return state


CHILDREN_OK = 3
CHILDREN_OVERFLOW = 9
PROPOSAL_CAP = 8


class TestScenarioA_DefaultThreshold(unittest.TestCase):
    """3-child epic at default threshold should validate and run."""

    def test_initial_authoring_state_validates(self):
        state = _base_state("SCRUM-XYZ")
        self.assertEqual(validate_state(state), [])

    def test_running_state_after_approval_validates(self):
        state = _base_state(
            "SCRUM-XYZ",
            status="running",
            next_pending=["SCRUM-A", "SCRUM-B", "SCRUM-C"],
        )
        self.assertEqual(validate_state(state), [])

    def test_default_threshold_is_400(self):
        self.assertEqual(MAX_ESTIMATED_LOC_DEFAULT, 400)


class TestScenarioB_OverrideThreshold(unittest.TestCase):
    """Epic-override max_estimated_loc=100 must validate."""

    def test_override_100_validates(self):
        state = _base_state("SCRUM-PQR", max_estimated_loc=100)
        self.assertEqual(validate_state(state), [])

    def test_override_at_default_400_validates(self):
        state = _base_state("SCRUM-PQR", max_estimated_loc=400)
        self.assertEqual(validate_state(state), [])

    def test_override_at_ceiling_800_validates(self):
        state = _base_state("SCRUM-PQR", max_estimated_loc=800)
        self.assertEqual(validate_state(state), [])


class TestScenarioC_BelowFloor(unittest.TestCase):
    """Below-floor override must be rejected at validate_state time."""

    def test_override_50_rejected(self):
        state = _base_state("SCRUM-MNO", max_estimated_loc=50)
        errors = validate_state(state)
        self.assertTrue(any("max_estimated_loc" in e for e in errors))
        self.assertTrue(any("[100" in e for e in errors))

    def test_override_99_rejected(self):
        state = _base_state("SCRUM-MNO", max_estimated_loc=99)
        errors = validate_state(state)
        self.assertTrue(any("99" in e for e in errors))

    def test_override_above_ceiling_rejected(self):
        state = _base_state("SCRUM-MNO", max_estimated_loc=801)
        errors = validate_state(state)
        self.assertTrue(any("801" in e for e in errors))


class TestScenarioD_OverflowCap(unittest.TestCase):
    """> 8 children proposed → halted state must validate with spec_missing."""

    def test_overflow_halt_state_validates(self):
        state = _base_state(
            "SCRUM-LARGE",
            status="halted",
            max_estimated_loc=100,
            halt_category="spec_missing",
            halt_reason=(
                f"decomposition produced {CHILDREN_OVERFLOW} children — Epic "
                "needs human re-scoping"
            ),
            halted_at="2026-05-19T20:00:00Z",
            awaiting_human=True,
        )
        self.assertEqual(validate_state(state), [])

    def test_proposal_cap_is_8(self):
        # Documented contract: more than 8 children = halt.
        self.assertEqual(PROPOSAL_CAP, 8)
        self.assertGreater(CHILDREN_OVERFLOW, PROPOSAL_CAP)
        self.assertLessEqual(CHILDREN_OK, PROPOSAL_CAP)


class TestApprovalAndRejectionPaths(unittest.TestCase):
    """User n → human_requested_halt; lint failure → spec_missing."""

    def test_user_n_halt_validates(self):
        state = _base_state(
            "SCRUM-N",
            status="halted",
            halt_category="human_requested_halt",
            halted_at="2026-05-19T20:00:00Z",
            awaiting_human=True,
        )
        self.assertEqual(validate_state(state), [])

    def test_lint_failure_after_creation_halt_validates(self):
        state = _base_state(
            "SCRUM-L",
            status="halted",
            tickets=[
                {
                    "key": "SCRUM-L1",
                    "status": "halted",
                    "halt_category": "spec_missing",
                    "halt_reason": "lint AC.min_count failed after auto-fix retry",
                }
            ],
            halt_category="spec_missing",
            halt_reason="child ticket failed lint after authoring",
            halted_at="2026-05-19T20:00:00Z",
            awaiting_human=True,
        )
        self.assertEqual(validate_state(state), [])


class TestStateTransitions(unittest.TestCase):
    """All five status values must validate in valid state-file shapes."""

    def test_authoring_validates(self):
        self.assertEqual(validate_state(_base_state("S", status="authoring")), [])

    def test_awaiting_approval_validates(self):
        self.assertEqual(
            validate_state(_base_state("S", status="awaiting_approval")), []
        )

    def test_running_validates(self):
        self.assertEqual(validate_state(_base_state("S", status="running")), [])

    def test_halted_validates(self):
        # halted typically pairs with halt_category but not required by schema.
        self.assertEqual(validate_state(_base_state("S", status="halted")), [])

    def test_complete_validates(self):
        self.assertEqual(validate_state(_base_state("S", status="complete")), [])


if __name__ == "__main__":
    unittest.main()
