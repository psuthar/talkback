#!/usr/bin/env python3
"""Unit tests for pr_gate.py — unified PR gate combiner."""
import json
import os
import tempfile
import unittest
from pathlib import Path

import sys
sys.path.insert(0, str(Path(__file__).parent))

from pr_gate import (
    GATE_SUMMARIES,
    PRIORITY_ACTION_KEYS,
    STANDARD_ACTIONS,
    PRRiskInput,
    ReadinessInput,
    build_gate_json,
    build_gate_markdown,
    build_required_actions,
    classify_gate_confidence,
    compute_gate_status,
    derive_rr_confidence,
    load_pr_risk,
    load_release_readiness,
    normalize_action,
    normalize_status,
    run,
    REC_DISPLAY,
)


# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------


def _make_risk(
    status: str = "PASS",
    score: float = 5.0,
    band: str = "low",
    req_val: list | None = None,
    factors: list | None = None,
    confidence: int | None = None,
) -> PRRiskInput:
    return PRRiskInput(
        status=status,
        score=score,
        band=band,
        label=REC_DISPLAY.get(status, status),
        confidence=confidence,
        required_validations=req_val or [],
        top_risk_factors=factors or [],
    )


def _make_rr(
    status: str = "PASS",
    score: float = 100.0,
    warnings: int = 0,
    blockers: int = 0,
    blocker_msgs: list | None = None,
    warning_msgs: list | None = None,
    recommended: list | None = None,
) -> ReadinessInput:
    return ReadinessInput(
        status=status,
        score=score,
        warnings_count=warnings,
        blockers_count=blockers,
        blocker_messages=blocker_msgs or [],
        warning_messages=warning_msgs or [],
        recommended_actions=recommended or [],
    )


class TestNormalizeStatus(unittest.TestCase):
    def test_valid_pass(self):
        self.assertEqual(normalize_status("pass"), "PASS")
        self.assertEqual(normalize_status("PASS"), "PASS")
        self.assertEqual(normalize_status("  Pass  "), "PASS")

    def test_valid_warn(self):
        self.assertEqual(normalize_status("warn"), "WARN")

    def test_valid_block(self):
        self.assertEqual(normalize_status("block"), "BLOCK")

    def test_invalid_raises(self):
        with self.assertRaises(ValueError):
            normalize_status("MAYBE")

    def test_empty_raises(self):
        with self.assertRaises(ValueError):
            normalize_status("")


# ---------------------------------------------------------------------------
# Gate combining logic — §8 requirements (all 7 combos + extras)
# ---------------------------------------------------------------------------


class TestComputeGateStatus(unittest.TestCase):

    def test_pass_pass(self):
        self.assertEqual(compute_gate_status("PASS", "PASS"), "PASS")

    def test_pass_warn(self):
        # PR risk WARN, RR PASS → WARN
        self.assertEqual(compute_gate_status("PASS", "WARN"), "WARN")

    def test_warn_pass(self):
        # RR WARN, PR risk PASS → WARN
        self.assertEqual(compute_gate_status("WARN", "PASS"), "WARN")

    def test_warn_warn(self):
        self.assertEqual(compute_gate_status("WARN", "WARN"), "WARN")

    def test_block_pass(self):
        # RR BLOCK, PR risk PASS → BLOCK
        self.assertEqual(compute_gate_status("BLOCK", "PASS"), "BLOCK")

    def test_pass_block(self):
        # RR PASS, PR risk BLOCK → BLOCK
        self.assertEqual(compute_gate_status("PASS", "BLOCK"), "BLOCK")

    def test_block_block(self):
        self.assertEqual(compute_gate_status("BLOCK", "BLOCK"), "BLOCK")

    def test_block_warn(self):
        # BLOCK wins over WARN from either side.
        self.assertEqual(compute_gate_status("BLOCK", "WARN"), "BLOCK")
        self.assertEqual(compute_gate_status("WARN", "BLOCK"), "BLOCK")


# ---------------------------------------------------------------------------
# Required actions
# ---------------------------------------------------------------------------


class TestRequiredActions(unittest.TestCase):

    def test_standard_actions_always_present(self):
        actions = build_required_actions(_make_risk(), _make_rr())
        for sa in STANDARD_ACTIONS:
            self.assertIn(sa, actions)

    def test_standard_actions_appear_first(self):
        risk = _make_risk(req_val=["run targeted regression"])
        actions = build_required_actions(risk, _make_rr())
        sa_max_idx = max(actions.index(sa) for sa in STANDARD_ACTIONS)
        target_idx = next(i for i, a in enumerate(actions) if "regression" in a.lower())
        self.assertLess(sa_max_idx, target_idx)

    def test_pr_risk_validations_before_rr_blockers(self):
        risk = _make_risk(req_val=["run targeted regression"])
        rr = _make_rr(blocker_msgs=["deploy to staging first"])
        actions = build_required_actions(risk, rr)
        risk_idx = next(i for i, a in enumerate(actions) if "regression" in a.lower())
        rr_idx = next(i for i, a in enumerate(actions) if "staging" in a.lower())
        self.assertLess(risk_idx, rr_idx)

    def test_deduplication_exact_match(self):
        risk = _make_risk(req_val=["CI checks must pass before merge"])
        rr = _make_rr(blocker_msgs=["CI checks must pass before merge"])
        actions = build_required_actions(risk, rr)
        ci_count = sum(1 for a in actions if "ci checks must pass" in a.lower())
        self.assertEqual(ci_count, 1, f"Expected 1, got {ci_count} in {actions}")

    def test_deduplication_trailing_period(self):
        """Items differing only by trailing period deduplicate."""
        risk = _make_risk(req_val=["Review migration scripts."])
        rr = _make_rr(blocker_msgs=["Review migration scripts"])
        actions = build_required_actions(risk, rr)
        count = sum(1 for a in actions if "review migration" in a.lower())
        self.assertEqual(count, 1)

    def test_deduplication_case_insensitive(self):
        risk = _make_risk(req_val=["Run E2E Tests"])
        rr = _make_rr(recommended=["run e2e tests"])
        actions = build_required_actions(risk, rr)
        count = sum(1 for a in actions if "e2e" in a.lower())
        self.assertEqual(count, 1)

    def test_empty_sources_returns_standard_actions_only(self):
        actions = build_required_actions(_make_risk(), _make_rr())
        self.assertEqual(actions, STANDARD_ACTIONS)


# ---------------------------------------------------------------------------
# Markdown semantics
# ---------------------------------------------------------------------------


class TestMarkdownSemantics(unittest.TestCase):

    def test_pass_mentions_prerequisites_and_disclaimer(self):
        """PASS markdown must not imply unconditional merge approval."""
        md = build_gate_markdown(_make_risk("PASS"), _make_rr("PASS"), "PASS", STANDARD_ACTIONS)
        self.assertIn("prerequisites", md.lower())
        self.assertIn("does not bypass branch protection", md)

    def test_block_wording_is_unambiguous(self):
        md = build_gate_markdown(_make_risk("BLOCK", score=75), _make_rr("PASS"), "BLOCK", STANDARD_ACTIONS)
        self.assertIn("Do not merge", md)

    def test_warn_is_cautionary_not_hard_stop(self):
        md = build_gate_markdown(_make_risk("WARN"), _make_rr("PASS"), "WARN", STANDARD_ACTIONS)
        self.assertIn("Not blocked", md)
        self.assertNotIn("Do not merge", md)

    def test_table_contains_all_three_signals(self):
        md = build_gate_markdown(_make_risk("PASS"), _make_rr("WARN", score=72.0), "WARN", STANDARD_ACTIONS)
        self.assertIn("PR Risk", md)
        self.assertIn("Release Readiness", md)
        self.assertIn("Final Gate", md)

    def test_rr_score_appears_in_table(self):
        md = build_gate_markdown(_make_risk("PASS"), _make_rr("PASS", score=85.0), "PASS", STANDARD_ACTIONS)
        self.assertIn("85", md)

    def test_required_actions_listed(self):
        actions = STANDARD_ACTIONS + ["run smoke tests"]
        md = build_gate_markdown(_make_risk("WARN"), _make_rr("WARN"), "WARN", actions)
        self.assertIn("run smoke tests", md)
        for sa in STANDARD_ACTIONS:
            self.assertIn(sa, md)

    def test_warn_decision_text_is_directive(self):
        """WARN decision text must say 'not blocked' and 'complete the required validations'."""
        md = build_gate_markdown(_make_risk("WARN"), _make_rr("WARN"), "WARN", STANDARD_ACTIONS)
        self.assertIn("Not blocked", md)
        self.assertIn("Complete", md)

    def test_footer_describes_warn_and_block_semantics(self):
        """Footer must mention WARN and BLOCK semantics alongside PASS disclaimer."""
        md = build_gate_markdown(_make_risk("PASS"), _make_rr("PASS"), "PASS", STANDARD_ACTIONS)
        self.assertIn("does not bypass branch protection", md)
        self.assertIn("WARN", md)
        self.assertIn("BLOCK", md)
        self.assertIn("do not merge", md.lower())

    def test_confidence_appears_in_supporting_detail(self):
        """When risk confidence is set, supporting detail shows all confidence lines."""
        md = build_gate_markdown(_make_risk("PASS", confidence=75), _make_rr("PASS", score=90.0), "PASS", [])
        self.assertIn("PR Risk test confidence", md)
        self.assertIn("75", md)
        self.assertIn("Release Readiness confidence", md)
        self.assertIn("Gate confidence", md)

    def test_no_pr_risk_confidence_line_when_none(self):
        """When risk.confidence is None, PR Risk test confidence line is omitted."""
        md = build_gate_markdown(_make_risk("PASS", confidence=None), _make_rr("PASS"), "PASS", [])
        self.assertNotIn("PR Risk test confidence", md)
        # But RR and gate confidence lines still present.
        self.assertIn("Release Readiness confidence", md)
        self.assertIn("Gate confidence", md)

    def test_no_internal_prefixes_in_markdown(self):
        """Taxonomy labels (ci:, config:, test:, process:) must not appear in rendered markdown."""
        risk = _make_risk("WARN", req_val=[
            "ci: required status checks must pass before merge",
            "config: workflow / deploy / go.mod changes validated against required checks",
            "test: run targeted regression on hotspot area",
            "process: PR description with scoped, evidence-backed review plan",
        ])
        actions = build_required_actions(risk, _make_rr())
        md = build_gate_markdown(risk, _make_rr("WARN"), "WARN", actions)
        for prefix in ("ci:", "config:", "test:", "process:"):
            self.assertNotIn(prefix, md.lower(), f"Internal prefix '{prefix}' found in markdown")


# ---------------------------------------------------------------------------
# Action normalization
# ---------------------------------------------------------------------------


class TestActionNormalization(unittest.TestCase):

    def test_ci_prefix_collapses_to_canonical(self):
        """'ci: required status checks...' normalizes to 'CI checks must pass'."""
        self.assertEqual(normalize_action("ci: required status checks must pass before merge"),
                         "CI checks must pass")

    def test_ci_checks_wording_collapses_to_canonical(self):
        """'CI checks must pass before merge' normalizes to 'CI checks must pass'."""
        self.assertEqual(normalize_action("CI checks must pass before merge"), "CI checks must pass")

    def test_ci_items_deduplicate_with_standard_action(self):
        """ci:-prefixed item and STANDARD_ACTIONS CI item collapse to one entry."""
        risk = _make_risk(req_val=["ci: required status checks must pass before merge"])
        actions = build_required_actions(risk, _make_rr())
        ci_count = sum(1 for a in actions if "ci checks must pass" in a.lower())
        self.assertEqual(ci_count, 1, f"Expected 1 CI action, got {ci_count}: {actions}")

    def test_meta_action_pr_risk_status_dropped(self):
        """'PR Risk: WARN — see pr_risk.md...' must not appear in required actions."""
        rr = _make_rr(recommended=["PR Risk: WARN — see pr_risk.md for required actions"])
        actions = build_required_actions(_make_risk(), rr)
        self.assertFalse(any("pr_risk.md" in a.lower() for a in actions))
        self.assertFalse(any(a.lower().startswith("pr risk") for a in actions))

    def test_meta_action_review_warnings_dropped(self):
        """'Review warnings before deploy' is a non-actionable meta item and must be excluded."""
        rr = _make_rr(recommended=["Review warnings before deploy"])
        actions = build_required_actions(_make_risk(), rr)
        self.assertFalse(any("review warnings before deploy" in a.lower() for a in actions))

    def test_config_workflow_action_becomes_concrete(self):
        """'config: workflow...' → 'Validate workflow/config changes against required checks'."""
        self.assertEqual(
            normalize_action("config: workflow / deploy / go.mod changes validated against required checks"),
            "Validate workflow/config changes against required checks",
        )

    def test_config_prefix_not_in_output(self):
        """'config:' prefix is never surfaced in the actions list."""
        risk = _make_risk(req_val=["config: workflow / deploy / go.mod changes validated against required checks"])
        actions = build_required_actions(risk, _make_rr())
        self.assertFalse(any("config:" in a.lower() for a in actions))

    def test_risky_config_warning_becomes_concrete_action(self):
        """RR warning about config path without validation note → concrete user-facing action."""
        rr = _make_rr(warning_msgs=[
            "Risky config/workflow paths changed without validation note: "
            ".github/workflows/release-readiness.yml"
        ])
        actions = build_required_actions(_make_risk(), rr)
        self.assertIn("Add a validation note for the workflow/config change", actions)

    def test_process_prefix_stripped(self):
        """'process:' prefix is stripped; remainder is capitalized and returned."""
        result = normalize_action("process: PR description with scoped, evidence-backed review plan")
        self.assertIsNotNone(result)
        self.assertFalse(result.lower().startswith("process:"))
        self.assertTrue(result[0].isupper())

    def test_no_internal_prefixes_in_actions(self):
        """Taxonomy labels must not appear verbatim in the final actions list."""
        risk = _make_risk(req_val=[
            "ci: required status checks must pass before merge",
            "config: workflow / deploy / go.mod changes validated against required checks",
            "test: run targeted regression on hotspot area",
            "process: PR description with scoped, evidence-backed review plan",
        ])
        actions = build_required_actions(risk, _make_rr())
        combined = " ".join(actions).lower()
        for prefix in ("ci:", "config:", "test:", "process:"):
            self.assertNotIn(prefix, combined, f"Found internal prefix '{prefix}' in: {actions}")

    # --- Suggestion 1: generic prefix catch-all ---

    def test_unknown_prefix_stripped_generically(self):
        """An unknown single-word lowercase prefix like 'security:' is stripped automatically."""
        result = normalize_action("security: review auth changes before merge")
        self.assertIsNotNone(result)
        self.assertFalse(result.lower().startswith("security:"),
                         f"Prefix leaked into output: {result!r}")
        self.assertEqual(result, "Review auth changes before merge")

    def test_unknown_prefix_infra_stripped(self):
        """'infra:' prefix is stripped; remainder is capitalized."""
        result = normalize_action("infra: ensure staging environment is healthy")
        self.assertIsNotNone(result)
        self.assertEqual(result, "Ensure staging environment is healthy")

    def test_url_scheme_not_stripped(self):
        """Real URL schemes (https://) are not touched — no space after colon."""
        url_action = "See https://docs.example.com for more context"
        result = normalize_action(url_action)
        self.assertEqual(result, url_action)
        self.assertIn("https://", result)

    def test_generic_prefix_strips_any_new_go_engine_label(self):
        """Any new single-word prefix from the Go engine is stripped without code changes."""
        for prefix in ("deploy:", "perf:", "docs:", "audit:"):
            raw = f"{prefix} some action text here"
            result = normalize_action(raw)
            self.assertIsNotNone(result, f"normalize_action({raw!r}) returned None")
            self.assertNotIn(prefix, result.lower(),
                             f"Prefix '{prefix}' leaked into output: {result!r}")


# ---------------------------------------------------------------------------
# Priority elevation
# ---------------------------------------------------------------------------


class TestPriorityElevation(unittest.TestCase):

    def test_priority_action_keys_is_nonempty(self):
        """PRIORITY_ACTION_KEYS must contain at least the validation-note key."""
        self.assertIn("add a validation note for the workflow/config change", PRIORITY_ACTION_KEYS)

    def test_validation_note_elevated_above_pr_risk_validations(self):
        """Validation-note action from RR warnings is inserted before PR risk validations."""
        risk = _make_risk(req_val=["Run targeted regression on hotspot area"])
        rr = _make_rr(warning_msgs=[
            "Risky config/workflow paths changed without validation note: "
            ".github/workflows/release-readiness.yml"
        ])
        actions = build_required_actions(risk, rr)
        note_idx = next(i for i, a in enumerate(actions) if "validation note" in a.lower())
        regression_idx = next(i for i, a in enumerate(actions) if "regression" in a.lower())
        self.assertLess(note_idx, regression_idx,
                        f"Validation note (idx {note_idx}) should precede regression "
                        f"(idx {regression_idx}): {actions}")

    def test_validation_note_elevated_above_rr_recommended(self):
        """Validation-note from recommended_actions is also elevated."""
        rr = _make_rr(
            recommended=[
                "Add more smoke tests",
                "Risky config/workflow paths changed without validation note: some/path.yml",
            ]
        )
        actions = build_required_actions(_make_risk(), rr)
        note_idx = next(i for i, a in enumerate(actions) if "validation note" in a.lower())
        smoke_idx = next(i for i, a in enumerate(actions) if "smoke tests" in a.lower())
        self.assertLess(note_idx, smoke_idx,
                        f"Validation note (idx {note_idx}) should precede smoke-test "
                        f"(idx {smoke_idx}): {actions}")

    def test_priority_item_appears_exactly_once(self):
        """A priority item present in multiple sources is not duplicated."""
        msg = "Risky config/workflow paths changed without validation note: x.yml"
        rr = _make_rr(
            warning_msgs=[msg],
            recommended=[msg],
        )
        actions = build_required_actions(_make_risk(), rr)
        count = sum(1 for a in actions if "validation note" in a.lower())
        self.assertEqual(count, 1, f"Expected 1, got {count}: {actions}")

    def test_priority_items_come_after_standard_actions(self):
        """Standard actions always precede any elevated priority item."""
        rr = _make_rr(warning_msgs=[
            "Risky config/workflow paths changed without validation note: x.yml"
        ])
        actions = build_required_actions(_make_risk(), rr)
        sa_max_idx = max(actions.index(sa) for sa in STANDARD_ACTIONS if sa in actions)
        note_idx = next(i for i, a in enumerate(actions) if "validation note" in a.lower())
        self.assertLess(sa_max_idx, note_idx,
                        f"Standard actions (max idx {sa_max_idx}) must precede "
                        f"validation note (idx {note_idx}): {actions}")


# ---------------------------------------------------------------------------
# Confidence helpers
# ---------------------------------------------------------------------------


class TestDeriveRRConfidence(unittest.TestCase):

    def test_high_score_no_issues_returns_near_score(self):
        rr = _make_rr("PASS", score=95.0, warnings=0, blockers=0)
        self.assertEqual(derive_rr_confidence(rr), 95)

    def test_score_capped_at_95(self):
        """Perfect 100 score yields confidence 95, not 100."""
        rr = _make_rr("PASS", score=100.0, warnings=0, blockers=0)
        self.assertEqual(derive_rr_confidence(rr), 95)

    def test_blockers_cap_at_50(self):
        rr = _make_rr("BLOCK", score=90.0, warnings=0, blockers=2)
        self.assertLessEqual(derive_rr_confidence(rr), 50)

    def test_moderate_score_no_blockers(self):
        rr = _make_rr("WARN", score=75.0, warnings=2, blockers=0)
        self.assertEqual(derive_rr_confidence(rr), 75)

    def test_low_score_blockers(self):
        rr = _make_rr("BLOCK", score=20.0, warnings=3, blockers=1)
        # min(95, 20) = 20, then capped at 50 → 20
        self.assertEqual(derive_rr_confidence(rr), 20)

    def test_zero_score(self):
        rr = _make_rr("BLOCK", score=0.0, warnings=0, blockers=5)
        self.assertEqual(derive_rr_confidence(rr), 0)

    def test_returns_int(self):
        rr = _make_rr("PASS", score=87.3, warnings=0, blockers=0)
        result = derive_rr_confidence(rr)
        self.assertIsInstance(result, int)


class TestClassifyGateConfidence(unittest.TestCase):

    def test_block_always_low(self):
        """BLOCK gate always returns 'low' regardless of signal confidence."""
        self.assertEqual(classify_gate_confidence(90, 95, "BLOCK"), "low")
        self.assertEqual(classify_gate_confidence(None, 95, "BLOCK"), "low")
        self.assertEqual(classify_gate_confidence(10, 10, "BLOCK"), "low")

    def test_pass_both_high_returns_high(self):
        # combined = (85 + 95) // 2 = 90 → high
        self.assertEqual(classify_gate_confidence(85, 95, "PASS"), "high")

    def test_pass_moderate_combined_returns_moderate(self):
        # combined = (60 + 75) // 2 = 67 → moderate
        self.assertEqual(classify_gate_confidence(60, 75, "PASS"), "moderate")

    def test_pass_low_combined_returns_low(self):
        # combined = (40 + 50) // 2 = 45 → low
        self.assertEqual(classify_gate_confidence(40, 50, "PASS"), "low")

    def test_warn_moderate_confidence(self):
        # combined = (60 + 90) // 2 = 75 → moderate
        self.assertEqual(classify_gate_confidence(60, 90, "WARN"), "moderate")

    def test_none_risk_confidence_treated_as_50(self):
        # combined = (50 + 90) // 2 = 70 → moderate
        self.assertEqual(classify_gate_confidence(None, 90, "PASS"), "moderate")

    def test_boundary_80_is_high(self):
        # combined = (80 + 80) // 2 = 80 → high (boundary inclusive)
        self.assertEqual(classify_gate_confidence(80, 80, "PASS"), "high")

    def test_boundary_79_is_moderate(self):
        # combined = (79 + 79) // 2 = 79 → moderate
        self.assertEqual(classify_gate_confidence(79, 79, "PASS"), "moderate")

    def test_boundary_60_is_moderate(self):
        # combined = (60 + 60) // 2 = 60 → moderate (boundary inclusive)
        self.assertEqual(classify_gate_confidence(60, 60, "PASS"), "moderate")

    def test_boundary_59_is_low(self):
        # combined = (59 + 59) // 2 = 59 → low
        self.assertEqual(classify_gate_confidence(59, 59, "PASS"), "low")

    def test_returns_valid_label(self):
        for status in ("PASS", "WARN", "BLOCK"):
            result = classify_gate_confidence(70, 80, status)
            self.assertIn(result, ("high", "moderate", "low"))


# ---------------------------------------------------------------------------
# Gate JSON shape
# ---------------------------------------------------------------------------


class TestGateJson(unittest.TestCase):

    def test_schema_fields_present(self):
        j = build_gate_json(_make_risk("PASS", score=5.0), _make_rr("PASS", score=100.0), "PASS", STANDARD_ACTIONS)
        self.assertEqual(j["version"], "v1")
        for key in ("pr_risk", "release_readiness", "final_gate", "required_actions", "report_enriched"):
            self.assertIn(key, j)
        # Confidence fields must be present in release_readiness and final_gate.
        self.assertIn("confidence", j["release_readiness"])
        self.assertIn("confidence", j["final_gate"])

    def test_rr_confidence_present_as_int(self):
        j = build_gate_json(_make_risk(), _make_rr("PASS", score=90.0), "PASS", [])
        self.assertIsInstance(j["release_readiness"]["confidence"], int)

    def test_gate_confidence_present_as_string(self):
        j = build_gate_json(_make_risk(), _make_rr("PASS", score=100.0), "PASS", [])
        self.assertIn(j["final_gate"]["confidence"], ("high", "moderate", "low"))

    def test_pr_risk_confidence_absent_when_none(self):
        """When risk.confidence is None, the key must not appear in pr_risk section."""
        j = build_gate_json(_make_risk(confidence=None), _make_rr(), "PASS", [])
        self.assertNotIn("confidence", j["pr_risk"])

    def test_pr_risk_confidence_present_when_set(self):
        j = build_gate_json(_make_risk(confidence=70), _make_rr(), "PASS", [])
        self.assertEqual(j["pr_risk"]["confidence"], 70)

    def test_block_gate_confidence_is_low(self):
        j = build_gate_json(_make_risk("BLOCK", confidence=80), _make_rr("PASS"), "BLOCK", [])
        self.assertEqual(j["final_gate"]["confidence"], "low")

    def test_report_enriched_false_by_default(self):
        """_make_rr() produces report_enriched=False; gate JSON reflects this."""
        j = build_gate_json(_make_risk(), _make_rr(), "PASS", STANDARD_ACTIONS)
        self.assertFalse(j["report_enriched"])

    def test_report_enriched_true_when_set(self):
        rr = _make_rr()
        rr.report_enriched = True
        j = build_gate_json(_make_risk(), rr, "PASS", STANDARD_ACTIONS)
        self.assertTrue(j["report_enriched"])

    def test_final_gate_status_matches(self):
        j = build_gate_json(_make_risk("WARN"), _make_rr("PASS"), "WARN", STANDARD_ACTIONS)
        self.assertEqual(j["final_gate"]["status"], "WARN")

    def test_scores_rounded_to_one_decimal(self):
        j = build_gate_json(_make_risk(score=5.123), _make_rr(score=100.0), "PASS", [])
        self.assertEqual(j["pr_risk"]["score"], 5.1)

    def test_top_risk_factors_included(self):
        risk = _make_risk("WARN", factors=["Large diff", "Auth area changed"])
        j = build_gate_json(risk, _make_rr("PASS"), "WARN", [])
        self.assertEqual(j["pr_risk"]["top_risk_factors"], ["Large diff", "Auth area changed"])

    def test_required_actions_deduplicated_in_json(self):
        risk = _make_risk(req_val=["CI checks must pass before merge"])
        j = build_gate_json(risk, _make_rr(), "PASS", build_required_actions(risk, _make_rr()))
        ci_count = sum(1 for a in j["required_actions"] if "ci checks must pass" in a.lower())
        self.assertEqual(ci_count, 1)


# ---------------------------------------------------------------------------
# Loaders
# ---------------------------------------------------------------------------


class TestLoaders(unittest.TestCase):

    def _tmp_json(self, data: dict) -> Path:
        f = tempfile.NamedTemporaryFile("w", suffix=".json", delete=False)
        json.dump(data, f)
        f.close()
        return Path(f.name)

    def tearDown(self):
        # Temp files are cleaned up individually in each test via os.unlink.
        pass

    def test_load_pr_risk_valid(self):
        p = self._tmp_json({
            "merge_recommendation": "WARN",
            "score": 42.5,
            "band": "medium",
            "required_validations": ["run e2e tests"],
            "top_risk_factors": ["Large diff"],
        })
        try:
            risk = load_pr_risk(p)
            self.assertEqual(risk.status, "WARN")
            self.assertAlmostEqual(risk.score, 42.5)
            self.assertEqual(risk.band, "medium")
            self.assertEqual(risk.label, "WARN")
            self.assertEqual(risk.required_validations, ["run e2e tests"])
            self.assertEqual(risk.top_risk_factors, ["Large diff"])
            # No test_confidence in input → confidence is None.
            self.assertIsNone(risk.confidence)
        finally:
            os.unlink(p)

    def test_load_pr_risk_with_test_confidence(self):
        """test_confidence field is loaded as an int when present."""
        p = self._tmp_json({
            "merge_recommendation": "PASS", "score": 5.0, "band": "low",
            "test_confidence": 72,
        })
        try:
            risk = load_pr_risk(p)
            self.assertEqual(risk.confidence, 72)
        finally:
            os.unlink(p)

    def test_load_pr_risk_without_test_confidence(self):
        """Missing test_confidence field yields confidence=None (legacy inputs)."""
        p = self._tmp_json({
            "merge_recommendation": "PASS", "score": 5.0, "band": "low",
        })
        try:
            risk = load_pr_risk(p)
            self.assertIsNone(risk.confidence)
        finally:
            os.unlink(p)

    def test_load_pr_risk_pass_label(self):
        p = self._tmp_json({"merge_recommendation": "PASS", "score": 5.0, "band": "low"})
        try:
            risk = load_pr_risk(p)
            self.assertEqual(risk.label, "PASS (low risk)")
        finally:
            os.unlink(p)

    def test_load_pr_risk_missing_file_raises(self):
        with self.assertRaises(FileNotFoundError):
            load_pr_risk(Path("/nonexistent/pr-risk.json"))

    def test_load_pr_risk_invalid_status_raises(self):
        p = self._tmp_json({"merge_recommendation": "MAYBE", "score": 10})
        try:
            with self.assertRaises(ValueError):
                load_pr_risk(p)
        finally:
            os.unlink(p)

    def test_load_pr_risk_malformed_json_raises(self):
        p = Path(tempfile.mktemp(suffix=".json"))
        p.write_text("{not valid json}", encoding="utf-8")
        try:
            with self.assertRaises(Exception):
                load_pr_risk(p)
        finally:
            p.unlink()

    def test_load_readiness_valid(self):
        p = self._tmp_json({"outcome": "PASS", "score": 100.0, "warnings": 0, "blockers": 0})
        try:
            rr = load_release_readiness(p)
            self.assertEqual(rr.status, "PASS")
            self.assertAlmostEqual(rr.score, 100.0)
            self.assertEqual(rr.warnings_count, 0)
            self.assertEqual(rr.blockers_count, 0)
        finally:
            os.unlink(p)

    def test_load_readiness_missing_file_raises(self):
        with self.assertRaises(FileNotFoundError):
            load_release_readiness(Path("/nonexistent/readiness.json"))

    def test_load_readiness_report_enriched_false_when_no_report_path(self):
        p = self._tmp_json({"outcome": "PASS", "score": 100.0, "warnings": 0, "blockers": 0})
        try:
            rr = load_release_readiness(p)  # no report_path
            self.assertFalse(rr.report_enriched)
        finally:
            os.unlink(p)

    def test_load_readiness_report_enriched_true_when_report_present(self):
        sp = self._tmp_json({"outcome": "PASS", "score": 100.0, "warnings": 0, "blockers": 0})
        rp = self._tmp_json({"blockers": [], "warnings": [], "recommended_actions": []})
        try:
            rr = load_release_readiness(sp, rp)
            self.assertTrue(rr.report_enriched)
        finally:
            os.unlink(sp)
            os.unlink(rp)

    def test_load_readiness_report_enriched_false_when_corrupt(self):
        sp = self._tmp_json({"outcome": "PASS", "score": 100.0, "warnings": 0, "blockers": 0})
        rp = Path(tempfile.mktemp(suffix=".json"))
        rp.write_text("not json", encoding="utf-8")
        try:
            rr = load_release_readiness(sp, rp)
            self.assertFalse(rr.report_enriched)
        finally:
            os.unlink(sp)
            rp.unlink()

    def test_load_readiness_enriched_by_report(self):
        sp = self._tmp_json({"outcome": "WARN", "score": 72.0, "warnings": 1, "blockers": 0})
        rp = self._tmp_json({
            "warnings": ["No smoke tests present"],
            "blockers": [],
            "recommended_actions": ["Add smoke test coverage"],
        })
        try:
            rr = load_release_readiness(sp, rp)
            self.assertEqual(rr.warning_messages, ["No smoke tests present"])
            self.assertEqual(rr.recommended_actions, ["Add smoke test coverage"])
            self.assertEqual(rr.blocker_messages, [])
        finally:
            os.unlink(sp)
            os.unlink(rp)

    def test_load_readiness_enrichment_gracefully_skipped_on_corrupt_report(self):
        """Corrupt report.json should degrade gracefully, not raise."""
        sp = self._tmp_json({"outcome": "PASS", "score": 100.0, "warnings": 0, "blockers": 0})
        rp = Path(tempfile.mktemp(suffix=".json"))
        rp.write_text("this is not json", encoding="utf-8")
        try:
            rr = load_release_readiness(sp, rp)  # must not raise
            self.assertEqual(rr.status, "PASS")
            self.assertEqual(rr.blocker_messages, [])
        finally:
            os.unlink(sp)
            rp.unlink()


# ---------------------------------------------------------------------------
# run() integration tests
# ---------------------------------------------------------------------------


class TestRun(unittest.TestCase):

    def _write(self, dir_: Path, name: str, data: dict) -> Path:
        p = dir_ / name
        p.write_text(json.dumps(data), encoding="utf-8")
        return p

    def test_pass_pass_produces_pass_exit0(self):
        with tempfile.TemporaryDirectory() as td:
            tdp = Path(td)
            rp = self._write(tdp, "pr-risk.json", {
                "merge_recommendation": "PASS", "score": 5.0, "band": "low",
                "required_validations": [], "top_risk_factors": [],
            })
            rrp = self._write(tdp, "readiness.json", {
                "outcome": "PASS", "score": 100.0, "warnings": 0, "blockers": 0,
            })
            result, code = run(rp, rrp, None, tdp / "out")
            self.assertEqual(code, 0)
            self.assertEqual(result["final_gate"]["status"], "PASS")
            self.assertTrue((tdp / "out" / "pr-gate-summary.json").exists())
            self.assertTrue((tdp / "out" / "pr-gate-summary.md").exists())

    def test_warn_pass_produces_warn(self):
        with tempfile.TemporaryDirectory() as td:
            tdp = Path(td)
            rp = self._write(tdp, "pr-risk.json", {
                "merge_recommendation": "WARN", "score": 30.0, "band": "medium",
                "required_validations": [], "top_risk_factors": [],
            })
            rrp = self._write(tdp, "readiness.json", {
                "outcome": "PASS", "score": 100.0, "warnings": 0, "blockers": 0,
            })
            result, code = run(rp, rrp, None, tdp / "out")
            self.assertEqual(code, 0)
            self.assertEqual(result["final_gate"]["status"], "WARN")

    def test_pass_warn_produces_warn(self):
        with tempfile.TemporaryDirectory() as td:
            tdp = Path(td)
            rp = self._write(tdp, "pr-risk.json", {
                "merge_recommendation": "PASS", "score": 10.0, "band": "low",
                "required_validations": [], "top_risk_factors": [],
            })
            rrp = self._write(tdp, "readiness.json", {
                "outcome": "WARN", "score": 75.0, "warnings": 2, "blockers": 0,
            })
            result, code = run(rp, rrp, None, tdp / "out")
            self.assertEqual(code, 0)
            self.assertEqual(result["final_gate"]["status"], "WARN")

    def test_warn_warn_produces_warn(self):
        with tempfile.TemporaryDirectory() as td:
            tdp = Path(td)
            rp = self._write(tdp, "pr-risk.json", {
                "merge_recommendation": "WARN", "score": 30.0, "band": "medium",
                "required_validations": [], "top_risk_factors": [],
            })
            rrp = self._write(tdp, "readiness.json", {
                "outcome": "WARN", "score": 75.0, "warnings": 1, "blockers": 0,
            })
            result, code = run(rp, rrp, None, tdp / "out")
            self.assertEqual(code, 0)
            self.assertEqual(result["final_gate"]["status"], "WARN")

    def test_block_pass_produces_block(self):
        with tempfile.TemporaryDirectory() as td:
            tdp = Path(td)
            rp = self._write(tdp, "pr-risk.json", {
                "merge_recommendation": "BLOCK", "score": 80.0, "band": "critical",
                "required_validations": [], "top_risk_factors": [],
            })
            rrp = self._write(tdp, "readiness.json", {
                "outcome": "PASS", "score": 100.0, "warnings": 0, "blockers": 0,
            })
            result, code = run(rp, rrp, None, tdp / "out")
            self.assertEqual(code, 0)
            self.assertEqual(result["final_gate"]["status"], "BLOCK")

    def test_pass_block_produces_block(self):
        with tempfile.TemporaryDirectory() as td:
            tdp = Path(td)
            rp = self._write(tdp, "pr-risk.json", {
                "merge_recommendation": "PASS", "score": 5.0, "band": "low",
                "required_validations": [], "top_risk_factors": [],
            })
            rrp = self._write(tdp, "readiness.json", {
                "outcome": "BLOCK", "score": 20.0, "warnings": 3, "blockers": 1,
            })
            result, code = run(rp, rrp, None, tdp / "out")
            self.assertEqual(code, 0)
            self.assertEqual(result["final_gate"]["status"], "BLOCK")

    def test_block_block_produces_block(self):
        with tempfile.TemporaryDirectory() as td:
            tdp = Path(td)
            rp = self._write(tdp, "pr-risk.json", {
                "merge_recommendation": "BLOCK", "score": 80.0, "band": "critical",
                "required_validations": [], "top_risk_factors": [],
            })
            rrp = self._write(tdp, "readiness.json", {
                "outcome": "BLOCK", "score": 20.0, "warnings": 2, "blockers": 2,
            })
            result, code = run(rp, rrp, None, tdp / "out")
            self.assertEqual(code, 0)
            self.assertEqual(result["final_gate"]["status"], "BLOCK")

    def test_missing_pr_risk_produces_block_exit1(self):
        with tempfile.TemporaryDirectory() as td:
            tdp = Path(td)
            rrp = self._write(tdp, "readiness.json", {
                "outcome": "PASS", "score": 100.0, "warnings": 0, "blockers": 0,
            })
            result, code = run(tdp / "nonexistent.json", rrp, None, tdp / "out")
            self.assertEqual(code, 1)
            self.assertEqual(result["final_gate"]["status"], "BLOCK")
            # Partial outputs still written.
            self.assertTrue((tdp / "out" / "pr-gate-summary.json").exists())
            self.assertTrue((tdp / "out" / "pr-gate-summary.md").exists())

    def test_missing_readiness_produces_block_exit1(self):
        with tempfile.TemporaryDirectory() as td:
            tdp = Path(td)
            rp = self._write(tdp, "pr-risk.json", {
                "merge_recommendation": "PASS", "score": 5.0, "band": "low",
                "required_validations": [], "top_risk_factors": [],
            })
            result, code = run(rp, tdp / "nonexistent.json", None, tdp / "out")
            self.assertEqual(code, 1)
            self.assertEqual(result["final_gate"]["status"], "BLOCK")

    def test_malformed_pr_risk_produces_block_exit1(self):
        with tempfile.TemporaryDirectory() as td:
            tdp = Path(td)
            rp = tdp / "pr-risk.json"
            rp.write_text("{not valid json}", encoding="utf-8")
            rrp = self._write(tdp, "readiness.json", {
                "outcome": "PASS", "score": 100.0, "warnings": 0, "blockers": 0,
            })
            result, code = run(rp, rrp, None, tdp / "out")
            self.assertEqual(code, 1)
            self.assertEqual(result["final_gate"]["status"], "BLOCK")

    def test_both_missing_produces_block_exit1(self):
        with tempfile.TemporaryDirectory() as td:
            tdp = Path(td)
            result, code = run(
                tdp / "a.json", tdp / "b.json", None, tdp / "out"
            )
            self.assertEqual(code, 1)
            self.assertEqual(result["final_gate"]["status"], "BLOCK")
            # Both partial fields should show error state.
            self.assertIn("UNKNOWN", str(result["pr_risk"]["status"]))
            self.assertIn("UNKNOWN", str(result["release_readiness"]["status"]))

    def test_required_actions_merged_and_deduplicated(self):
        with tempfile.TemporaryDirectory() as td:
            tdp = Path(td)
            rp = self._write(tdp, "pr-risk.json", {
                "merge_recommendation": "WARN",
                "score": 30.0,
                "band": "medium",
                "required_validations": [
                    "run targeted regression",
                    "CI checks must pass before merge",  # duplicate of STANDARD_ACTIONS
                ],
                "top_risk_factors": [],
            })
            rrp = self._write(tdp, "readiness.json", {
                "outcome": "PASS", "score": 100.0, "warnings": 0, "blockers": 0,
            })
            result, _ = run(rp, rrp, None, tdp / "out")
            actions = result["required_actions"]
            ci_count = sum(1 for a in actions if "ci checks must pass" in a.lower())
            self.assertEqual(ci_count, 1)
            self.assertIn("run targeted regression", actions)
            for sa in STANDARD_ACTIONS:
                self.assertIn(sa, actions)

    def test_output_json_is_valid_and_readable(self):
        """Output JSON must round-trip through json.loads."""
        with tempfile.TemporaryDirectory() as td:
            tdp = Path(td)
            rp = self._write(tdp, "pr-risk.json", {
                "merge_recommendation": "PASS", "score": 0.0, "band": "low",
                "required_validations": [], "top_risk_factors": [],
            })
            rrp = self._write(tdp, "readiness.json", {
                "outcome": "PASS", "score": 100.0, "warnings": 0, "blockers": 0,
            })
            run(rp, rrp, None, tdp / "out")
            written = json.loads((tdp / "out" / "pr-gate-summary.json").read_text())
            self.assertEqual(written["version"], "v1")
            self.assertEqual(written["final_gate"]["status"], "PASS")

    def test_report_enriched_false_when_no_report_path(self):
        """report_enriched=False when report.json is not provided to run()."""
        with tempfile.TemporaryDirectory() as td:
            tdp = Path(td)
            rp = self._write(tdp, "pr-risk.json", {
                "merge_recommendation": "PASS", "score": 0.0, "band": "low",
                "required_validations": [], "top_risk_factors": [],
            })
            rrp = self._write(tdp, "readiness.json", {
                "outcome": "PASS", "score": 100.0, "warnings": 0, "blockers": 0,
            })
            result, _ = run(rp, rrp, None, tdp / "out")
            self.assertFalse(result["report_enriched"])

    def test_report_enriched_true_when_report_json_present(self):
        """report_enriched=True when report.json is provided and readable."""
        with tempfile.TemporaryDirectory() as td:
            tdp = Path(td)
            rp = self._write(tdp, "pr-risk.json", {
                "merge_recommendation": "PASS", "score": 0.0, "band": "low",
                "required_validations": [], "top_risk_factors": [],
            })
            rrp = self._write(tdp, "readiness.json", {
                "outcome": "PASS", "score": 100.0, "warnings": 0, "blockers": 0,
            })
            rep = self._write(tdp, "report.json", {
                "blockers": [], "warnings": [], "recommended_actions": [],
            })
            result, _ = run(rp, rrp, rep, tdp / "out")
            self.assertTrue(result["report_enriched"])


if __name__ == "__main__":
    unittest.main()
