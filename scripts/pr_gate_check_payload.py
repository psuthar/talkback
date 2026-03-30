#!/usr/bin/env python3
"""
Build GitHub Checks API payload from pr-gate-summary.json (single source of truth).

Maps final_gate.status:
  PASS -> check conclusion success,  workflow_should_fail False
  WARN -> check conclusion neutral,  workflow_should_fail False
  BLOCK / error -> check conclusion failure, workflow_should_fail True

Writes artifacts/pr-gate-check.json for actions/github-script.
"""

from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path
from typing import Any

from pr_gate import GATE_FOOTER_MARKDOWN, format_required_actions_grouped_markdown

CHECK_NAME = "TalkBack PR Gate"

VALID_STATUSES = frozenset({"PASS", "WARN", "BLOCK"})


def _norm_status(raw: Any) -> str:
    s = (str(raw) if raw is not None else "").strip().upper()
    return s if s in VALID_STATUSES else "UNKNOWN"


def conclusion_for_status(status: str) -> str:
    if status == "PASS":
        return "success"
    if status == "WARN":
        return "neutral"
    return "failure"


def build_title(status: str) -> str:
    if status in VALID_STATUSES:
        return f"{CHECK_NAME}: {status}"
    return f"{CHECK_NAME}: error"


def build_summary_block(
    gate: dict[str, Any],
    *,
    run_url: str = "",
) -> str:
    """Uses final_gate.summary from pr-gate-summary.json (already confidence-aware)."""
    base = (gate.get("summary") or "").strip()
    lines = [base] if base else []
    if run_url:
        lines.append(f"Workflow run / artifacts: {run_url}")
    return "\n\n".join(lines) if lines else ""


def build_text_detail(data: dict[str, Any], *, run_url: str = "") -> str:
    """Compact markdown for check output body; mirrors pr-gate-summary.md structure."""
    pr = data.get("pr_risk") or {}
    rr = data.get("release_readiness") or {}
    fg = data.get("final_gate") or {}
    actions = data.get("required_actions") or []

    pr_s = _norm_status(pr.get("status"))
    rr_s = _norm_status(rr.get("status"))
    fg_s = _norm_status(fg.get("status"))

    pr_label = pr.get("label") or pr_s
    rr_score = rr.get("score")
    rr_line = f"{rr_s} ({rr_score}/100)" if rr_score is not None else str(rr_s)

    lines: list[str] = [
        "### Signals",
        "",
        "| Layer | Status |",
        "|-------|--------|",
        f"| PR Risk | {pr_label} |",
        f"| Release Readiness | {rr_line} |",
        f"| **Final Gate** | **{fg_s}** |",
        "",
    ]

    if actions:
        lines.append("### Required actions before merge")
        lines.append("")
        lines.append(format_required_actions_grouped_markdown(actions))
        lines.append("")

    lines.append("### Supporting detail")
    lines.append("")
    score = pr.get("score")
    band = pr.get("band") or "unknown"
    if score is not None:
        lines.append(f"- PR Risk score: **{score}** / 100 ({band})")
    if pr.get("confidence") is not None:
        lines.append(f"- PR Risk test confidence: **{pr['confidence']}** / 100")
    if rr.get("score") is not None:
        lines.append(f"- Release Readiness score: **{rr['score']}** / 100")
    if rr.get("warnings") is not None:
        lines.append(f"- Release Readiness warnings: **{rr['warnings']}**")
    if rr.get("blockers") is not None:
        lines.append(f"- Release Readiness blockers: **{rr['blockers']}**")
    if rr.get("confidence") is not None:
        lines.append(f"- Release Readiness confidence: **{rr['confidence']}** / 100")
    if fg.get("confidence"):
        lines.append(f"- Gate confidence: **{fg['confidence']}**")

    factors = pr.get("top_risk_factors") or []
    if factors:
        lines.append("")
        lines.append("**Top risk signals:**")
        for f in factors[:6]:
            lines.append(f"- {f}")

    lines.append("")
    lines.append("### Drill-down")
    lines.append("")
    lines.append(
        "Full deterministic report: download workflow artifact **release-readiness** — "
        "files **`pr-gate-summary.md`** and **`pr-gate-summary.json`**."
    )
    if run_url:
        lines.append(f"Job summary and logs: {run_url}")
    lines.append("")
    lines.append(GATE_FOOTER_MARKDOWN)
    return "\n".join(lines)


def build_payload_from_dict(
    data: dict[str, Any],
    *,
    run_url: str = "",
) -> dict[str, Any]:
    """Build check payload from parsed pr-gate-summary.json."""
    fg = data.get("final_gate") or {}
    status = _norm_status(fg.get("status"))

    if status == "UNKNOWN":
        return build_error_payload(
            "final_gate.status missing or invalid in pr-gate-summary.json",
            raw_json=data,
            run_url=run_url,
        )

    summary = build_summary_block(fg, run_url=run_url)
    text = build_text_detail(data, run_url=run_url)
    wf_fail = status == "BLOCK"

    return {
        "check_name": CHECK_NAME,
        "check_conclusion": conclusion_for_status(status),
        "title": build_title(status),
        "summary": summary,
        "text": text,
        "workflow_should_fail": wf_fail,
        "details_url": run_url.strip() or None,
        "final_gate_status": status,
    }


def build_error_payload(
    message: str,
    *,
    raw_json: Any = None,
    run_url: str = "",
) -> dict[str, Any]:
    """When gate JSON is missing or invalid; check always fails."""
    detail = message
    if raw_json is not None and not isinstance(raw_json, dict):
        detail += f"\n\nUnexpected type: {type(raw_json).__name__}"
    snippet = ""
    if isinstance(raw_json, dict):
        try:
            snippet = json.dumps(raw_json, indent=2)[:6000]
        except Exception:
            snippet = repr(raw_json)[:6000]
    text = "### Error\n\n" + detail
    if snippet:
        text += "\n\n```json\n" + snippet + "\n```"
    if run_url:
        text += f"\n\nWorkflow run: {run_url}"
    return {
        "check_name": CHECK_NAME,
        "check_conclusion": "failure",
        "title": build_title("UNKNOWN"),
        "summary": f"**TalkBack PR Gate could not be computed.** {message}",
        "text": text,
        "workflow_should_fail": True,
        "details_url": run_url.strip() or None,
        "final_gate_status": "ERROR",
    }


def main(argv: list[str] | None = None) -> int:
    import os

    p = argparse.ArgumentParser(description="Build GitHub Check payload for TalkBack PR Gate.")
    p.add_argument("--gate-json", type=Path, default=Path("artifacts/pr-gate-summary.json"))
    p.add_argument("--output", type=Path, default=Path("artifacts/pr-gate-check.json"))
    p.add_argument(
        "--run-url",
        default="",
        help="Workflow run URL (GITHUB_RUN_URL) for summary and details_url",
    )
    args = p.parse_args(argv)

    run_url = (args.run_url or "").strip() or os.environ.get("GITHUB_RUN_URL", "").strip()

    path = args.gate_json
    if not path.is_file():
        payload = build_error_payload(f"File not found: {path}", run_url=run_url)
    else:
        try:
            data = json.loads(path.read_text(encoding="utf-8"))
        except json.JSONDecodeError as exc:
            payload = build_error_payload(f"Invalid JSON in {path}: {exc}", run_url=run_url)
        else:
            if not isinstance(data, dict):
                payload = build_error_payload(
                    f"Expected JSON object in {path}", raw_json=data, run_url=run_url
                )
            else:
                payload = build_payload_from_dict(data, run_url=run_url)

    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(payload, indent=2), encoding="utf-8")
    print(payload["title"], "->", payload["check_conclusion"])
    return 0


if __name__ == "__main__":
    sys.exit(main())
