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
            patch_session(base_url, cookie, session_id, patch)
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


def ordered_fixture_ids(cases: list[dict[str, Any]]) -> list[str]:
    seen: OrderedDict[str, None] = OrderedDict()
    for c in cases:
        fid = str(c.get("fixture_id", ""))
        if fid and fid not in seen:
            seen[fid] = None
    return list(seen.keys())


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

    results = run_inventory_cases(
        base_url=base_url,
        cookie=cookie,
        session_for_fixture=session_map,
        cases=cases,
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
    print(f"Wrote eval run to {run_dir}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
