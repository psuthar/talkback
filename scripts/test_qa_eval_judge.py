#!/usr/bin/env python3
"""Unit tests for strict JSON judge parsing/retry/error handling."""

from __future__ import annotations

import sys
import unittest
from pathlib import Path

_REPO_ROOT = Path(__file__).resolve().parents[1]
if str(_REPO_ROOT) not in sys.path:
    sys.path.insert(0, str(_REPO_ROOT))

from scripts.qa_eval_judge import (  # noqa: E402
    JudgeException,
    judge_case_with_retry,
    parse_judge_text,
    validate_judge_payload,
)


class TestValidateJudgePayload(unittest.TestCase):
    def test_valid_payload(self) -> None:
        p = {
            "is_correct": True,
            "hallucination": False,
            "score_0_to_1": 0.91,
            "reason": "Matches fixture facts.",
        }
        out = validate_judge_payload(p)
        self.assertEqual(out["score_0_to_1"], 0.91)

    def test_missing_key_raises(self) -> None:
        with self.assertRaises(JudgeException):
            validate_judge_payload({"is_correct": True})

    def test_score_out_of_range_raises(self) -> None:
        with self.assertRaises(JudgeException):
            validate_judge_payload(
                {
                    "is_correct": True,
                    "hallucination": False,
                    "score_0_to_1": 1.4,
                    "reason": "bad",
                }
            )


class TestParseJudgeText(unittest.TestCase):
    def test_invalid_json_raises(self) -> None:
        with self.assertRaises(JudgeException):
            parse_judge_text("{not json}")


class TestJudgeCaseWithRetry(unittest.TestCase):
    def _sample_inputs(self) -> tuple[dict[str, object], dict[str, object]]:
        case_contract = {
            "case_id": "FF-001",
            "expected_status": "answered",
            "expected_keywords": ["meridian", "approved"],
            "hallucination_constraints": {"answer_must_not_contain": ["rejected"]},
        }
        score_target = {
            "case_id": "FF-001",
            "correctness_min": 0.88,
            "hallucination_max": 0.12,
            "weight": 1.0,
        }
        return case_contract, score_target

    def test_not_configured(self) -> None:
        cc, st = self._sample_inputs()
        out = judge_case_with_retry(
            case_contract=cc,
            score_target=st,
            model_answer="Meridian was approved",
            response_http_status=200,
            api_key="",
        )
        self.assertFalse(out["ok"])
        self.assertEqual(out["error_code"], "judge_not_configured")

    def test_retry_then_success(self) -> None:
        cc, st = self._sample_inputs()
        calls = {"n": 0}

        def invoke(_: str) -> str:
            calls["n"] += 1
            if calls["n"] == 1:
                return "not-json"
            return (
                '{"is_correct": true, "hallucination": false, '
                '"score_0_to_1": 0.93, "reason": "good"}'
            )

        out = judge_case_with_retry(
            case_contract=cc,
            score_target=st,
            model_answer="Meridian was approved",
            response_http_status=200,
            api_key="x",
            max_attempts=2,
            invoke_fn=invoke,
        )
        self.assertTrue(out["ok"])
        self.assertEqual(out["attempts"], 2)
        self.assertEqual(out["verdict"]["is_correct"], True)

    def test_non_retryable_error_stops(self) -> None:
        cc, st = self._sample_inputs()

        def invoke(_: str) -> str:
            raise JudgeException("judge_http_error", "401", False)

        out = judge_case_with_retry(
            case_contract=cc,
            score_target=st,
            model_answer="Meridian was approved",
            response_http_status=200,
            api_key="x",
            max_attempts=3,
            invoke_fn=invoke,
        )
        self.assertFalse(out["ok"])
        self.assertEqual(out["attempts"], 1)
        self.assertEqual(out["error_code"], "judge_http_error")


if __name__ == "__main__":
    unittest.main()
