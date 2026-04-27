#!/usr/bin/env python3
"""Unit tests for Q&A eval runner orchestration and serialization."""

from __future__ import annotations

import json
import sys
import tempfile
import unittest
from pathlib import Path
from unittest.mock import MagicMock, patch

_REPO_ROOT = Path(__file__).resolve().parents[1]
if str(_REPO_ROOT) not in sys.path:
    sys.path.insert(0, str(_REPO_ROOT))

from scripts.run_qa_eval import (
    CaseResult,
    aggregate_report,
    auto_setup_sessions,
    case_to_artifact,
    render_terminal_report,
    normalize_session_ask_response,
    ordered_fixture_ids,
    parse_sessions_json,
    run_inventory_cases,
    build_judge_contracts,
    write_report_artifact,
    write_run_artifacts,
)


class TestNormalizeSessionAskResponse(unittest.TestCase):
    def test_extracts_answer_and_question(self) -> None:
        body = {
            "question": {"id": "q1", "question_text": "Hello?"},
            "answer": {
                "answer_text": "Hi",
                "citations": [{"citation_id": "c1"}],
                "llm_model": "gpt-4",
            },
        }
        n = normalize_session_ask_response(body)
        self.assertEqual(n["answer_text"], "Hi")
        self.assertEqual(n["citation_count"], 1)
        self.assertEqual(n["llm_model"], "gpt-4")
        self.assertEqual(n["question_id"], "q1")
        self.assertEqual(n["question_text"], "Hello?")

    def test_non_dict_returns_empty_shape(self) -> None:
        n = normalize_session_ask_response(None)
        self.assertIsNone(n["answer_text"])
        n2 = normalize_session_ask_response("x")
        self.assertIsNone(n2["answer_text"])


class TestParseSessionsJson(unittest.TestCase):
    def test_parses_object(self) -> None:
        m = parse_sessions_json('{"a": "uuid-1"}')
        self.assertEqual(m, {"a": "uuid-1"})

    def test_rejects_non_object(self) -> None:
        with self.assertRaises(ValueError):
            parse_sessions_json("[1]")


class TestOrderedFixtureIds(unittest.TestCase):
    def test_preserves_first_seen_order(self) -> None:
        cases = [
            {"fixture_id": "b"},
            {"fixture_id": "a"},
            {"fixture_id": "b"},
        ]
        self.assertEqual(ordered_fixture_ids(cases), ["b", "a"])


class TestRunInventoryCases(unittest.TestCase):
    def test_mock_ask_and_missing_session(self) -> None:
        def mock_ask(
            base_url: str, cookie: str, session_id: str, question_text: str
        ) -> tuple[int, str]:
            self.assertEqual(session_id, "s1")
            return (
                200,
                json.dumps(
                    {
                        "question": {"id": "q", "question_text": question_text},
                        "answer": {"answer_text": "ok", "citations": []},
                    }
                ),
            )

        cases = [
            {"case_id": "FF-001", "fixture_id": "fx", "question": "Q1"},
            {"case_id": "FF-002", "fixture_id": "missing", "question": "Q2"},
        ]
        results = run_inventory_cases(
            base_url="http://localhost:8080",
            cookie="x",
            session_for_fixture={"fx": "s1"},
            cases=cases,
            ask_fn=mock_ask,
        )
        self.assertEqual(results[0].http_status, 200)
        self.assertEqual(results[0].parsed_json["answer"]["answer_text"], "ok")
        self.assertEqual(results[1].skipped_reason, "no session mapped for fixture_id=missing")

    def test_judge_hook_attaches_result(self) -> None:
        def mock_ask(
            base_url: str, cookie: str, session_id: str, question_text: str
        ) -> tuple[int, str]:
            return (
                200,
                json.dumps(
                    {
                        "question": {"id": "q", "question_text": question_text},
                        "answer": {"answer_text": "Meridian approved", "citations": []},
                    }
                ),
            )

        def mock_judge(
            case_contract: dict, score_target: dict, answer_text: str, http_status: int | None
        ) -> dict:
            return {
                "ok": True,
                "error_code": None,
                "error_message": None,
                "attempts": 1,
                "verdict": {
                    "is_correct": True,
                    "hallucination": False,
                    "score_0_to_1": 0.9,
                    "reason": "ok",
                },
            }

        contracts = {
            "FF-001": {
                "case_contract": {"case_id": "FF-001"},
                "score_target": {"case_id": "FF-001"},
            }
        }
        cases = [{"case_id": "FF-001", "fixture_id": "fx", "question": "Q1"}]
        results = run_inventory_cases(
            base_url="http://localhost:8080",
            cookie="x",
            session_for_fixture={"fx": "s1"},
            cases=cases,
            ask_fn=mock_ask,
            judge_contract_by_case=contracts,
            judge_fn=mock_judge,
        )
        self.assertIsNotNone(results[0].judge)
        self.assertTrue(results[0].judge["ok"])


class TestWriteRunArtifacts(unittest.TestCase):
    def test_writes_manifest_and_case_files(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            run_dir = Path(tmp) / "run1"
            cases = [
                {
                    "case_id": "FF-001",
                    "fixture_id": "fx",
                    "question": "Q?",
                    "expected_status": "answered",
                }
            ]
            results = [
                CaseResult(
                    case_id="FF-001",
                    fixture_id="fx",
                    question="Q?",
                    http_status=200,
                    raw_body='{"answer":{"answer_text":"A"}}',
                    parsed_json={"answer": {"answer_text": "A"}},
                )
            ]
            write_run_artifacts(
                run_dir,
                run_id="20260101T000000Z",
                base_url="http://localhost:8080",
                inventory_path=Path("/tmp/inventory.json"),
                auto_setup=False,
                dry_run=False,
                session_map={"fx": "sid"},
                cases=cases,
                results=results,
            )
            manifest = json.loads((run_dir / "run_manifest.json").read_text())
            self.assertEqual(manifest["case_count"], 1)
            self.assertEqual(manifest["session_map"]["fx"], "sid")
            case_file = run_dir / "cases" / "FF-001.json"
            self.assertTrue(case_file.is_file())
            payload = json.loads(case_file.read_text())
            self.assertEqual(payload["normalized"]["answer_text"], "A")
            self.assertIsNone(payload["judge"])


class TestAggregateReport(unittest.TestCase):
    def test_aggregates_correctness_hallucination_and_breakdown(self) -> None:
        cases = [
            {"case_id": "FF-001", "expected_status": "answered"},
            {"case_id": "FF-002", "expected_status": "not_covered"},
            {"case_id": "FF-003", "expected_status": "answered"},
        ]
        results = [
            CaseResult(
                case_id="FF-001",
                fixture_id="f1",
                question="q1",
                judge={
                    "ok": True,
                    "verdict": {
                        "is_correct": True,
                        "hallucination": False,
                        "score_0_to_1": 0.9,
                        "reason": "ok",
                    },
                },
            ),
            CaseResult(
                case_id="FF-002",
                fixture_id="f2",
                question="q2",
                judge={
                    "ok": True,
                    "verdict": {
                        "is_correct": False,
                        "hallucination": True,
                        "score_0_to_1": 0.2,
                        "reason": "hallucinated",
                    },
                },
            ),
            CaseResult(
                case_id="FF-003",
                fixture_id="f3",
                question="q3",
                judge={"ok": False, "error_code": "judge_invalid_json", "verdict": None},
            ),
        ]
        report = aggregate_report(cases, results)
        m = report["metrics"]
        self.assertEqual(m["total_cases"], 3)
        self.assertEqual(m["judge_attempted"], 3)
        self.assertEqual(m["judge_ok"], 2)
        self.assertEqual(m["judge_error"], 1)
        self.assertEqual(m["correct_count"], 1)
        self.assertEqual(m["hallucination_count"], 1)
        self.assertEqual(m["correctness_percentage"], 50.0)
        self.assertEqual(m["status_breakdown"]["answered"], 2)
        self.assertEqual(m["status_breakdown"]["not_covered"], 1)
        self.assertEqual(report["failed_case_ids"], ["FF-003"])

    def test_write_report_and_render_terminal(self) -> None:
        report = {
            "metrics": {
                "total_cases": 1,
                "judge_attempted": 1,
                "judge_ok": 1,
                "judge_error": 0,
                "correct_count": 1,
                "hallucination_count": 0,
                "correctness_percentage": 100.0,
                "status_breakdown": {
                    "answered": 1,
                    "not_covered": 0,
                    "other_or_missing": 0,
                },
            },
            "failed_case_ids": [],
        }
        text = render_terminal_report(report)
        self.assertIn("correctness %: 100.0%", text)
        self.assertIn("hallucination count: 0", text)

        with tempfile.TemporaryDirectory() as tmp:
            run_dir = Path(tmp) / "run"
            write_report_artifact(
                run_dir,
                run_id="r1",
                inventory_path=_REPO_ROOT / "eval" / "qa" / "fixture_fact_inventory.json",
                report=report,
            )
            payload = json.loads((run_dir / "report.json").read_text())
            self.assertEqual(payload["run_id"], "r1")
            self.assertEqual(payload["metrics"]["correctness_percentage"], 100.0)
            self.assertEqual(payload["source_run_manifest"], "run_manifest.json")


class TestCaseToArtifactOrdering(unittest.TestCase):
    def test_key_order_stable(self) -> None:
        case = {"expected_status": "answered"}
        result = CaseResult(
            case_id="FF-001",
            fixture_id="f",
            question="q",
            http_status=400,
            raw_body="oops",
        )
        art = case_to_artifact(case, result)
        keys = list(art.keys())
        self.assertEqual(
            keys,
            [
                "case_id",
                "fixture_id",
                "question",
                "inventory_expected_status",
                "request",
                "response",
                "normalized",
                "judge",
            ],
        )


class TestAutoSetupSessions(unittest.TestCase):
    @patch("scripts.run_qa_eval.patch_session")
    @patch("scripts.run_qa_eval.paste_material")
    @patch("scripts.run_qa_eval.create_session")
    def test_invokes_create_paste_patch(
        self, mock_create: MagicMock, mock_paste: MagicMock, mock_patch: MagicMock
    ) -> None:
        mock_create.return_value = "session-1"
        mapping = auto_setup_sessions(
            "http://x",
            "cookie",
            ["smoke_session_update_apac_decision"],
            "runid",
        )
        self.assertEqual(mapping["smoke_session_update_apac_decision"], "session-1")
        mock_create.assert_called_once()
        mock_paste.assert_called_once()
        mock_patch.assert_called_once()


class TestJudgeContracts(unittest.TestCase):
    def test_build_contracts_contains_case(self) -> None:
        contracts = build_judge_contracts(
            _REPO_ROOT / "eval" / "qa" / "eval_cases_v1.json",
            _REPO_ROOT / "eval" / "qa" / "expected_scores_v1.json",
        )
        self.assertIn("FF-001", contracts)
        self.assertIn("case_contract", contracts["FF-001"])
        self.assertIn("score_target", contracts["FF-001"])


if __name__ == "__main__":
    unittest.main()
