#!/usr/bin/env python3
"""Run Q&A eval inventory cases against TalkBack SessionAsk and write timestamped artifacts.

Environment (network runs):
  TALKBACK_API_BASE — API origin (default: http://localhost:8080). Alias: QA_EVAL_BASE_URL.
  QA_EVAL_COOKIE — Value for the Cookie header (e.g. tb_login=<uuid> from an authenticated browser).
  QA_EVAL_EMAIL / QA_EVAL_PASSWORD — Optional; if QA_EVAL_COOKIE is unset, login via /api/auth/login.
  QA_EVAL_SESSIONS_JSON — JSON object mapping fixture_id -> session UUID. Required when not using --auto-setup.

Example:
  export QA_EVAL_COOKIE='tb_login=...'
  export QA_EVAL_SESSIONS_JSON='{"smokeDocText_meridian_apac_churn":"<uuid>"}'
  python scripts/run_qa_eval.py

Auto local setup (creates one session per fixture in the inventory):
  python scripts/run_qa_eval.py --auto-setup
"""

from __future__ import annotations

import argparse
import json
import os
import sys
import time
import urllib.error
import urllib.request
from collections import OrderedDict
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path
from typing import Any, Callable, Mapping
from uuid import uuid4

# Reuse inventory validation
REPO_ROOT = Path(__file__).resolve().parents[1]
if str(REPO_ROOT) not in sys.path:
    sys.path.insert(0, str(REPO_ROOT))

from scripts.qa_fixture_inventory import load_inventory, validate_inventory  # noqa: E402
from scripts.qa_eval_datasets import load_json  # noqa: E402
from scripts.qa_eval_judge import (
    DEFAULT_JUDGE_MODEL,
    judge_case_with_retry,
)  # noqa: E402

# Deterministic paste/patch payloads aligned with smoke and e2e fixtures.
FIXTURE_SETUP: dict[str, dict[str, Any]] = {
    "smokeDocText_meridian_apac_churn": {
        "paste": (
            "Meridian Report",
            "Meridian proposal approved. APAC expansion budget confirmed at 2.4M. "
            "Churn reduced to below 6% this quarter.",
        ),
    },
    "qa_history_project_omega": {
        "paste": (
            "History Report",
            "Decision was made to proceed with Project Omega at 1.2M budget.",
        ),
    },
    "smoke_session_update_apac_decision": {
        "paste": (
            "APAC Context Report",
            "We need to decide on the APAC expansion. Approve 2.4M budget for APAC.",
        ),
        "patch_session": {
            "title": "Revised Title",
            "premise": "We need to decide on the APAC expansion.",
            "primary_decision": "Approve 2.4M budget for APAC.",
        },
    },
    "no_content_not_covered_path": {},
}


def default_base_url() -> str:
    return (
        os.environ.get("QA_EVAL_BASE_URL")
        or os.environ.get("TALKBACK_API_BASE")
        or "http://localhost:8080"
    ).rstrip("/")


def utc_run_id() -> str:
    return datetime.now(timezone.utc).strftime("%Y%m%dT%H%M%SZ")


def parse_sessions_json(raw: str | None) -> dict[str, str]:
    if not raw or not raw.strip():
        return {}
    data = json.loads(raw)
    if not isinstance(data, dict):
        raise ValueError("QA_EVAL_SESSIONS_JSON must be a JSON object")
    out: dict[str, str] = {}
    for k, v in data.items():
        if not isinstance(k, str) or not isinstance(v, str):
            raise ValueError("QA_EVAL_SESSIONS_JSON keys and values must be strings")
        out[k] = v.strip()
    return out


def collect_cookie_header_value(resp: Any) -> str:
    """Build a Cookie header value from Set-Cookie response headers."""
    headers = resp.headers
    parts: list[str] = []
    get_all = getattr(headers, "get_all", None)
    if callable(get_all):
        for item in get_all("Set-Cookie") or []:
            parts.append(item.split(";", 1)[0].strip())
    else:
        raw = headers.get("Set-Cookie")
        if raw:
            for line in raw.split("\n"):
                line = line.strip()
                if line:
                    parts.append(line.split(";", 1)[0].strip())
    return "; ".join(parts)


def http_json(
    method: str,
    url: str,
    *,
    cookie: str | None,
    body: dict[str, Any] | None = None,
    timeout: float = 120.0,
) -> tuple[int, str]:
    data = None
    headers = {"Accept": "application/json"}
    if body is not None:
        data = json.dumps(body).encode("utf-8")
        headers["Content-Type"] = "application/json"
    if cookie:
        headers["Cookie"] = cookie
    req = urllib.request.Request(url, data=data, headers=headers, method=method)
    try:
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            text = resp.read().decode("utf-8", errors="replace")
            return resp.status, text
    except urllib.error.HTTPError as e:
        text = e.read().decode("utf-8", errors="replace")
        return e.code, text


def login_cookie(base_url: str, email: str, password: str) -> str:
    url = f"{base_url}/api/auth/login"
    data = json.dumps({"email": email, "password": password}).encode("utf-8")
    req = urllib.request.Request(
        url,
        data=data,
        headers={"Content-Type": "application/json", "Accept": "application/json"},
        method="POST",
    )
    try:
        with urllib.request.urlopen(req, timeout=60.0) as resp:
            if resp.status not in (200, 201):
                raise RuntimeError(f"login failed: HTTP {resp.status}")
            cookie = collect_cookie_header_value(resp)
            if not cookie:
                raise RuntimeError("login succeeded but no Set-Cookie returned")
            return cookie
    except urllib.error.HTTPError as e:
        detail = e.read().decode("utf-8", errors="replace")
        raise RuntimeError(f"login failed: HTTP {e.code}: {detail}") from e


def resolve_auth_cookie(base_url: str) -> str:
    c = os.environ.get("QA_EVAL_COOKIE", "").strip()
    if c:
        return c
    email = os.environ.get("QA_EVAL_EMAIL", "").strip()
    password = os.environ.get("QA_EVAL_PASSWORD", "").strip()
    if email and password:
        return login_cookie(base_url, email, password)
    return ""


def create_session(base_url: str, cookie: str, title: str) -> str:
    status, text = http_json(
        "POST",
        f"{base_url}/sessions",
        cookie=cookie,
        body={"title": title},
    )
    if status != 201:
        raise RuntimeError(f"create session failed: HTTP {status}: {text}")
    payload = json.loads(text)
    sid = payload.get("id")
    if not isinstance(sid, str):
        raise RuntimeError(f"unexpected create session response: {text[:500]}")
    return sid


def paste_material(base_url: str, cookie: str, session_id: str, title: str, text: str) -> None:
    status, body = http_json(
        "POST",
        f"{base_url}/sessions/{session_id}/materials/paste",
        cookie=cookie,
        body={"title": title, "text": text},
    )
    if status != 201:
        raise RuntimeError(f"paste material failed: HTTP {status}: {body}")


def patch_session(base_url: str, cookie: str, session_id: str, fields: dict[str, Any]) -> None:
    status, body = http_json(
        "PATCH",
        f"{base_url}/api/sessions/{session_id}",
        cookie=cookie,
        body=fields,
    )
    if status != 200:
        raise RuntimeError(f"patch session failed: HTTP {status}: {body}")


def auto_setup_sessions(
    base_url: str,
    cookie: str,
    fixture_ids: list[str],
    run_id: str,
) -> dict[str, str]:
    mapping: dict[str, str] = {}
    for fid in fixture_ids:
        spec = FIXTURE_SETUP.get(fid)
        if spec is None:
            raise KeyError(f"unknown fixture_id for auto-setup: {fid}")
        title = f"QA eval {fid} {run_id} {uuid4().hex[:8]}"
        session_id = create_session(base_url, cookie, title)
        paste = spec.get("paste")
        if paste:
            paste_material(base_url, cookie, session_id, paste[0], paste[1])
        patch = spec.get("patch_session")
        if patch:
            patch_payload = dict(patch)
            if isinstance(patch_payload.get("title"), str):
                patch_payload["title"] = f"{patch_payload['title']} {run_id} {uuid4().hex[:8]}"
            patch_session(base_url, cookie, session_id, patch_payload)
        mapping[fid] = session_id
    return mapping


def normalize_session_ask_response(parsed: Any) -> dict[str, Any]:
    """Extract stable fields from a SessionAsk JSON body (or error payload)."""
    out: dict[str, Any] = {
        "answer_text": None,
        "citation_count": None,
        "llm_model": None,
        "question_id": None,
        "question_text": None,
    }
    if not isinstance(parsed, dict):
        return out
    ans = parsed.get("answer")
    if isinstance(ans, dict):
        at = ans.get("answer_text")
        out["answer_text"] = at if isinstance(at, str) else None
        cites = ans.get("citations")
        if isinstance(cites, list):
            out["citation_count"] = len(cites)
        lm = ans.get("llm_model")
        out["llm_model"] = lm if isinstance(lm, str) else None
    q = parsed.get("question")
    if isinstance(q, dict):
        qid = q.get("id")
        out["question_id"] = qid if isinstance(qid, str) else None
        qt = q.get("question_text")
        out["question_text"] = qt if isinstance(qt, str) else None
    return out


# SCRUM-571: known refusal-guardrail slugs. Kept in sync with the
# `guardrails_fired` enum in docs/guardrails/log-shape.md and the
# catalog in docs/guardrails/refusal-shape.md. Used to validate the
# slug on a refusal body so a typo or rename surfaces as `other`
# rather than getting silently miscounted.
KNOWN_REFUSAL_GUARDRAILS = frozenset(
    {
        "input_injection",
        "input_off_scope",
        "input_too_long",
        "citation_missing",
        "grounding_failed",
    }
)


def detect_refusal(parsed: Any) -> dict[str, str] | None:
    """SCRUM-571: classify a SessionAsk response body as a guardrail
    refusal per the contract in docs/guardrails/refusal-shape.md.

    Returns a dict ``{"guardrail": <slug>, "code": <code>}`` when the
    parsed body matches::

        {"error": "guardrail_blocked",
         "guardrail": <slug>,
         "code": <code>,
         "user_message": <string>}

    The four fields are required by the contract; any missing or non-
    string field causes the detection to fail-open (return None) so a
    legit answer body never gets mis-classified as a refusal.

    Returns ``{"guardrail": "other", "code": "<raw>"}`` when the
    guardrail slug is non-empty but not in KNOWN_REFUSAL_GUARDRAILS —
    catches a future rename / typo before the rate computation skews.
    """
    if not isinstance(parsed, dict):
        return None
    if parsed.get("error") != "guardrail_blocked":
        return None
    guardrail = parsed.get("guardrail")
    code = parsed.get("code")
    user_message = parsed.get("user_message")
    if not isinstance(guardrail, str) or not guardrail:
        return None
    if not isinstance(code, str) or not code:
        return None
    if not isinstance(user_message, str) or not user_message:
        return None
    if guardrail not in KNOWN_REFUSAL_GUARDRAILS:
        return {"guardrail": "other", "code": guardrail}
    return {"guardrail": guardrail, "code": code}


@dataclass
class CaseResult:
    case_id: str
    fixture_id: str
    question: str
    http_status: int | None = None
    raw_body: str = ""
    parsed_json: Any | None = None
    error: str | None = None
    skipped_reason: str | None = None
    request_url: str = ""
    duration_ms: float | None = None
    judge: dict[str, Any] | None = None
    # SCRUM-571: populated when the response body matches the guardrail
    # refusal contract (docs/guardrails/refusal-shape.md). When set, the
    # judge step is skipped with judge_skipped_due_to_guardrail_refusal
    # and aggregate_report counts the case toward the relevant rate
    # denominator. `None` for normal answers / not_covered / errors.
    refusal: dict[str, str] | None = None


def ask_session(
    base_url: str,
    cookie: str,
    session_id: str,
    question_text: str,
) -> tuple[int, str]:
    return http_json(
        "POST",
        f"{base_url}/api/sessions/{session_id}/ask",
        cookie=cookie,
        body={"question_text": question_text, "asked_via": "text"},
        timeout=180.0,
    )


def run_inventory_cases(
    *,
    base_url: str,
    cookie: str,
    session_for_fixture: Mapping[str, str],
    cases: list[dict[str, Any]],
    ask_fn: Callable[[str, str, str, str], tuple[int, str]] | None = None,
    provision_session_fn: Callable[[str], str] | None = None,
    judge_contract_by_case: Mapping[str, dict[str, Any]] | None = None,
    judge_fn: Callable[[dict[str, Any], dict[str, Any], str, int | None], dict[str, Any]]
    | None = None,
) -> list[CaseResult]:
    _ask = ask_fn or ask_session
    results: list[CaseResult] = []
    for case in cases:
        cid = case.get("case_id", "")
        fid = case.get("fixture_id", "")
        q = case.get("question", "")
        res = CaseResult(case_id=str(cid), fixture_id=str(fid), question=str(q))
        sid = session_for_fixture.get(str(fid))
        if not sid:
            res.skipped_reason = f"no session mapped for fixture_id={fid}"
            results.append(res)
            continue
        res.request_url = f"{base_url}/api/sessions/{sid}/ask"
        t0 = time.perf_counter()
        try:
            status, text = _ask(base_url, cookie, sid, str(q))
            if (
                status == 429
                and provision_session_fn is not None
                and "session question limit reached" in (text or "").lower()
            ):
                # SessionAsk enforces per-session question cap; provision a fresh session and retry once.
                sid = provision_session_fn(str(fid))
                res.request_url = f"{base_url}/api/sessions/{sid}/ask"
                status, text = _ask(base_url, cookie, sid, str(q))
            res.http_status = status
            res.raw_body = text
            try:
                res.parsed_json = json.loads(text) if text else None
            except json.JSONDecodeError:
                res.parsed_json = None
                res.error = "response is not valid JSON"
        except OSError as e:
            res.error = str(e)
            res.http_status = None

        # SCRUM-571: stamp the refusal classification before judging.
        # A guardrail refusal has http_status=200 + a structured body
        # per refusal-shape.md; the judge can't score it and must be
        # skipped with a distinct reason so the case doesn't pollute
        # the judge_error count.
        res.refusal = detect_refusal(res.parsed_json)

        if judge_fn is not None:
            contract = (judge_contract_by_case or {}).get(res.case_id)
            if contract is None:
                res.judge = {
                    "ok": False,
                    "error_code": "judge_missing_case_contract",
                    "error_message": f"no judge contract for case_id={res.case_id}",
                    "attempts": 0,
                    "verdict": None,
                }
            elif res.refusal is not None:
                # SCRUM-571: deliberate guardrail refusal — not a judge
                # failure. The case is counted in
                # refusal_count_by_guardrail and contributes to the
                # citation_rate / groundedness_rate denominators in
                # aggregate_report.
                res.judge = {
                    "ok": False,
                    "error_code": "judge_skipped_due_to_guardrail_refusal",
                    "error_message": (
                        f"guardrail={res.refusal['guardrail']} "
                        f"code={res.refusal['code']}"
                    ),
                    "attempts": 0,
                    "verdict": None,
                }
            elif res.http_status not in (200, 201):
                res.judge = {
                    "ok": False,
                    "error_code": "judge_skipped_due_to_ask_error",
                    "error_message": f"ask did not return success status ({res.http_status})",
                    "attempts": 0,
                    "verdict": None,
                }
            else:
                normalized = normalize_session_ask_response(res.parsed_json)
                answer_text = normalized.get("answer_text")
                if not isinstance(answer_text, str) or not answer_text.strip():
                    res.judge = {
                        "ok": False,
                        "error_code": "judge_skipped_empty_answer",
                        "error_message": "answer_text missing in parsed response",
                        "attempts": 0,
                        "verdict": None,
                    }
                else:
                    res.judge = judge_fn(
                        contract["case_contract"],
                        contract["score_target"],
                        answer_text,
                        res.http_status,
                    )
        res.duration_ms = (time.perf_counter() - t0) * 1000.0
        results.append(res)
    return results


def write_json(path: Path, obj: Any) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(obj, indent=2, ensure_ascii=False) + "\n", encoding="utf-8")


def _inventory_path_for_manifest(inventory_path: Path) -> str:
    try:
        return str(inventory_path.resolve().relative_to(REPO_ROOT))
    except ValueError:
        return str(inventory_path.resolve())


def case_to_artifact(case: dict[str, Any], result: CaseResult) -> dict[str, Any]:
    return OrderedDict(
        [
            ("case_id", result.case_id),
            ("fixture_id", result.fixture_id),
            ("question", result.question),
            ("inventory_expected_status", case.get("expected_status")),
            ("request", {"url": result.request_url, "method": "POST"}),
            (
                "response",
                OrderedDict(
                    [
                        ("http_status", result.http_status),
                        ("duration_ms", result.duration_ms),
                        ("skipped_reason", result.skipped_reason),
                        ("error", result.error),
                        ("raw_body_text", result.raw_body),
                        ("parsed_json", result.parsed_json),
                    ]
                ),
            ),
            ("normalized", normalize_session_ask_response(result.parsed_json)),
            # SCRUM-571: surface the refusal classification in the
            # per-case artifact so post-run debugging doesn't require
            # re-parsing raw_body_text. None for non-refused cases.
            ("refusal", result.refusal),
            ("judge", result.judge),
        ]
    )


def write_run_artifacts(
    run_dir: Path,
    *,
    run_id: str,
    base_url: str,
    inventory_path: Path,
    auto_setup: bool,
    dry_run: bool,
    session_map: Mapping[str, str],
    cases: list[dict[str, Any]],
    results: list[CaseResult],
) -> None:
    cases_dir = run_dir / "cases"
    cases_dir.mkdir(parents=True, exist_ok=True)
    index: list[dict[str, Any]] = []
    for case, result in zip(cases, results):
        artifact = case_to_artifact(case, result)
        write_json(cases_dir / f"{result.case_id}.json", artifact)
        index.append(
            {
                "case_id": result.case_id,
                "fixture_id": result.fixture_id,
                "http_status": result.http_status,
                "skipped_reason": result.skipped_reason,
                "error": result.error,
                "artifact": f"cases/{result.case_id}.json",
            }
        )
    manifest = {
        "run_id": run_id,
        "started_at": run_id,
        "base_url": base_url,
        "inventory_path": _inventory_path_for_manifest(inventory_path),
        "auto_setup": auto_setup,
        "dry_run": dry_run,
        "session_map": dict(session_map),
        "case_count": len(results),
        "cases_index": index,
    }
    write_json(run_dir / "run_manifest.json", manifest)


def aggregate_report(
    cases: list[dict[str, Any]],
    results: list[CaseResult],
    *,
    score_defaults: Mapping[str, Any] | None = None,
    score_targets_by_case: Mapping[str, Mapping[str, Any]] | None = None,
    failed_case_cap: int = 5,
) -> dict[str, Any]:
    case_map = {
        str(c.get("case_id")): c
        for c in cases
        if isinstance(c, dict) and isinstance(c.get("case_id"), str)
    }

    totals = {
        "total_cases": len(results),
        "judge_attempted": 0,
        "judge_ok": 0,
        "judge_error": 0,
        "correct_count": 0,
        "hallucination_count": 0,
        "status_breakdown": {
            "answered": 0,
            "not_covered": 0,
            "other_or_missing": 0,
        },
        "thresholds_evaluated": 0,
        "weighted_correctness": None,
        "overall_pass": None,
        # SCRUM-571: per-guardrail refusal tally. Pre-seeded with the
        # known slugs so a downstream consumer can rely on the keys
        # existing even when the count is zero.
        "refusal_count_by_guardrail": {
            "input_injection": 0,
            "input_off_scope": 0,
            "input_too_long": 0,
            "citation_missing": 0,
            "grounding_failed": 0,
            "other": 0,
        },
    }
    # SCRUM-571: track the buckets that feed citation_rate +
    # groundedness_rate. Computed after the case loop.
    cases_reached_citation_gate = 0  # answered + citation_missing + grounding_failed
    cases_passed_citation_gate = 0   # answered + grounding_failed
    cases_reached_grounding_judge = 0  # answered + grounding_failed
    cases_passed_grounding_judge = 0   # answered
    failed_case_ids: list[str] = []
    per_case_threshold_pass: dict[str, bool | None] = {}
    threshold_missing_case_ids: list[str] = []
    weighted_num = 0.0
    weighted_den = 0.0

    for res in results:
        expected_status = case_map.get(res.case_id, {}).get("expected_status")
        if expected_status == "answered":
            totals["status_breakdown"]["answered"] += 1
        elif expected_status == "not_covered":
            totals["status_breakdown"]["not_covered"] += 1
        else:
            totals["status_breakdown"]["other_or_missing"] += 1

        # SCRUM-571: tally refusals + bucket cases for citation_rate /
        # groundedness_rate. The semantics mirror the qa.go flow:
        #   - input_*  refusals never reach the citation/grounding
        #     guardrails — counted in refusal_count_by_guardrail only.
        #   - citation_missing refusals reached the citation guardrail
        #     and failed it.
        #   - grounding_failed refusals reached the citation guardrail
        #     (passed) and reached the grounding judge (failed).
        #   - cases with no refusal that *attempted* an `answered`
        #     response passed both gates.
        if res.refusal is not None:
            slug = res.refusal["guardrail"]
            totals["refusal_count_by_guardrail"][slug] = (
                totals["refusal_count_by_guardrail"].get(slug, 0) + 1
            )
            if slug == "citation_missing":
                cases_reached_citation_gate += 1
            elif slug == "grounding_failed":
                cases_reached_citation_gate += 1
                cases_passed_citation_gate += 1
                cases_reached_grounding_judge += 1
        elif expected_status == "answered" and res.http_status in (200, 201):
            # No refusal + the case was supposed to answer + the
            # request succeeded => the response passed both output
            # guardrails (CheckCitations + CheckGrounding) in qa.go.
            cases_reached_citation_gate += 1
            cases_passed_citation_gate += 1
            cases_reached_grounding_judge += 1
            cases_passed_grounding_judge += 1

        j = res.judge
        if not isinstance(j, dict):
            continue
        totals["judge_attempted"] += 1
        if j.get("ok") is True and isinstance(j.get("verdict"), dict):
            totals["judge_ok"] += 1
            verdict = j["verdict"]
            score_value = verdict.get("score_0_to_1")
            if verdict.get("is_correct") is True:
                totals["correct_count"] += 1
            if verdict.get("hallucination") is True:
                totals["hallucination_count"] += 1

            score_cfg = (score_targets_by_case or {}).get(res.case_id)
            if score_cfg is None:
                per_case_threshold_pass[res.case_id] = None
                threshold_missing_case_ids.append(res.case_id)
                continue

            correctness_min = score_cfg.get(
                "correctness_min",
                (score_defaults or {}).get("correctness_min"),
            )
            hallucination_max = score_cfg.get(
                "hallucination_max",
                (score_defaults or {}).get("hallucination_max"),
            )
            weight = score_cfg.get("weight", (score_defaults or {}).get("weight", 1.0))

            if not isinstance(score_value, (int, float)) or not isinstance(
                correctness_min, (int, float)
            ) or not isinstance(hallucination_max, (int, float)):
                per_case_threshold_pass[res.case_id] = None
                threshold_missing_case_ids.append(res.case_id)
                continue

            hallucination_val = 1.0 if verdict.get("hallucination") is True else 0.0
            threshold_pass = (
                float(score_value) >= float(correctness_min)
                and hallucination_val <= float(hallucination_max)
            )
            per_case_threshold_pass[res.case_id] = threshold_pass
            totals["thresholds_evaluated"] += 1

            if isinstance(weight, (int, float)) and float(weight) > 0:
                weighted_num += float(score_value) * float(weight)
                weighted_den += float(weight)
        else:
            totals["judge_error"] += 1
            failed_case_ids.append(res.case_id)

    denom = totals["judge_ok"] if totals["judge_ok"] > 0 else 0
    correctness_pct = (
        round((totals["correct_count"] / denom) * 100.0, 2) if denom > 0 else None
    )
    if weighted_den > 0:
        totals["weighted_correctness"] = round(weighted_num / weighted_den, 4)

    # SCRUM-562: p95_latency_ms over per-case duration_ms (already recorded by
    # the case loop). Null when no cases produced a duration (dry runs, mass
    # transport errors). Computed inline rather than via statistics.quantiles
    # so the math is identical across Python versions and obvious to read.
    durations = sorted(r.duration_ms for r in results if r.duration_ms is not None)
    if durations:
        idx = int(round(0.95 * (len(durations) - 1)))
        totals["p95_latency_ms"] = round(float(durations[idx]), 1)
    else:
        totals["p95_latency_ms"] = None

    # SCRUM-571: citation_rate = cases that passed CheckCitations /
    # cases that reached it. Null when no cases reached the gate
    # (e.g. all input-refused or all not_covered). Matches the
    # baseline-compare semantics in scripts/qa_eval_ci.py — a drop
    # below the threshold floor flags a regression where qa.go's
    # output guardrails are letting more ungrounded answers through.
    if cases_reached_citation_gate > 0:
        totals["citation_rate"] = round(
            cases_passed_citation_gate / cases_reached_citation_gate, 4
        )
    else:
        totals["citation_rate"] = None

    # SCRUM-571: groundedness_rate = cases the judge OK'd / cases that
    # reached the judge. Null when no cases reached it (e.g. all
    # citation_missing-refused upstream).
    if cases_reached_grounding_judge > 0:
        totals["groundedness_rate"] = round(
            cases_passed_grounding_judge / cases_reached_grounding_judge, 4
        )
    else:
        totals["groundedness_rate"] = None

    evaluated = [
        v for v in per_case_threshold_pass.values() if isinstance(v, bool)
    ]
    totals["overall_pass"] = all(evaluated) if evaluated else None
    failed_threshold_case_ids = [k for k, v in per_case_threshold_pass.items() if v is False]

    return {
        "metrics": {
            **totals,
            "correctness_percentage": correctness_pct,
        },
        "failed_case_ids": failed_case_ids,
        "failed_threshold_case_ids": failed_threshold_case_ids,
        "failed_threshold_case_ids_capped": failed_threshold_case_ids[: max(1, failed_case_cap)],
        "threshold_missing_case_ids": threshold_missing_case_ids,
        "per_case_threshold_pass": per_case_threshold_pass,
    }


def write_report_artifact(
    run_dir: Path,
    *,
    run_id: str,
    inventory_path: Path,
    report: dict[str, Any],
) -> None:
    report_payload = {
        "run_id": run_id,
        "inventory_path": _inventory_path_for_manifest(inventory_path),
        "source_run_manifest": "run_manifest.json",
        "source_case_artifacts_glob": "cases/*.json",
        "metrics": report["metrics"],
        "failed_case_ids": report["failed_case_ids"],
        "failed_threshold_case_ids": report["failed_threshold_case_ids"],
        "failed_threshold_case_ids_capped": report["failed_threshold_case_ids_capped"],
        "threshold_missing_case_ids": report["threshold_missing_case_ids"],
        "per_case_threshold_pass": report["per_case_threshold_pass"],
        "generated_at": datetime.now(timezone.utc).isoformat(),
    }
    write_json(run_dir / "report.json", report_payload)


def render_terminal_report(report: dict[str, Any]) -> str:
    m = report["metrics"]
    correctness_text = (
        f"{m['correctness_percentage']}%"
        if m["correctness_percentage"] is not None
        else "n/a"
    )
    return (
        "Eval summary:\n"
        f"- correctness %: {correctness_text}\n"
        f"- hallucination count: {m['hallucination_count']}\n"
        f"- weighted correctness: {m['weighted_correctness'] if m['weighted_correctness'] is not None else 'n/a'}\n"
        f"- overall pass: {m['overall_pass'] if m['overall_pass'] is not None else 'n/a'}\n"
        f"- thresholds evaluated: {m['thresholds_evaluated']}\n"
        f"- threshold misses (top): {report['failed_threshold_case_ids_capped']}\n"
        f"- judge attempted: {m['judge_attempted']}\n"
        f"- judge ok: {m['judge_ok']}\n"
        f"- judge errors: {m['judge_error']}\n"
        "- status breakdown: "
        f"answered={m['status_breakdown']['answered']}, "
        f"not_covered={m['status_breakdown']['not_covered']}, "
        f"other_or_missing={m['status_breakdown']['other_or_missing']}"
    )


def ordered_fixture_ids(cases: list[dict[str, Any]]) -> list[str]:
    seen: OrderedDict[str, None] = OrderedDict()
    for c in cases:
        fid = str(c.get("fixture_id", ""))
        if fid and fid not in seen:
            seen[fid] = None
    return list(seen.keys())


def build_judge_contracts(
    eval_cases_path: Path,
    expected_scores_path: Path,
    *,
    inventory_cases: list[dict[str, Any]] | None = None,
) -> dict[str, dict[str, Any]]:
    eval_data = load_json(eval_cases_path)
    scores_data = load_json(expected_scores_path)

    eval_cases = eval_data.get("cases")
    score_targets = scores_data.get("case_targets")
    if not isinstance(eval_cases, list) or not isinstance(score_targets, list):
        raise ValueError("eval_cases and expected_scores must contain array fields")

    eval_by_id: dict[str, dict[str, Any]] = {}
    for c in eval_cases:
        if isinstance(c, dict) and isinstance(c.get("case_id"), str):
            eval_by_id[c["case_id"]] = c
    if inventory_cases:
        for c in inventory_cases:
            if isinstance(c, dict) and isinstance(c.get("case_id"), str):
                case_id = c["case_id"]
                if case_id in eval_by_id:
                    continue
                eval_by_id[case_id] = {
                    "case_id": case_id,
                    "fixture_id": c.get("fixture_id"),
                    "question": c.get("question"),
                    "expected_status": c.get("expected_status"),
                    "expected_keywords": c.get("expected_keywords"),
                    "hallucination_constraints": {
                        "answer_must_not_contain": [],
                        "notes": "Fallback contract synthesized from fixture inventory.",
                    },
                }

    scores_by_id: dict[str, dict[str, Any]] = {}
    for t in score_targets:
        if isinstance(t, dict) and isinstance(t.get("case_id"), str):
            scores_by_id[t["case_id"]] = t

    contracts: dict[str, dict[str, Any]] = {}
    for case_id, case_contract in eval_by_id.items():
        st = scores_by_id.get(case_id)
        if st is None:
            continue
        contracts[case_id] = {"case_contract": case_contract, "score_target": st}
    return contracts


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--inventory",
        type=Path,
        default=REPO_ROOT / "eval" / "qa" / "fixture_fact_inventory.json",
        help="Path to eval case JSON",
    )
    parser.add_argument(
        "--out-dir",
        type=Path,
        default=REPO_ROOT / "eval" / "qa" / "runs",
        help="Directory under which a per-run folder is created",
    )
    parser.add_argument(
        "--run-id",
        type=str,
        default="",
        help="Override run folder name (default: UTC timestamp)",
    )
    parser.add_argument(
        "--dry-run",
        action="store_true",
        help="Validate inventory and write artifacts without calling SessionAsk",
    )
    parser.add_argument(
        "--auto-setup",
        action="store_true",
        help="Create sessions (and paste/patch) per fixture via API; requires auth cookie or login env",
    )
    parser.add_argument(
        "--eval-cases",
        type=Path,
        default=REPO_ROOT / "eval" / "qa" / "eval_cases_v1.json",
        help="Path to eval_cases JSON used for judge rubric",
    )
    parser.add_argument(
        "--expected-scores",
        type=Path,
        default=REPO_ROOT / "eval" / "qa" / "expected_scores_v1.json",
        help="Path to expected_scores JSON used for judge constraints",
    )
    parser.add_argument(
        "--no-judge",
        action="store_true",
        help="Disable per-case LLM judge invocation",
    )
    parser.add_argument(
        "--judge-model",
        type=str,
        default=os.environ.get("QA_EVAL_JUDGE_MODEL", DEFAULT_JUDGE_MODEL),
        help="OpenAI model for judge invocation",
    )
    parser.add_argument(
        "--judge-max-attempts",
        type=int,
        default=2,
        help="Max judge attempts for retryable errors",
    )
    parser.add_argument(
        "--judge-timeout-seconds",
        type=float,
        default=45.0,
        help="Timeout for one judge call",
    )
    args = parser.parse_args(argv)

    inventory_path = args.inventory.resolve()
    data = load_inventory(inventory_path)
    errors = validate_inventory(data)
    if errors:
        print("Inventory validation failed:", file=sys.stderr)
        for e in errors:
            print(f"  - {e}", file=sys.stderr)
        return 2

    cases = data.get("cases")
    assert isinstance(cases, list)

    run_id = args.run_id.strip() or utc_run_id()
    run_dir = (args.out_dir / run_id).resolve()
    run_dir.mkdir(parents=True, exist_ok=True)

    base_url = default_base_url()
    auto_setup = bool(args.auto_setup)
    dry_run = bool(args.dry_run)
    judge_enabled = not dry_run and not args.no_judge

    session_map: dict[str, str] = {}
    cookie = ""

    if dry_run:
        results = [
            CaseResult(
                case_id=str(c.get("case_id", "")),
                fixture_id=str(c.get("fixture_id", "")),
                question=str(c.get("question", "")),
                skipped_reason="dry_run",
            )
            for c in cases
        ]
        write_run_artifacts(
            run_dir,
            run_id=run_id,
            base_url=base_url,
            inventory_path=inventory_path,
            auto_setup=auto_setup,
            dry_run=True,
            session_map={},
            cases=cases,
            results=results,
        )
        print(f"Wrote dry-run artifacts to {run_dir}")
        return 0

    cookie = resolve_auth_cookie(base_url)
    if not cookie:
        print(
            "Auth required: set QA_EVAL_COOKIE or QA_EVAL_EMAIL and QA_EVAL_PASSWORD",
            file=sys.stderr,
        )
        return 3

    if auto_setup:
        fids = ordered_fixture_ids(cases)
        for fid in fids:
            if fid not in FIXTURE_SETUP:
                print(f"Unknown fixture_id for auto-setup: {fid}", file=sys.stderr)
                return 4
        session_map = auto_setup_sessions(base_url, cookie, fids, run_id)
    else:
        session_map = parse_sessions_json(os.environ.get("QA_EVAL_SESSIONS_JSON", ""))
        missing = [f for f in ordered_fixture_ids(cases) if f not in session_map]
        if missing:
            print(
                "QA_EVAL_SESSIONS_JSON missing fixture_ids: "
                + ", ".join(missing),
                file=sys.stderr,
            )
            return 4

    judge_contracts: dict[str, dict[str, Any]] = {}
    if judge_enabled:
        try:
            judge_contracts = build_judge_contracts(
                args.eval_cases.resolve(),
                args.expected_scores.resolve(),
                inventory_cases=cases,
            )
        except (OSError, json.JSONDecodeError, ValueError) as e:
            print(f"Failed to load judge contracts: {e}", file=sys.stderr)
            return 5

    api_key = os.environ.get("OPENAI_API_KEY", "").strip()

    def _judge_fn(
        case_contract: dict[str, Any],
        score_target: dict[str, Any],
        model_answer: str,
        response_http_status: int | None,
    ) -> dict[str, Any]:
        return judge_case_with_retry(
            case_contract=case_contract,
            score_target=score_target,
            model_answer=model_answer,
            response_http_status=response_http_status,
            api_key=api_key,
            model=args.judge_model,
            timeout_seconds=args.judge_timeout_seconds,
            max_attempts=max(1, int(args.judge_max_attempts)),
        )

    def _provision_session_for_fixture(fixture_id: str) -> str:
        if fixture_id not in FIXTURE_SETUP:
            raise KeyError(f"unknown fixture_id for auto-setup: {fixture_id}")
        session_id = auto_setup_sessions(base_url, cookie, [fixture_id], run_id)[fixture_id]
        session_map[fixture_id] = session_id
        return session_id

    results = run_inventory_cases(
        base_url=base_url,
        cookie=cookie,
        session_for_fixture=session_map,
        cases=cases,
        provision_session_fn=_provision_session_for_fixture if auto_setup else None,
        judge_contract_by_case=judge_contracts if judge_enabled else None,
        judge_fn=_judge_fn if judge_enabled else None,
    )

    expected_scores_defaults: dict[str, Any] = {}
    expected_scores_targets: dict[str, dict[str, Any]] = {}
    try:
        expected_scores_data = load_json(args.expected_scores.resolve())
        defaults_obj = expected_scores_data.get("defaults")
        if isinstance(defaults_obj, dict):
            expected_scores_defaults = defaults_obj
        case_targets = expected_scores_data.get("case_targets")
        if isinstance(case_targets, list):
            for item in case_targets:
                if isinstance(item, dict) and isinstance(item.get("case_id"), str):
                    expected_scores_targets[item["case_id"]] = item
    except (OSError, json.JSONDecodeError):
        expected_scores_defaults = {}
        expected_scores_targets = {}

    report = aggregate_report(
        cases,
        results,
        score_defaults=expected_scores_defaults,
        score_targets_by_case=expected_scores_targets,
    )
    write_run_artifacts(
        run_dir,
        run_id=run_id,
        base_url=base_url,
        inventory_path=inventory_path,
        auto_setup=auto_setup,
        dry_run=False,
        session_map=session_map,
        cases=cases,
        results=results,
    )
    write_report_artifact(
        run_dir,
        run_id=run_id,
        inventory_path=inventory_path,
        report=report,
    )
    print(render_terminal_report(report))
    print(f"Wrote eval run to {run_dir}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
