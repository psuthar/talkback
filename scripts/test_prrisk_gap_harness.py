#!/usr/bin/env python3
"""Unit tests for prrisk_gap_harness.py — pure-function coverage only.

The end-to-end git-worktree + subprocess path is exercised by running
the harness against ``ops/release-readiness/gap-test-prs.txt`` (see
SCRUM-263 acceptance run); these tests cover JSON parsing, diff logic,
band/score thresholds, file parsing, and Markdown rendering using
fixtures only — no git, no subprocess, no network.
"""
import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent))

from prrisk_gap_harness import (
    PRRiskOutput,
    PerPRDelta,
    is_significant_gap,
    load_shas,
    parse_pr_risk_json,
    render_markdown,
)


def _make_payload(
    *,
    score=10,
    band="medium",
    rec="warn",
    factor_ids=("intent_mismatch",),
    action_ids=("act_one",),
    validations=("ci: required status checks must pass before merge",),
):
    return {
        "risk_score": score,
        "risk_band": band,
        "factors": [{"id": fid, "label": fid, "points": 1} for fid in factor_ids],
        "required_actions": [{"id": aid, "title": aid} for aid in action_ids],
        "enforcement": {
            "merge_recommendation": rec,
            "required_validations": list(validations),
        },
    }


def _make_output(**overrides) -> PRRiskOutput:
    payload = _make_payload(**overrides)
    return parse_pr_risk_json(payload)


class ParsePrRiskJsonTests(unittest.TestCase):
    def test_extracts_score_band_and_recommendation(self):
        out = parse_pr_risk_json(_make_payload(score=42, band="high", rec="block"))
        self.assertEqual(out.score, 42.0)
        self.assertEqual(out.band, "high")
        self.assertEqual(out.merge_recommendation, "block")

    def test_coerces_integer_score_to_float(self):
        out = parse_pr_risk_json(_make_payload(score=6))
        self.assertIsInstance(out.score, float)
        self.assertEqual(out.score, 6.0)

    def test_factor_action_validation_ids_are_frozensets(self):
        out = parse_pr_risk_json(
            _make_payload(
                factor_ids=("a", "b"),
                action_ids=("x",),
                validations=("v1", "v2"),
            )
        )
        self.assertIsInstance(out.factor_ids, frozenset)
        self.assertEqual(out.factor_ids, frozenset({"a", "b"}))
        self.assertEqual(out.action_ids, frozenset({"x"}))
        self.assertEqual(out.validation_ids, frozenset({"v1", "v2"}))

    def test_missing_risk_score_raises(self):
        payload = _make_payload()
        del payload["risk_score"]
        with self.assertRaises(ValueError):
            parse_pr_risk_json(payload)

    def test_handles_missing_enforcement_block(self):
        payload = _make_payload()
        del payload["enforcement"]
        out = parse_pr_risk_json(payload)
        self.assertEqual(out.merge_recommendation, "")
        self.assertEqual(out.validation_ids, frozenset())

    def test_skips_factors_and_actions_without_id(self):
        payload = _make_payload(factor_ids=("good",), action_ids=("ok",))
        payload["factors"].append({"label": "no id"})
        payload["required_actions"].append({"title": "no id"})
        out = parse_pr_risk_json(payload)
        self.assertEqual(out.factor_ids, frozenset({"good"}))
        self.assertEqual(out.action_ids, frozenset({"ok"}))


class PerPRDeltaTests(unittest.TestCase):
    def _delta(self, **rrc_overrides):
        go = _make_output()
        rrc = _make_output(**rrc_overrides)
        return PerPRDelta(sha="abc1234567", summary="auth scope", go=go, rrc=rrc)

    def test_score_delta_is_rrc_minus_go(self):
        delta = self._delta(score=15)
        self.assertEqual(delta.score_delta, 5.0)

    def test_band_match_when_bands_equal(self):
        self.assertTrue(self._delta().band_match)
        self.assertFalse(self._delta(band="high").band_match)

    def test_rec_match_when_recommendations_equal(self):
        self.assertTrue(self._delta().rec_match)
        self.assertFalse(self._delta(rec="block").rec_match)

    def test_factor_only_lists_are_set_diffs_sorted(self):
        delta = self._delta(factor_ids=("intent_mismatch", "extra_rrc"))
        self.assertEqual(delta.factor_only_go, [])
        self.assertEqual(delta.factor_only_rrc, ["extra_rrc"])

    def test_action_and_validation_diffs_round_trip(self):
        delta = self._delta(
            action_ids=("brand_new",),
            validations=("v_only_rrc",),
        )
        self.assertEqual(delta.action_only_go, ["act_one"])
        self.assertEqual(delta.action_only_rrc, ["brand_new"])
        self.assertEqual(
            delta.validation_only_go,
            ["ci: required status checks must pass before merge"],
        )
        self.assertEqual(delta.validation_only_rrc, ["v_only_rrc"])


class SignificantGapTests(unittest.TestCase):
    def _delta(self, *, go_score, rrc_score, go_band="low", rrc_band="low"):
        go = _make_output(score=go_score, band=go_band)
        rrc = _make_output(score=rrc_score, band=rrc_band)
        return PerPRDelta(sha="ab", summary="", go=go, rrc=rrc)

    def test_band_mismatch_is_always_significant(self):
        d = self._delta(go_score=5, rrc_score=5, go_band="medium", rrc_band="low")
        self.assertTrue(is_significant_gap(d, score_tolerance=100))

    def test_score_delta_within_tolerance_is_not_significant(self):
        d = self._delta(go_score=10, rrc_score=14)
        self.assertFalse(is_significant_gap(d, score_tolerance=5.0))

    def test_score_delta_at_tolerance_is_not_significant(self):
        d = self._delta(go_score=10, rrc_score=15)
        self.assertFalse(is_significant_gap(d, score_tolerance=5.0))

    def test_score_delta_above_tolerance_is_significant(self):
        d = self._delta(go_score=10, rrc_score=16)
        self.assertTrue(is_significant_gap(d, score_tolerance=5.0))

    def test_negative_score_delta_above_tolerance_is_significant(self):
        d = self._delta(go_score=20, rrc_score=10)
        self.assertTrue(is_significant_gap(d, score_tolerance=5.0))


class LoadShasTests(unittest.TestCase):
    def test_strips_blank_lines_and_full_comments(self):
        text = "\n# comment\n\nabc1234567\n  \n# another\n"
        self.assertEqual(load_shas(text), [("abc1234567", "")])

    def test_inline_summary_after_hash_is_captured(self):
        text = "abc1234567  # area=auth ticket=SCRUM-227\n"
        self.assertEqual(
            load_shas(text), [("abc1234567", "area=auth ticket=SCRUM-227")]
        )

    def test_multiple_lines_preserve_order(self):
        text = "aaa  # one\nbbb  # two\nccc\n"
        self.assertEqual(
            load_shas(text),
            [("aaa", "one"), ("bbb", "two"), ("ccc", "")],
        )

    def test_empty_text_returns_empty_list(self):
        self.assertEqual(load_shas(""), [])
        self.assertEqual(load_shas("   \n# only comments\n"), [])


class RenderMarkdownTests(unittest.TestCase):
    def test_includes_header_summary_count_and_tolerance_footer(self):
        d = PerPRDelta(
            sha="abcdef1234567890",
            summary="auth flag flip",
            go=_make_output(score=12, band="medium"),
            rrc=_make_output(score=14, band="medium"),
        )
        md = render_markdown([d], score_tolerance=5.0)
        self.assertIn("# PR-risk gap test", md)
        self.assertIn("PRs scanned: **1**", md)
        self.assertIn("Significant gaps", md)
        self.assertIn("Score tolerance: ±5", md)
        self.assertIn("`abcdef1234`", md)
        self.assertIn("auth flag flip", md)

    def test_marks_significant_gaps_visually(self):
        d_ok = PerPRDelta(
            sha="0000000001",
            summary="ok",
            go=_make_output(score=10),
            rrc=_make_output(score=12),
        )
        d_bad = PerPRDelta(
            sha="0000000002",
            summary="bad",
            go=_make_output(score=10, band="medium"),
            rrc=_make_output(score=10, band="low"),
        )
        md = render_markdown([d_ok, d_bad], score_tolerance=5.0)
        self.assertIn("0000000002` ⚠️", md)
        self.assertNotIn("0000000001` ⚠️", md)
        self.assertIn("Significant gaps (band mismatch or |score Δ| > 5): **1**", md)

    def test_handles_empty_delta_list(self):
        md = render_markdown([], score_tolerance=5.0)
        self.assertIn("PRs scanned: **0**", md)
        self.assertIn("Significant gaps", md)


if __name__ == "__main__":
    unittest.main()
