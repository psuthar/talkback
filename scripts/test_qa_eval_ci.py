#!/usr/bin/env python3
"""Unit tests for scripts/qa_eval_ci.py (SCRUM-562).

Threshold-logic tests exercise ``evaluate_thresholds`` directly; the
``main()`` tests use a temp baseline + thresholds + a never-existing
base-ref to drive the no-prior-baseline + error-handling paths
without standing up a git history.
"""

from __future__ import annotations

import json
import sys
import tempfile
import unittest
from pathlib import Path

_REPO_ROOT = Path(__file__).resolve().parents[1]
if str(_REPO_ROOT) not in sys.path:
    sys.path.insert(0, str(_REPO_ROOT))

from scripts.qa_eval_ci import (  # noqa: E402
    DEFAULT_BASELINE,
    DEFAULT_THRESHOLDS,
    evaluate_thresholds,
    main,
)


# A representative baseline used as the "prior" in threshold tests.
_PRIOR_METRICS = {
    "correctness_percentage": 100.0,
    "hallucination_count": 0,
    "weighted_correctness": 1.0,
    "overall_pass": True,
    "p95_latency_ms": 200.0,
}

_DEFAULT_THRESHOLDS_DICT = {
    "correctness_percentage_min_delta_pp": -2.0,
    "hallucination_count_max_delta": 0,
    "weighted_correctness_min_delta": -0.02,
    "p95_latency_ms_max_delta_pct": 25.0,
    "overall_pass_required": True,
    # SCRUM-564 (Slice 3) — input-guardrail metrics
    "refusal_when_oos_rate_min_delta": -0.05,
    "legitimate_false_positive_rate_max_delta": 0.02,
}


def _by_metric(evals: list[dict]) -> dict[str, dict]:
    return {e["metric"]: e for e in evals}


class TestEvaluateThresholdsHappyPaths(unittest.TestCase):
    def test_no_prior_skips_compare(self) -> None:
        evals = evaluate_thresholds(_PRIOR_METRICS, None, _DEFAULT_THRESHOLDS_DICT)
        self.assertEqual(len(evals), 1)
        self.assertEqual(evals[0]["metric"], "_compare")
        self.assertTrue(evals[0]["skipped"])
        self.assertTrue(evals[0]["pass"])

    def test_current_equals_prior_passes_all(self) -> None:
        evals = evaluate_thresholds(_PRIOR_METRICS, _PRIOR_METRICS, _DEFAULT_THRESHOLDS_DICT)
        for e in evals:
            self.assertTrue(e["pass"], msg=f"{e['metric']}: {e}")

    def test_small_correctness_drop_within_threshold_passes(self) -> None:
        current = {**_PRIOR_METRICS, "correctness_percentage": 99.0}
        e = _by_metric(evaluate_thresholds(current, _PRIOR_METRICS, _DEFAULT_THRESHOLDS_DICT))
        self.assertTrue(e["correctness_percentage"]["pass"])
        self.assertEqual(e["correctness_percentage"]["delta"], -1.0)


class TestEvaluateThresholdsFailures(unittest.TestCase):
    def test_correctness_drop_below_threshold_fails(self) -> None:
        current = {**_PRIOR_METRICS, "correctness_percentage": 90.0}
        e = _by_metric(evaluate_thresholds(current, _PRIOR_METRICS, _DEFAULT_THRESHOLDS_DICT))["correctness_percentage"]
        self.assertFalse(e["pass"])
        self.assertEqual(e["delta"], -10.0)
        self.assertIn("threshold floor", e["note"])

    def test_hallucination_increase_fails(self) -> None:
        current = {**_PRIOR_METRICS, "hallucination_count": 1}
        e = _by_metric(evaluate_thresholds(current, _PRIOR_METRICS, _DEFAULT_THRESHOLDS_DICT))["hallucination_count"]
        self.assertFalse(e["pass"])
        self.assertEqual(e["delta"], 1)

    def test_weighted_correctness_drop_fails(self) -> None:
        current = {**_PRIOR_METRICS, "weighted_correctness": 0.5}
        e = _by_metric(evaluate_thresholds(current, _PRIOR_METRICS, _DEFAULT_THRESHOLDS_DICT))["weighted_correctness"]
        self.assertFalse(e["pass"])

    def test_p95_latency_grows_too_much_fails(self) -> None:
        current = {**_PRIOR_METRICS, "p95_latency_ms": 400.0}  # +100% on 200 baseline
        e = _by_metric(evaluate_thresholds(current, _PRIOR_METRICS, _DEFAULT_THRESHOLDS_DICT))["p95_latency_ms"]
        self.assertFalse(e["pass"])
        self.assertIn("grew by", e["note"])

    def test_overall_pass_false_fails_when_required(self) -> None:
        current = {**_PRIOR_METRICS, "overall_pass": False}
        e = _by_metric(evaluate_thresholds(current, _PRIOR_METRICS, _DEFAULT_THRESHOLDS_DICT))["overall_pass"]
        self.assertFalse(e["pass"])


class TestSCRUM564InputGuardrailThresholds(unittest.TestCase):
    """SCRUM-564 (Slice 3): refusal_when_oos_rate +
    legitimate_false_positive_rate threshold rules."""

    _PRIOR = {**_PRIOR_METRICS, "refusal_when_oos_rate": 1.0, "legitimate_false_positive_rate": 0.0}

    def test_refusal_when_oos_rate_equal_to_baseline_passes(self) -> None:
        e = _by_metric(evaluate_thresholds(self._PRIOR, self._PRIOR, _DEFAULT_THRESHOLDS_DICT))["refusal_when_oos_rate"]
        self.assertTrue(e["pass"])
        self.assertEqual(e["delta"], 0.0)

    def test_refusal_when_oos_rate_small_drop_passes(self) -> None:
        cur = {**self._PRIOR, "refusal_when_oos_rate": 0.97}
        e = _by_metric(evaluate_thresholds(cur, self._PRIOR, _DEFAULT_THRESHOLDS_DICT))["refusal_when_oos_rate"]
        self.assertTrue(e["pass"])
        self.assertEqual(e["delta"], -0.03)

    def test_refusal_when_oos_rate_large_drop_fails(self) -> None:
        cur = {**self._PRIOR, "refusal_when_oos_rate": 0.80}
        e = _by_metric(evaluate_thresholds(cur, self._PRIOR, _DEFAULT_THRESHOLDS_DICT))["refusal_when_oos_rate"]
        self.assertFalse(e["pass"])
        self.assertIn("threshold floor", e["note"])

    def test_legitimate_false_positive_rate_equal_passes(self) -> None:
        e = _by_metric(evaluate_thresholds(self._PRIOR, self._PRIOR, _DEFAULT_THRESHOLDS_DICT))["legitimate_false_positive_rate"]
        self.assertTrue(e["pass"])

    def test_legitimate_false_positive_rate_small_rise_passes(self) -> None:
        cur = {**self._PRIOR, "legitimate_false_positive_rate": 0.01}
        e = _by_metric(evaluate_thresholds(cur, self._PRIOR, _DEFAULT_THRESHOLDS_DICT))["legitimate_false_positive_rate"]
        self.assertTrue(e["pass"])

    def test_legitimate_false_positive_rate_large_rise_fails(self) -> None:
        cur = {**self._PRIOR, "legitimate_false_positive_rate": 0.10}
        e = _by_metric(evaluate_thresholds(cur, self._PRIOR, _DEFAULT_THRESHOLDS_DICT))["legitimate_false_positive_rate"]
        self.assertFalse(e["pass"])
        self.assertIn("threshold ceiling", e["note"])

    def test_metrics_missing_in_prior_skipped(self) -> None:
        # When the base ref pre-dates SCRUM-564, the prior baseline has
        # no refusal_when_oos_rate key — the comparison must skip
        # gracefully rather than blow up the gate.
        prior = {k: v for k, v in self._PRIOR.items() if k != "refusal_when_oos_rate"}
        e = _by_metric(evaluate_thresholds(self._PRIOR, prior, _DEFAULT_THRESHOLDS_DICT))["refusal_when_oos_rate"]
        self.assertTrue(e["skipped"])
        self.assertTrue(e["pass"])


class TestEvaluateThresholdsSkipsMissingSignal(unittest.TestCase):
    def test_p95_null_in_baseline_skips(self) -> None:
        prior = {**_PRIOR_METRICS, "p95_latency_ms": None}
        current = {**_PRIOR_METRICS, "p95_latency_ms": 500.0}
        e = _by_metric(evaluate_thresholds(current, prior, _DEFAULT_THRESHOLDS_DICT))["p95_latency_ms"]
        self.assertTrue(e["skipped"])
        self.assertTrue(e["pass"])

    def test_p95_baseline_zero_skips(self) -> None:
        prior = {**_PRIOR_METRICS, "p95_latency_ms": 0}
        current = {**_PRIOR_METRICS, "p95_latency_ms": 100.0}
        e = _by_metric(evaluate_thresholds(current, prior, _DEFAULT_THRESHOLDS_DICT))["p95_latency_ms"]
        self.assertTrue(e["skipped"])

    def test_missing_threshold_skips_metric(self) -> None:
        thresholds = {k: v for k, v in _DEFAULT_THRESHOLDS_DICT.items() if k != "correctness_percentage_min_delta_pp"}
        e = _by_metric(evaluate_thresholds(_PRIOR_METRICS, _PRIOR_METRICS, thresholds))["correctness_percentage"]
        self.assertTrue(e["skipped"])


class TestMainEndToEnd(unittest.TestCase):
    def _write_baseline(self, dir_: Path, metrics: dict) -> Path:
        p = dir_ / "baseline.json"
        p.write_text(
            json.dumps(
                {
                    "schema": "qa-eval-baseline/v1",
                    "source_recorded_at": "2026-05-25T00:00:00Z",
                    "source_commit": "abc123",
                    "metrics": metrics,
                }
            ),
            encoding="utf-8",
        )
        return p

    def _write_thresholds(self, dir_: Path) -> Path:
        # Use JSON since PyYAML accepts JSON as a valid YAML subset.
        p = dir_ / "thresholds.yaml"
        p.write_text(json.dumps(_DEFAULT_THRESHOLDS_DICT), encoding="utf-8")
        return p

    def test_no_prior_baseline_passes_and_emits_summary(self) -> None:
        with tempfile.TemporaryDirectory() as td:
            tdir = Path(td)
            baseline = self._write_baseline(tdir, _PRIOR_METRICS)
            thresholds = self._write_thresholds(tdir)
            output = tdir / "qa-eval-summary.json"
            rc = main(
                [
                    "--baseline", str(baseline),
                    "--thresholds", str(thresholds),
                    "--base-ref", "00000000000000000000000000000000deadbeef",  # never exists
                    "--output", str(output),
                    "--skip-cli-sanity",
                ]
            )
            self.assertEqual(rc, 0)
            self.assertTrue(output.exists())
            summary = json.loads(output.read_text(encoding="utf-8"))
            self.assertEqual(summary["schema"], "qa-eval-summary/v1")
            self.assertFalse(summary["prior_baseline_present"])
            self.assertTrue(summary["overall_pass"])
            self.assertEqual(summary["current_metrics"]["correctness_percentage"], 100.0)
            self.assertIn("threshold_evaluations", summary)
            self.assertIn("notes", summary)

    def test_missing_baseline_file_returns_exit_2(self) -> None:
        with tempfile.TemporaryDirectory() as td:
            tdir = Path(td)
            thresholds = self._write_thresholds(tdir)
            rc = main(
                [
                    "--baseline", str(tdir / "missing.json"),
                    "--thresholds", str(thresholds),
                    "--base-ref", "00000000000000000000000000000000deadbeef",
                    "--output", str(tdir / "out.json"),
                    "--skip-cli-sanity",
                ]
            )
            self.assertEqual(rc, 2)

    def test_malformed_baseline_json_returns_exit_2(self) -> None:
        with tempfile.TemporaryDirectory() as td:
            tdir = Path(td)
            baseline = tdir / "bad.json"
            baseline.write_text("{not json", encoding="utf-8")
            thresholds = self._write_thresholds(tdir)
            rc = main(
                [
                    "--baseline", str(baseline),
                    "--thresholds", str(thresholds),
                    "--base-ref", "00000000000000000000000000000000deadbeef",
                    "--output", str(tdir / "out.json"),
                    "--skip-cli-sanity",
                ]
            )
            self.assertEqual(rc, 2)


class TestRepoDefaults(unittest.TestCase):
    """The seeded files in eval/baselines/ must self-validate.

    Self-compare (same file as both current and prior) is the cheap
    smoke test that the schema + thresholds line up.
    """

    def test_default_baseline_and_thresholds_parse(self) -> None:
        self.assertTrue(DEFAULT_BASELINE.exists(), DEFAULT_BASELINE)
        self.assertTrue(DEFAULT_THRESHOLDS.exists(), DEFAULT_THRESHOLDS)
        data = json.loads(DEFAULT_BASELINE.read_text(encoding="utf-8"))
        self.assertEqual(data.get("schema"), "qa-eval-baseline/v1")
        self.assertIn("metrics", data)


if __name__ == "__main__":
    unittest.main()
