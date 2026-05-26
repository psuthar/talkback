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
    detect_refusal,
    render_terminal_report,
    normalize_session_ask_response,
    ordered_fixture_ids,
    parse_sessions_json,
    run_inventory_cases,
    run_session_warmup,
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

    def test_retries_on_session_question_limit_with_new_session(self) -> None:
        call_count = {"n": 0}

        def mock_ask(
            base_url: str, cookie: str, session_id: str, question_text: str
        ) -> tuple[int, str]:
            call_count["n"] += 1
            if call_count["n"] == 1:
                return (429, json.dumps({"error": "session question limit reached"}))
            self.assertEqual(session_id, "s2")
            return (
                200,
                json.dumps(
                    {
                        "question": {"id": "q", "question_text": question_text},
                        "answer": {"answer_text": "ok", "citations": []},
                    }
                ),
            )

        def provision_session(fid: str) -> str:
            self.assertEqual(fid, "fx")
            return "s2"

        cases = [{"case_id": "FF-001", "fixture_id": "fx", "question": "Q1"}]
        results = run_inventory_cases(
            base_url="http://localhost:8080",
            cookie="x",
            session_for_fixture={"fx": "s1"},
            cases=cases,
            ask_fn=mock_ask,
            provision_session_fn=provision_session,
        )
        self.assertEqual(results[0].http_status, 200)
        self.assertEqual(call_count["n"], 2)


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
    def test_aggregates_thresholds_weighted_and_overall_pass(self) -> None:
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
        report = aggregate_report(
            cases,
            results,
            score_defaults={"weight": 1.0, "correctness_min": 0.5, "hallucination_max": 0.5},
            score_targets_by_case={
                "FF-001": {
                    "case_id": "FF-001",
                    "weight": 2.0,
                    "correctness_min": 0.85,
                    "hallucination_max": 0.0,
                },
                "FF-002": {
                    "case_id": "FF-002",
                    "weight": 1.0,
                    "correctness_min": 0.3,
                    "hallucination_max": 0.0,
                },
            },
        )
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
        self.assertEqual(m["thresholds_evaluated"], 2)
        self.assertAlmostEqual(m["weighted_correctness"], 0.6667, places=4)
        self.assertFalse(m["overall_pass"])
        self.assertEqual(report["per_case_threshold_pass"]["FF-001"], True)
        self.assertEqual(report["per_case_threshold_pass"]["FF-002"], False)
        self.assertEqual(report["threshold_missing_case_ids"], [])
        self.assertEqual(report["failed_threshold_case_ids_capped"], ["FF-002"])

    def test_missing_expected_scores_are_null_and_excluded(self) -> None:
        cases = [{"case_id": "FF-010", "expected_status": "answered"}]
        results = [
            CaseResult(
                case_id="FF-010",
                fixture_id="f",
                question="q",
                judge={
                    "ok": True,
                    "verdict": {
                        "is_correct": True,
                        "hallucination": False,
                        "score_0_to_1": 0.88,
                        "reason": "ok",
                    },
                },
            )
        ]
        report = aggregate_report(cases, results, score_defaults={}, score_targets_by_case={})
        self.assertIsNone(report["per_case_threshold_pass"]["FF-010"])
        self.assertEqual(report["threshold_missing_case_ids"], ["FF-010"])
        self.assertIsNone(report["metrics"]["overall_pass"])

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
                "thresholds_evaluated": 0,
                "weighted_correctness": None,
                "overall_pass": None,
                "status_breakdown": {
                    "answered": 1,
                    "not_covered": 0,
                    "other_or_missing": 0,
                },
            },
            "failed_case_ids": [],
            "failed_threshold_case_ids": [],
            "failed_threshold_case_ids_capped": [],
            "threshold_missing_case_ids": [],
            "per_case_threshold_pass": {},
        }
        text = render_terminal_report(report)
        self.assertIn("correctness %: 100.0%", text)
        self.assertIn("hallucination count: 0", text)
        self.assertIn("weighted correctness: n/a", text)
        self.assertIn("overall pass: n/a", text)

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
            self.assertIn("per_case_threshold_pass", payload)


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
                # SCRUM-571: refusal slot lives between normalized + judge
                # so the artifact key order remains stable across runs.
                "refusal",
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

    def test_build_contracts_includes_inventory_fallback_case(self) -> None:
        import json
        import tempfile

        with tempfile.TemporaryDirectory() as tmp:
            tmp_dir = Path(tmp)
            eval_path = tmp_dir / "eval.json"
            score_path = tmp_dir / "scores.json"
            eval_path.write_text(
                json.dumps({"version": "1.0.0", "description": "x", "cases": []}),
                encoding="utf-8",
            )
            score_path.write_text(
                json.dumps(
                    {
                        "version": "1.0.0",
                        "description": "x",
                        "defaults": {
                            "weight": 1.0,
                            "correctness_min": 0.8,
                            "hallucination_max": 0.2,
                        },
                        "case_targets": [
                            {
                                "case_id": "FF-099",
                                "weight": 1.0,
                                "correctness_min": 0.8,
                                "hallucination_max": 0.2,
                            }
                        ],
                    }
                ),
                encoding="utf-8",
            )
            contracts = build_judge_contracts(
                eval_path,
                score_path,
                inventory_cases=[
                    {
                        "case_id": "FF-099",
                        "fixture_id": "no_content_not_covered_path",
                        "question": "Q?",
                        "expected_status": "answered",
                        "expected_keywords": ["not_covered"],
                    }
                ],
            )
        self.assertIn("FF-099", contracts)
        self.assertIn("hallucination_constraints", contracts["FF-099"]["case_contract"])
        self.assertEqual(
            contracts["FF-099"]["case_contract"]["hallucination_constraints"]["notes"],
            "Fallback contract synthesized from fixture inventory.",
        )

    def test_build_contracts_prefers_explicit_eval_case_over_inventory(self) -> None:
        contracts = build_judge_contracts(
            _REPO_ROOT / "eval" / "qa" / "eval_cases_v1.json",
            _REPO_ROOT / "eval" / "qa" / "expected_scores_v1.json",
            inventory_cases=[
                {
                    "case_id": "FF-024",
                    "fixture_id": "smoke_session_update_apac_decision",
                    "question": "tampered question",
                    "expected_status": "not_covered",
                }
            ],
        )
        self.assertIn("FF-024", contracts)
        self.assertEqual(
            contracts["FF-024"]["case_contract"]["question"],
            "Who approved the APAC budget in the session update fixture?",
        )


class TestRunSessionWarmup(unittest.TestCase):
    """SCRUM-572: per-session warm-up before the timed inventory."""

    def test_one_call_per_session_id_in_order(self) -> None:
        calls: list[tuple[str, str, str, str]] = []

        def ask(base_url: str, cookie: str, sid: str, q: str) -> tuple[int, str]:
            calls.append((base_url, cookie, sid, q))
            return 200, "{}"

        out = run_session_warmup(
            "http://x",
            "cookie",
            ["s1", "s2", "s3"],
            ask_fn=ask,
        )
        # Exactly one call per session, in the given order.
        self.assertEqual([c[2] for c in calls], ["s1", "s2", "s3"])
        # Returned manifest entries: one per session, latency_ms recorded.
        self.assertEqual(len(out), 3)
        for entry, sid in zip(out, ["s1", "s2", "s3"]):
            self.assertEqual(entry["session_id"], sid)
            self.assertEqual(entry["http_status"], 200)
            self.assertIsNone(entry["error"])
            self.assertIsInstance(entry["latency_ms"], (int, float))

    def test_empty_session_list_returns_empty(self) -> None:
        # The main() path skips run_session_warmup when session_map is
        # empty; this guards against a future caller passing [] anyway.
        out = run_session_warmup("http://x", "cookie", [], ask_fn=lambda *a: (200, "{}"))
        self.assertEqual(out, [])

    def test_transport_error_records_entry_and_continues(self) -> None:
        # Warm-up failures must NOT raise — the inventory loop runs
        # regardless; a failed warm-up just means that session's first
        # inventory case still hits the cold path.
        attempts: list[str] = []

        def ask(base_url: str, cookie: str, sid: str, q: str) -> tuple[int, str]:
            attempts.append(sid)
            if sid == "s2":
                raise OSError("simulated transport failure")
            return 200, "{}"

        out = run_session_warmup("http://x", "cookie", ["s1", "s2", "s3"], ask_fn=ask)
        self.assertEqual(attempts, ["s1", "s2", "s3"], "all sessions attempted")
        self.assertEqual(len(out), 3)
        self.assertIsNone(out[0]["error"])
        self.assertEqual(out[1]["error"], "simulated transport failure")
        self.assertIsNone(out[1]["http_status"])
        self.assertIsNone(out[2]["error"])

    def test_non_200_response_recorded_as_status(self) -> None:
        # A 5xx warm-up means indexing didn't run — record the status
        # so the operator can see it in run_manifest.json. Don't raise.
        def ask(base_url: str, cookie: str, sid: str, q: str) -> tuple[int, str]:
            return 503, "service unavailable"

        out = run_session_warmup("http://x", "cookie", ["s1"], ask_fn=ask)
        self.assertEqual(out[0]["http_status"], 503)
        self.assertIsNone(out[0]["error"])

    def test_custom_warmup_question_used(self) -> None:
        received: list[str] = []

        def ask(base_url: str, cookie: str, sid: str, q: str) -> tuple[int, str]:
            received.append(q)
            return 200, "{}"

        run_session_warmup(
            "http://x",
            "cookie",
            ["s1"],
            ask_fn=ask,
            warmup_question="Custom warm-up prompt.",
        )
        self.assertEqual(received, ["Custom warm-up prompt."])


class TestWriteRunArtifactsWarmupField(unittest.TestCase):
    """SCRUM-572: warm-up entries surface in run_manifest.json."""

    def test_manifest_includes_warmup_calls(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            run_dir = Path(tmp) / "run1"
            warmup_calls = [
                {"session_id": "s1", "http_status": 200, "latency_ms": 31234.5, "error": None},
                {"session_id": "s2", "http_status": 200, "latency_ms": 28100.0, "error": None},
            ]
            write_run_artifacts(
                run_dir,
                run_id="20260101T000000Z",
                base_url="http://localhost:8080",
                inventory_path=Path("/tmp/inventory.json"),
                auto_setup=True,
                dry_run=False,
                session_map={"f1": "s1", "f2": "s2"},
                cases=[],
                results=[],
                warmup_calls=warmup_calls,
            )
            manifest = json.loads((run_dir / "run_manifest.json").read_text())
            self.assertIn("warmup_calls", manifest)
            self.assertEqual(len(manifest["warmup_calls"]), 2)
            self.assertEqual(manifest["warmup_calls"][0]["session_id"], "s1")
            self.assertEqual(manifest["warmup_calls"][0]["latency_ms"], 31234.5)

    def test_manifest_warmup_calls_defaults_to_empty_list(self) -> None:
        # Backward-compat: callers that don't pass warmup_calls (or
        # pass None) get an empty list in the manifest. Existing
        # tests + the dry-run path keep working without changes.
        with tempfile.TemporaryDirectory() as tmp:
            run_dir = Path(tmp) / "run1"
            write_run_artifacts(
                run_dir,
                run_id="20260101T000000Z",
                base_url="http://localhost:8080",
                inventory_path=Path("/tmp/inventory.json"),
                auto_setup=False,
                dry_run=False,
                session_map={},
                cases=[],
                results=[],
            )
            manifest = json.loads((run_dir / "run_manifest.json").read_text())
            self.assertEqual(manifest["warmup_calls"], [])


class TestDetectRefusal(unittest.TestCase):
    """SCRUM-571: classify SessionAsk response bodies as guardrail refusals
    per docs/guardrails/refusal-shape.md."""

    def test_full_contract_returns_classification(self) -> None:
        body = {
            "error": "guardrail_blocked",
            "guardrail": "input_injection",
            "code": "input_injection",
            "user_message": "Question detected to have unsafe content — not processed.",
        }
        self.assertEqual(
            detect_refusal(body),
            {"guardrail": "input_injection", "code": "input_injection"},
        )

    def test_each_known_slug_recognized(self) -> None:
        for slug in (
            "input_injection",
            "input_off_scope",
            "input_too_long",
            "citation_missing",
            "grounding_failed",
        ):
            body = {
                "error": "guardrail_blocked",
                "guardrail": slug,
                "code": slug,
                "user_message": "x",
            }
            self.assertEqual(detect_refusal(body), {"guardrail": slug, "code": slug}, slug)

    def test_unknown_slug_classified_as_other(self) -> None:
        body = {
            "error": "guardrail_blocked",
            "guardrail": "some_future_slug",
            "code": "some_future_slug",
            "user_message": "x",
        }
        # An unrecognized guardrail slug shouldn't get silently bucketed
        # into one of the known counters — it surfaces as `other` so a
        # future rename or typo is visible.
        self.assertEqual(
            detect_refusal(body),
            {"guardrail": "other", "code": "some_future_slug"},
        )

    def test_normal_answer_body_returns_none(self) -> None:
        body = {
            "question": {"id": "q1", "question_text": "Hello?"},
            "answer": {"answer_text": "Hi", "citations": []},
        }
        self.assertIsNone(detect_refusal(body))

    def test_non_dict_returns_none(self) -> None:
        self.assertIsNone(detect_refusal(None))
        self.assertIsNone(detect_refusal([]))
        self.assertIsNone(detect_refusal("not json"))

    def test_missing_required_field_returns_none(self) -> None:
        # Each of the four fields is required by the contract.
        base = {
            "error": "guardrail_blocked",
            "guardrail": "input_injection",
            "code": "input_injection",
            "user_message": "x",
        }
        for missing in ("error", "guardrail", "code", "user_message"):
            body = {k: v for k, v in base.items() if k != missing}
            self.assertIsNone(detect_refusal(body), f"missing {missing} should fail-open")

    def test_empty_string_field_returns_none(self) -> None:
        # Defensive: an empty string in any of the four required fields
        # is treated as "not a real refusal" rather than a special case.
        for k in ("guardrail", "code", "user_message"):
            body = {
                "error": "guardrail_blocked",
                "guardrail": "input_injection",
                "code": "input_injection",
                "user_message": "x",
            }
            body[k] = ""
            self.assertIsNone(detect_refusal(body), f"empty {k} should fail-open")

    def test_wrong_error_value_returns_none(self) -> None:
        body = {
            "error": "some_other_error",
            "guardrail": "input_injection",
            "code": "input_injection",
            "user_message": "x",
        }
        self.assertIsNone(detect_refusal(body))


class TestAggregateReportRefusalRates(unittest.TestCase):
    """SCRUM-571: refusal_count_by_guardrail + citation_rate +
    groundedness_rate computation."""

    def _case_result(
        self,
        case_id: str,
        *,
        refusal: dict[str, str] | None = None,
        http_status: int = 200,
        verdict_ok: bool = True,
        score: float = 1.0,
    ) -> CaseResult:
        res = CaseResult(
            case_id=case_id,
            fixture_id="f",
            question="q",
            http_status=http_status,
            duration_ms=100.0,
        )
        res.refusal = refusal
        if refusal is not None:
            res.judge = {
                "ok": False,
                "error_code": "judge_skipped_due_to_guardrail_refusal",
                "error_message": f"guardrail={refusal['guardrail']} code={refusal['code']}",
                "attempts": 0,
                "verdict": None,
            }
        else:
            res.judge = {
                "ok": verdict_ok,
                "error_code": None,
                "error_message": None,
                "attempts": 1,
                "verdict": {
                    "is_correct": True,
                    "hallucination": False,
                    "score_0_to_1": score,
                    "reason": "ok",
                } if verdict_ok else None,
            }
        return res

    def _cases(self, *case_ids: str) -> list[dict]:
        # Build inventory cases — all marked answered so the bucketing
        # logic treats no-refusal cases as "passed both gates".
        return [
            {"case_id": cid, "expected_status": "answered"}
            for cid in case_ids
        ]

    def test_all_answered_yields_rates_of_one(self) -> None:
        results = [
            self._case_result("c1"),
            self._case_result("c2"),
            self._case_result("c3"),
        ]
        report = aggregate_report(self._cases("c1", "c2", "c3"), results)
        m = report["metrics"]
        self.assertEqual(m["citation_rate"], 1.0)
        self.assertEqual(m["groundedness_rate"], 1.0)
        self.assertEqual(m["refusal_count_by_guardrail"]["citation_missing"], 0)
        self.assertEqual(m["refusal_count_by_guardrail"]["grounding_failed"], 0)

    def test_citation_missing_refusal_drops_citation_rate(self) -> None:
        # 3 cases — 2 answered, 1 refused with citation_missing.
        # citation_rate = 2 / (2 + 1) ≈ 0.6667
        # groundedness_rate = 2 / 2 = 1.0 (refused case didn't reach judge)
        results = [
            self._case_result("c1"),
            self._case_result("c2"),
            self._case_result(
                "c3",
                refusal={"guardrail": "citation_missing", "code": "citation_missing"},
            ),
        ]
        report = aggregate_report(self._cases("c1", "c2", "c3"), results)
        m = report["metrics"]
        self.assertAlmostEqual(m["citation_rate"], 0.6667, places=4)
        self.assertEqual(m["groundedness_rate"], 1.0)
        self.assertEqual(m["refusal_count_by_guardrail"]["citation_missing"], 1)
        self.assertEqual(m["refusal_count_by_guardrail"]["grounding_failed"], 0)

    def test_grounding_failed_refusal_drops_groundedness_rate_only(self) -> None:
        # 3 cases — 2 answered, 1 refused with grounding_failed.
        # citation_rate = 3 / 3 = 1.0 (grounding_failed cases passed citation)
        # groundedness_rate = 2 / 3 ≈ 0.6667
        results = [
            self._case_result("c1"),
            self._case_result("c2"),
            self._case_result(
                "c3",
                refusal={"guardrail": "grounding_failed", "code": "grounding_failed"},
            ),
        ]
        report = aggregate_report(self._cases("c1", "c2", "c3"), results)
        m = report["metrics"]
        self.assertEqual(m["citation_rate"], 1.0)
        self.assertAlmostEqual(m["groundedness_rate"], 0.6667, places=4)
        self.assertEqual(m["refusal_count_by_guardrail"]["grounding_failed"], 1)

    def test_mixed_citation_and_grounding_refusals(self) -> None:
        # 4 cases: 2 answered, 1 citation_missing, 1 grounding_failed.
        # Citation gate denominator = 4 (2 answered + 1 cm + 1 gf).
        # Citation gate passed = 3 (2 answered + 1 gf — gf passed citation).
        # citation_rate = 3/4 = 0.75.
        # Grounding judge denominator = 3 (2 answered + 1 gf).
        # Grounding judge passed = 2 (2 answered).
        # groundedness_rate = 2/3 ≈ 0.6667.
        results = [
            self._case_result("c1"),
            self._case_result("c2"),
            self._case_result(
                "c3",
                refusal={"guardrail": "citation_missing", "code": "citation_missing"},
            ),
            self._case_result(
                "c4",
                refusal={"guardrail": "grounding_failed", "code": "grounding_failed"},
            ),
        ]
        report = aggregate_report(self._cases("c1", "c2", "c3", "c4"), results)
        m = report["metrics"]
        self.assertEqual(m["citation_rate"], 0.75)
        self.assertAlmostEqual(m["groundedness_rate"], 0.6667, places=4)
        self.assertEqual(m["refusal_count_by_guardrail"]["citation_missing"], 1)
        self.assertEqual(m["refusal_count_by_guardrail"]["grounding_failed"], 1)

    def test_input_refusals_excluded_from_output_rates(self) -> None:
        # Input-guardrail refusals never reach the citation / grounding
        # gates — they pre-empt the LLM call entirely. They count in
        # refusal_count_by_guardrail but NOT in the output-rate denoms.
        results = [
            self._case_result("c1"),
            self._case_result(
                "c2",
                refusal={"guardrail": "input_injection", "code": "input_injection"},
            ),
        ]
        report = aggregate_report(self._cases("c1", "c2"), results)
        m = report["metrics"]
        # c1 is the only case that reached either output gate.
        self.assertEqual(m["citation_rate"], 1.0)
        self.assertEqual(m["groundedness_rate"], 1.0)
        self.assertEqual(m["refusal_count_by_guardrail"]["input_injection"], 1)
        self.assertEqual(m["refusal_count_by_guardrail"]["citation_missing"], 0)

    def test_no_cases_reach_gate_yields_null_rates(self) -> None:
        # All cases are input-refused or not_covered: nothing reached
        # the output guardrails. Both rates should be null (= "no signal"
        # — qa_eval_ci.py skips the threshold compare on either side null).
        results = [
            self._case_result(
                "c1",
                refusal={"guardrail": "input_injection", "code": "input_injection"},
            ),
        ]
        report = aggregate_report(self._cases("c1"), results)
        m = report["metrics"]
        self.assertIsNone(m["citation_rate"])
        self.assertIsNone(m["groundedness_rate"])

    def test_other_slug_counted_in_other_bucket(self) -> None:
        results = [
            self._case_result(
                "c1",
                refusal={"guardrail": "other", "code": "future_slug"},
            ),
        ]
        report = aggregate_report(self._cases("c1"), results)
        m = report["metrics"]
        self.assertEqual(m["refusal_count_by_guardrail"]["other"], 1)


class TestAggregateReportP95Latency(unittest.TestCase):
    """SCRUM-562: p95_latency_ms is computed from per-case duration_ms."""

    def _result(self, case_id: str, duration_ms: float | None) -> CaseResult:
        return CaseResult(
            case_id=case_id,
            fixture_id="f",
            question="q",
            judge={"ok": True, "verdict": {"score_0_to_1": 1.0, "is_correct": True}},
            duration_ms=duration_ms,
        )

    def test_p95_over_uniform_durations(self) -> None:
        # 20 results at 100ms — p95 is 100.
        results = [self._result(f"FF-{i:03d}", 100.0) for i in range(20)]
        cases = [{"case_id": r.case_id, "expected_status": "answered"} for r in results]
        report = aggregate_report(cases, results)
        self.assertEqual(report["metrics"]["p95_latency_ms"], 100.0)

    def test_p95_skews_to_tail(self) -> None:
        # 19 cases at 100ms + 1 case at 1000ms — p95 (at int(0.95 * 19) = 18)
        # lands at 100, but adding one more 1000ms case (21 total, p95 at
        # int(0.95 * 20) = 19) should pull it up to 1000.
        results = [self._result(f"FF-{i:03d}", 100.0) for i in range(20)] + [
            self._result("FF-spike-1", 1000.0),
            self._result("FF-spike-2", 1000.0),
        ]
        cases = [{"case_id": r.case_id, "expected_status": "answered"} for r in results]
        report = aggregate_report(cases, results)
        self.assertEqual(report["metrics"]["p95_latency_ms"], 1000.0)

    def test_p95_is_null_when_no_durations(self) -> None:
        results = [self._result("FF-001", None), self._result("FF-002", None)]
        cases = [{"case_id": r.case_id, "expected_status": "answered"} for r in results]
        report = aggregate_report(cases, results)
        self.assertIsNone(report["metrics"]["p95_latency_ms"])


if __name__ == "__main__":
    unittest.main()
