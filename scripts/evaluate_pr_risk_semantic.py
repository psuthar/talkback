#!/usr/bin/env python3
"""
Map PR Risk machine output (artifacts/pr-risk.json) to GitHub semantic conclusions
and optional workflow exit codes for the Release Readiness workflow.

PASS  -> check success,  workflow exit 0
WARN  -> check neutral, workflow exit 0
BLOCK -> check failure, workflow exit 1
Generator failure / missing or invalid JSON -> check failure, workflow exit 1
"""

from __future__ import annotations

import argparse
import json
import os
import sys
from pathlib import Path
from typing import Any

VALID_REC = frozenset({"PASS", "WARN", "BLOCK"})
REC_EMOJI = {"PASS": "🟢", "WARN": "🟡", "BLOCK": "🔴"}
# Display labels: PASS is annotated to make clear it is a risk assessment, not merge approval.
REC_DISPLAY = {"PASS": "PASS (low risk)", "WARN": "WARN", "BLOCK": "BLOCK"}


def normalize_rec(raw: Any) -> str | None:
    if raw is None:
        return None
    s = str(raw).strip().upper()
    return s if s in VALID_REC else None


def build_semantic_record(
    *,
    generator_outcome: str,
    pr_risk_path: Path,
    pr_risk_raw: dict[str, Any] | None,
) -> dict[str, Any]:
    """Build the check-run payload and workflow flags."""
    artifact_name = "release-readiness"
    run_url = (os.environ.get("GITHUB_RUN_URL") or "").strip()
    artifact_hint = (
        f"Download the `{artifact_name}` workflow artifact for full `pr_risk.md` "
        "and `artifacts/pr-risk.json`."
    )
    if run_url:
        artifact_hint = f"{artifact_hint} Run: {run_url}"

    def fail(reason: str, detail: str) -> dict[str, Any]:
        title = "PR Risk: error"
        summary = f"{reason}. {artifact_hint}"
        return {
            "check_conclusion": "failure",
            "semantic_conclusion": "failure",
            "title": title,
            "summary": summary,
            "text": detail,
            "workflow_should_fail": True,
            "score": None,
            "band": None,
            "merge_recommendation": None,
            "top_risk_factors": [],
        }

    gen = (generator_outcome or "").strip().lower()
    if gen != "success":
        return fail(
            "PR Risk generator did not complete successfully",
            f"Step outcome was {generator_outcome!r}.",
        )

    if pr_risk_raw is None:
        return fail(
            f"Missing or unreadable {pr_risk_path}",
            "Expected JSON from `go run ./cmd/prrisk`.",
        )

    rec = normalize_rec(pr_risk_raw.get("merge_recommendation"))
    if rec is None:
        return fail(
            "Invalid or missing merge_recommendation",
            json.dumps(pr_risk_raw, indent=2)[:8000],
        )

    score = pr_risk_raw.get("score")
    band = pr_risk_raw.get("band")
    factors = pr_risk_raw.get("top_risk_factors") or []

    emoji = REC_EMOJI.get(rec, "⚪")
    display = REC_DISPLAY.get(rec, rec)
    title = f"PR Risk: {emoji} {display}"
    lines = [
        f"**Score:** {score}  ",
        f"**Band:** {band}  ",
        f"**PR risk assessment:** {emoji} {display}  ",
        "",
        "_This is a PR-risk signal only. CI checks, code review, and required testing still apply regardless of this assessment._  ",
        "",
        artifact_hint,
    ]
    if factors:
        lines.append("")
        lines.append("**Top risk factors:**")
        for f in factors[:8]:
            lines.append(f"- {f}")
    summary = "\n".join(lines)

    req_val = pr_risk_raw.get("required_validations") or []
    text_lines = [
        f"- Score: {score}",
        f"- Band: {band}",
        f"- PR risk assessment: {display}",
    ]
    if req_val:
        text_lines.append("- Required validations:")
        for v in req_val:
            text_lines.append(f"  - {v}")
    else:
        text_lines.append("- Required validations: none")
    if factors:
        text_lines.append("- Top risk factors:")
        for f in factors[:8]:
            text_lines.append(f"  - {f}")
    text = "\n".join(text_lines)

    if rec == "PASS":
        conclusion = "success"
        wf_fail = False
    elif rec == "WARN":
        conclusion = "neutral"
        wf_fail = False
    else:  # BLOCK
        conclusion = "failure"
        wf_fail = True

    return {
        "check_conclusion": conclusion,
        "semantic_conclusion": conclusion,
        "title": title,
        "summary": summary,
        "text": text,
        "workflow_should_fail": wf_fail,
        "score": score,
        "band": band,
        "merge_recommendation": rec,
        "top_risk_factors": factors,
    }


def write_github_output(path: Path, data: dict[str, Any]) -> None:
    """Append KEY=value and multiline fields to GITHUB_OUTPUT."""
    esc = data["title"]
    summ = data["summary"]
    conc = data["semantic_conclusion"]
    with path.open("a", encoding="utf-8") as f:
        f.write(f"semantic_conclusion={conc}\n")
        f.write(f"semantic_title={esc}\n")
        f.write("semantic_summary<<PRRISK_EOF\n")
        f.write(summ)
        f.write("\nPRRISK_EOF\n")


def append_step_summary(path: Path | None, data: dict[str, Any]) -> None:
    if not path:
        return
    p = Path(path)
    rec = data.get("merge_recommendation")
    conc = data.get("semantic_conclusion")
    emoji = REC_EMOJI.get(rec, "⚪") if rec else "⚪"
    run_url = (os.environ.get("GITHUB_RUN_URL") or "").strip()
    display = REC_DISPLAY.get(rec, rec) if rec else rec
    lines = [
        "## PR Risk",
        "",
        f"{emoji} **PR risk assessment: {display}** — Score: {data.get('score')} ({data.get('band')})",
        f"GitHub check: `{conc}` (PR Risk / semantic-result)",
        "",
        "_PR-risk signal only — CI, review, and testing still required before merging._",
        "",
    ]
    if run_url:
        lines.append(f"Artifacts: {run_url}")
        lines.append("")
    factors = data.get("top_risk_factors") or []
    if factors:
        lines.append("**Top risk factors:**")
        for x in factors[:8]:
            lines.append(f"- {x}")
        lines.append("")
    p.parent.mkdir(parents=True, exist_ok=True)
    with p.open("a", encoding="utf-8") as f:
        f.write("\n".join(lines) + "\n")


def load_pr_risk(path: Path) -> dict[str, Any] | None:
    if not path.is_file():
        return None
    try:
        raw = path.read_text(encoding="utf-8")
        return json.loads(raw)
    except (json.JSONDecodeError, OSError):
        return None


def main() -> int:
    ap = argparse.ArgumentParser(description="Evaluate PR Risk semantic mapping for CI.")
    ap.add_argument("--pr-risk-json", type=Path, default=Path("artifacts/pr-risk.json"))
    ap.add_argument("--generator-outcome", default=os.environ.get("PRRISK_GEN_OUTCOME", ""))
    ap.add_argument("--github-output", type=Path, default=None)
    ap.add_argument("--semantic-json-out", type=Path, default=Path("artifacts/pr-risk-semantic.json"))
    ap.add_argument("--step-summary-file", default=os.environ.get("GITHUB_STEP_SUMMARY"))
    args = ap.parse_args()

    gh_out = args.github_output
    if gh_out is None:
        p = os.environ.get("GITHUB_OUTPUT")
        gh_out = Path(p) if p else None

    raw = load_pr_risk(args.pr_risk_json)
    record = build_semantic_record(
        generator_outcome=args.generator_outcome,
        pr_risk_path=args.pr_risk_json,
        pr_risk_raw=raw,
    )

    args.semantic_json_out.parent.mkdir(parents=True, exist_ok=True)
    args.semantic_json_out.write_text(json.dumps(record, indent=2), encoding="utf-8")

    if gh_out is not None:
        write_github_output(gh_out, record)

    append_step_summary(Path(args.step_summary_file) if args.step_summary_file else None, record)

    return 1 if record["workflow_should_fail"] else 0


if __name__ == "__main__":
    sys.exit(main())
