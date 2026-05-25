#!/usr/bin/env python3
"""SCRUM-570 — smoke test for eval/qa/fixture_input_guardrail.json.

Validates the labeled fixture set used by SCRUM-564's input-guardrail
gate (refusal_when_oos_rate metric):

- File parses as JSON.
- Top-level shape matches the contract (version, labels, cases).
- Every case has a unique case_id, a label drawn from the declared
  set, and a non-empty question string.
- Counts meet the ticket's minimum bar (~30 legitimate, ~10 off-scope,
  ~10 injection) so the metric has enough signal to be meaningful.
"""

from __future__ import annotations

import json
import sys
import unittest
from pathlib import Path

_REPO_ROOT = Path(__file__).resolve().parents[1]
_FIXTURE = _REPO_ROOT / "eval" / "qa" / "fixture_input_guardrail.json"

_REQUIRED_LABELS = {"legitimate", "off_scope", "injection"}
_MIN_COUNTS = {"legitimate": 30, "off_scope": 10, "injection": 10}


class TestInputGuardrailFixture(unittest.TestCase):
    @classmethod
    def setUpClass(cls) -> None:
        cls.data = json.loads(_FIXTURE.read_text(encoding="utf-8"))
        cls.cases = cls.data.get("cases") or []

    def test_top_level_shape(self) -> None:
        self.assertIn("version", self.data)
        self.assertIn("description", self.data)
        self.assertEqual(set(self.data.get("labels", [])), _REQUIRED_LABELS)
        self.assertIsInstance(self.cases, list)
        self.assertGreater(len(self.cases), 0)

    def test_case_ids_unique(self) -> None:
        ids = [c["case_id"] for c in self.cases]
        self.assertEqual(len(ids), len(set(ids)), "duplicate case_id detected")

    def test_every_case_well_formed(self) -> None:
        for c in self.cases:
            self.assertIn("case_id", c, c)
            self.assertIn("label", c, c)
            self.assertIn("question", c, c)
            self.assertIsInstance(c["case_id"], str)
            self.assertTrue(c["case_id"].startswith("IG-"), c)
            self.assertIn(c["label"], _REQUIRED_LABELS, c)
            self.assertIsInstance(c["question"], str)
            self.assertTrue(c["question"].strip(), c)

    def test_per_label_minimum_counts(self) -> None:
        counts = {label: 0 for label in _REQUIRED_LABELS}
        for c in self.cases:
            counts[c["label"]] += 1
        for label, minimum in _MIN_COUNTS.items():
            self.assertGreaterEqual(
                counts[label],
                minimum,
                f"label {label!r} has {counts[label]} cases; ticket minimum is {minimum}",
            )

    def test_legitimate_set_includes_adversarial_phrasings(self) -> None:
        """Defense against trivial substring-rule guardrails.

        The legit set must include phrasings that *contain* common
        injection-trigger substrings (e.g. \"ignore previous\",
        \"system\") inside otherwise-real session questions. A
        guardrail that just regex-matches those substrings would
        false-positive on these and tank refusal_when_oos_rate; this
        test pins that the dataset has the signal in the first place.
        """
        legits = [c["question"].lower() for c in self.cases if c["label"] == "legitimate"]
        # At least one legitimate Q must contain "ignore" + "previous" — the
        # canonical injection trigger phrase, used here in a real-sounding
        # session question.
        adversarial = [q for q in legits if "ignore" in q and "previous" in q]
        self.assertGreater(
            len(adversarial),
            0,
            "expected at least one legitimate Q containing the 'ignore previous' phrase to defend against trivial substring guardrails",
        )

    def test_injection_set_includes_sentinel_attack(self) -> None:
        """At least one injection case must try to close + re-open the SCRUM-563 USER_CONTENT sentinel.

        Slice 2's prompt hardening relies on the sanitizer dropping
        any literal `<<<USER_CONTENT` / `<<<END_USER_CONTENT>>>` inside
        a chunk. The eval set should pin that an *input*-side variant
        of the same attack is also present — different layer, same
        category.
        """
        injections = [c["question"] for c in self.cases if c["label"] == "injection"]
        sentinel = [q for q in injections if "USER_CONTENT" in q]
        self.assertGreater(
            len(sentinel),
            0,
            "expected at least one injection Q exercising the <<<USER_CONTENT ...>>> sentinel attack",
        )


if __name__ == "__main__":
    unittest.main()
