#!/usr/bin/env python3
"""
pr_gate.py — Unified PR Gate combiner (TalkBack v1).

Reads pr-risk.json and release-readiness.json, applies a deterministic
combining rule, and writes:
  - pr-gate-summary.json   (machine-readable, stable schema)
  - pr-gate-summary.md     (human-readable)

Combining rule (highest severity wins):
  BLOCK  if either input is BLOCK
  WARN   if either input is WARN (and neither is BLOCK)
  PASS   otherwise

Exit codes:
  0 — gate computed successfully (PASS, WARN, and BLOCK are all valid outcomes)
  1 — one or both inputs could not be parsed; gate forced to BLOCK
"""
from __future__ import annotations

import argparse
import json
import os
import sys
from dataclasses import dataclass, field
from pathlib import Path
from typing import Optional

VERSION = "v1"

REC_DISPLAY: dict[str, str] = {
    "PASS": "PASS (low risk)",
    "WARN": "WARN",
    "BLOCK": "BLOCK",
}
STATUS_EMOJI: dict[str, str] = {
    "PASS": "🟢",
    "WARN": "🟡",
    "BLOCK": "🔴",
}

# Always-present standard merge prerequisites — placed first, before per-signal items.
STANDARD_ACTIONS: list[str] = [
    "CI checks must pass before merge",
    "At least one approving review required",
]

GATE_SUMMARIES: dict[str, str] = {
    "PASS": (
        "Low-risk change and readiness checks passed. "
        "Normal merge prerequisites still apply before merge."
    ),
    "WARN": (
        "Elevated attention needed. "
        "Review all warnings and required actions before merging."
    ),
    "BLOCK": (
        "One or more hard blockers detected. "
        "Do not merge until all blockers are resolved."
    ),
}


# ---------------------------------------------------------------------------
# Data types
# ---------------------------------------------------------------------------


@dataclass
class PRRiskInput:
    status: str            # PASS | WARN | BLOCK
    score: float
    band: str
    label: str             # e.g. "PASS (low risk)"
    required_validations: list[str] = field(default_factory=list)
    top_risk_factors: list[str] = field(default_factory=list)


@dataclass
class ReadinessInput:
    status: str            # PASS | WARN | BLOCK
    score: float
    warnings_count: int
    blockers_count: int
    blocker_messages: list[str] = field(default_factory=list)
    warning_messages: list[str] = field(default_factory=list)
    recommended_actions: list[str] = field(default_factory=list)
    # True when report.json was successfully loaded and contributed blocker/warning strings.
    # False means required_actions may be incomplete (counts only, no strings).
    report_enriched: bool = False


# ---------------------------------------------------------------------------
# Core logic
# ---------------------------------------------------------------------------


def normalize_status(s: str) -> str:
    """Return PASS | WARN | BLOCK; raise ValueError for anything else."""
    s = (s or "").strip().upper()
    if s not in ("PASS", "WARN", "BLOCK"):
        raise ValueError(f"Unknown gate status: {s!r}")
    return s


def compute_gate_status(rr_status: str, risk_status: str) -> str:
    """Deterministic: BLOCK beats WARN beats PASS."""
    if rr_status == "BLOCK" or risk_status == "BLOCK":
        return "BLOCK"
    if rr_status == "WARN" or risk_status == "WARN":
        return "WARN"
    return "PASS"


def _action_key(s: str) -> str:
    """Normalise for deduplication: strip, remove trailing period, lowercase."""
    return s.strip().rstrip(".").lower()


def build_required_actions(risk: PRRiskInput, rr: ReadinessInput) -> list[str]:
    """
    Deduplicated, ordered required-actions list.
    Order: standard always-present → PR risk validations → RR blockers → RR warnings → RR recommended.
    """
    seen: set[str] = set()
    result: list[str] = []

    def add(item: str) -> None:
        key = _action_key(item)
        if key and key not in seen:
            seen.add(key)
            result.append(item.strip())

    for a in STANDARD_ACTIONS:
        add(a)
    for v in risk.required_validations:
        add(v)
    for b in rr.blocker_messages:
        add(b)
    for w in rr.warning_messages:
        add(w)
    for r in rr.recommended_actions:
        add(r)

    return result


# ---------------------------------------------------------------------------
# Loaders
# ---------------------------------------------------------------------------


def load_pr_risk(path: Path) -> PRRiskInput:
    """Load and validate artifacts/pr-risk.json."""
    data = json.loads(path.read_text(encoding="utf-8"))
    rec = normalize_status(data.get("merge_recommendation", ""))
    return PRRiskInput(
        status=rec,
        score=float(data.get("score") or 0),
        band=str(data.get("band") or "unknown"),
        label=REC_DISPLAY.get(rec, rec),
        required_validations=[str(v) for v in (data.get("required_validations") or [])],
        top_risk_factors=[str(f) for f in (data.get("top_risk_factors") or [])],
    )


def load_release_readiness(
    summary_path: Path,
    report_path: Optional[Path] = None,
) -> ReadinessInput:
    """
    Load artifacts/release-readiness.json.
    Optionally enriches with blocker/warning strings from artifacts/release-readiness/report.json.
    """
    summary = json.loads(summary_path.read_text(encoding="utf-8"))
    status = normalize_status(summary.get("outcome", ""))

    blocker_msgs: list[str] = []
    warning_msgs: list[str] = []
    recommended: list[str] = []
    report_enriched = False

    if report_path and report_path.exists():
        try:
            report = json.loads(report_path.read_text(encoding="utf-8"))
            blocker_msgs = [str(b) for b in (report.get("blockers") or [])]
            warning_msgs = [str(w) for w in (report.get("warnings") or [])]
            recommended = [str(r) for r in (report.get("recommended_actions") or [])]
            report_enriched = True
        except Exception as exc:
            print(
                f"Warning: report.json at {report_path} could not be read ({exc}) — "
                "required actions may be incomplete",
                file=sys.stderr,
            )
    else:
        print(
            f"Warning: report.json not available at {report_path} — "
            "required actions sourced from summary counts only",
            file=sys.stderr,
        )

    return ReadinessInput(
        status=status,
        score=float(summary.get("score") or 0),
        warnings_count=int(summary.get("warnings") or 0),
        blockers_count=int(summary.get("blockers") or 0),
        blocker_messages=blocker_msgs,
        warning_messages=warning_msgs,
        recommended_actions=recommended,
        report_enriched=report_enriched,
    )


# ---------------------------------------------------------------------------
# Output builders
# ---------------------------------------------------------------------------


def build_gate_json(
    risk: PRRiskInput,
    rr: ReadinessInput,
    gate_status: str,
    required_actions: list[str],
) -> dict:
    return {
        "version": VERSION,
        "pr_risk": {
            "status": risk.status,
            "label": risk.label,
            "score": round(risk.score, 1),
            "top_risk_factors": risk.top_risk_factors,
        },
        "release_readiness": {
            "status": rr.status,
            "score": round(rr.score, 1),
            "warnings": rr.warnings_count,
            "blockers": rr.blockers_count,
        },
        "final_gate": {
            "status": gate_status,
            "summary": GATE_SUMMARIES[gate_status],
        },
        "required_actions": required_actions,
        # True when release-readiness/report.json was read and contributed blocker/warning strings.
        # False means required_actions contains only standard items + PR risk validations.
        "report_enriched": rr.report_enriched,
    }


def build_gate_markdown(
    risk: PRRiskInput,
    rr: ReadinessInput,
    gate_status: str,
    required_actions: list[str],
) -> str:
    ge = STATUS_EMOJI.get(gate_status, "⚪")
    re = STATUS_EMOJI.get(risk.status, "⚪")
    rre = STATUS_EMOJI.get(rr.status, "⚪")

    rr_label = f"{rr.status} ({rr.score:.0f}/100)"
    if rr.warnings_count or rr.blockers_count:
        rr_label += f" · {rr.warnings_count} warn · {rr.blockers_count} block"

    lines: list[str] = [
        "# TalkBack PR Gate Summary",
        "",
        "| Signal | Result |",
        "|--------|--------|",
        f"| PR Risk | {re} {risk.label} |",
        f"| Release Readiness | {rre} {rr_label} |",
        f"| **Final Gate** | **{ge} {gate_status}** |",
        "",
        "## Decision",
        "",
        GATE_SUMMARIES[gate_status],
        "",
        "## Required actions before merge",
        "",
    ]

    if required_actions:
        for a in required_actions:
            lines.append(f"- {a}")
    else:
        lines.append("_None beyond standard CI and review requirements._")

    lines += [
        "",
        "## Supporting detail",
        "",
        f"- PR Risk score: {risk.score:.1f} / 100 ({risk.band})",
        f"- Release Readiness score: {rr.score:.1f} / 100",
        f"- Release Readiness warnings: {rr.warnings_count}",
        f"- Release Readiness blockers: {rr.blockers_count}",
        "",
        "---",
        "_This gate is deterministic. PASS does not bypass branch protection or required code review._",
        "",
    ]

    return "\n".join(lines)


def _partial_gate_json(
    risk: Optional[PRRiskInput],
    rr: Optional[ReadinessInput],
    errors: list[str],
) -> dict:
    """Minimal gate JSON emitted when one or both inputs fail to load."""
    return {
        "version": VERSION,
        "pr_risk": (
            {"status": risk.status, "label": risk.label, "score": round(risk.score, 1),
             "top_risk_factors": risk.top_risk_factors}
            if risk else {"status": "UNKNOWN", "error": "Failed to parse PR risk input"}
        ),
        "release_readiness": (
            {"status": rr.status, "score": round(rr.score, 1),
             "warnings": rr.warnings_count, "blockers": rr.blockers_count}
            if rr else {"status": "UNKNOWN", "error": "Failed to parse release readiness input"}
        ),
        "final_gate": {
            "status": "BLOCK",
            "summary": "Gate inputs could not be parsed — treated as BLOCK. " + " | ".join(errors),
        },
        "required_actions": list(STANDARD_ACTIONS),
        "report_enriched": False,
    }


def _partial_gate_md(errors: list[str]) -> str:
    lines = [
        "# TalkBack PR Gate Summary",
        "",
        "**🔴 BLOCK — Gate inputs could not be parsed.**",
        "",
        "The following errors occurred while loading gate inputs:",
        "",
    ]
    for e in errors:
        lines.append(f"- {e}")
    lines += [
        "",
        "_Ensure `artifacts/pr-risk.json` and `artifacts/release-readiness.json` were generated._",
        "",
    ]
    return "\n".join(lines)


def _append_step_summary(gate_json: dict) -> None:
    """Append a compact gate section to GITHUB_STEP_SUMMARY (no-op outside GitHub Actions)."""
    summary_path = os.environ.get("GITHUB_STEP_SUMMARY")
    if not summary_path:
        return
    gate = gate_json.get("final_gate", {})
    status = gate.get("status", "UNKNOWN")
    emoji = STATUS_EMOJI.get(status, "⚪")
    risk = gate_json.get("pr_risk", {})
    rr = gate_json.get("release_readiness", {})
    rr_emoji = STATUS_EMOJI.get(str(rr.get("status", "")), "⚪")

    lines = [
        "## PR Gate",
        "",
        f"{emoji} **Final gate: {status}** &nbsp;·&nbsp; {gate.get('summary', '')}",
        "",
        "| PR Risk | Release Readiness |",
        "|---------|-------------------|",
        f"| {risk.get('label', '?')} | {rr_emoji} {rr.get('status', '?')} ({rr.get('score', '?')}/100) |",
        "",
    ]
    with open(summary_path, "a", encoding="utf-8") as f:
        f.write("\n".join(lines) + "\n")


# ---------------------------------------------------------------------------
# Main entry point
# ---------------------------------------------------------------------------


def run(
    pr_risk_path: Path,
    readiness_summary_path: Path,
    readiness_report_path: Optional[Path],
    output_dir: Path,
    step_summary: bool = False,
) -> tuple[dict, int]:
    """
    Compute gate, write output files.
    Returns (gate_json, exit_code): 0 = inputs parsed OK, 1 = parse failure.
    Output files are always written (partial on failure).
    """
    errors: list[str] = []
    risk: Optional[PRRiskInput] = None
    rr: Optional[ReadinessInput] = None

    try:
        risk = load_pr_risk(pr_risk_path)
    except Exception as exc:
        errors.append(f"PR risk ({pr_risk_path.name}): {exc}")

    try:
        rr = load_release_readiness(readiness_summary_path, readiness_report_path)
    except Exception as exc:
        errors.append(f"Release readiness ({readiness_summary_path.name}): {exc}")

    output_dir.mkdir(parents=True, exist_ok=True)

    if errors:
        gate_json = _partial_gate_json(risk, rr, errors)
        gate_md = _partial_gate_md(errors)
        (output_dir / "pr-gate-summary.json").write_text(
            json.dumps(gate_json, indent=2), encoding="utf-8"
        )
        (output_dir / "pr-gate-summary.md").write_text(gate_md, encoding="utf-8")
        if step_summary:
            _append_step_summary(gate_json)
        return gate_json, 1

    assert risk is not None and rr is not None

    gate_status = compute_gate_status(rr.status, risk.status)
    required_actions = build_required_actions(risk, rr)
    gate_json = build_gate_json(risk, rr, gate_status, required_actions)
    gate_md = build_gate_markdown(risk, rr, gate_status, required_actions)

    (output_dir / "pr-gate-summary.json").write_text(
        json.dumps(gate_json, indent=2), encoding="utf-8"
    )
    (output_dir / "pr-gate-summary.md").write_text(gate_md, encoding="utf-8")

    if step_summary:
        _append_step_summary(gate_json)

    return gate_json, 0


def main(argv: Optional[list[str]] = None) -> int:
    parser = argparse.ArgumentParser(description="Compute unified PR gate summary.")
    parser.add_argument(
        "--pr-risk-json", default="artifacts/pr-risk.json",
        help="Path to artifacts/pr-risk.json",
    )
    parser.add_argument(
        "--readiness-json", default="artifacts/release-readiness.json",
        help="Path to artifacts/release-readiness.json",
    )
    parser.add_argument(
        "--readiness-report-json",
        default="artifacts/release-readiness/report.json",
        help="Path to full release readiness report JSON (optional; enriches required-actions)",
    )
    parser.add_argument(
        "--output-dir", default="artifacts",
        help="Directory for output files (pr-gate-summary.json / .md)",
    )
    parser.add_argument(
        "--step-summary", action="store_true",
        help="Append compact gate section to GITHUB_STEP_SUMMARY",
    )
    args = parser.parse_args(argv)

    gate_json, exit_code = run(
        pr_risk_path=Path(args.pr_risk_json),
        readiness_summary_path=Path(args.readiness_json),
        readiness_report_path=Path(args.readiness_report_json),
        output_dir=Path(args.output_dir),
        step_summary=args.step_summary,
    )

    status = gate_json.get("final_gate", {}).get("status", "UNKNOWN")
    print(f"{STATUS_EMOJI.get(status, '⚪')} PR Gate: {status}")
    return exit_code


if __name__ == "__main__":
    sys.exit(main())
