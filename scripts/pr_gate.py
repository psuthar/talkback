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
import re
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
    "CI checks must pass",
    "At least one approving review is required",
]

# Merged workflow/config validation (one line replaces overlapping sources).
MERGED_WORKFLOW_CONFIG_ACTION: str = (
    "Add a validation note and validate workflow/config changes against required checks"
)

# Single footer semantics for markdown, step summary, and GitHub Check text.
GATE_FOOTER_MARKDOWN: str = (
    "_This gate is deterministic. "
    "**PASS** does not bypass branch protection or required code review. "
    "**WARN** requires completing validations and review before merging. "
    "**BLOCK** means do not merge until all blockers are resolved._"
)


def gate_decision_summary(gate_status: str, gate_confidence: str) -> str:
    """
    Confidence-aware decision text (deterministic). gate_confidence: high | moderate | low.
    Mirrors policy thresholds from classify_gate_confidence without re-deriving numerics here.
    """
    if gate_status == "PASS":
        if gate_confidence == "high":
            return (
                "Low-risk change and readiness checks passed with strong supporting confidence. "
                "Normal merge prerequisites still apply before merge."
            )
        if gate_confidence == "moderate":
            return (
                "Low-risk change and readiness checks passed with moderate supporting confidence. "
                "Normal merge prerequisites still apply before merge."
            )
        return (
            "Low-risk change and readiness checks passed, but supporting confidence is limited. "
            "Normal merge prerequisites still apply before merge."
        )
    if gate_status == "WARN":
        if gate_confidence == "high":
            return (
                "Not blocked, but elevated attention is required due to warnings. "
                "Complete the required validations and review before merging."
            )
        if gate_confidence == "moderate":
            return (
                "Not blocked, but elevated attention is required due to warnings "
                "and only moderate supporting confidence. "
                "Complete the required validations before merging."
            )
        return (
            "Not blocked, but elevated attention is required due to warnings and low supporting confidence. "
            "Complete the required validations and review before merging."
        )
    return (
        "One or more hard blockers detected. "
        "Do not merge until all blockers are resolved."
    )


# Backward-compat for tests and simple lookups (non–confidence-aware PASS/WARN).
GATE_SUMMARIES: dict[str, str] = {
    "PASS": gate_decision_summary("PASS", "high"),
    "WARN": gate_decision_summary("WARN", "moderate"),
    "BLOCK": gate_decision_summary("BLOCK", "low"),
}


# ---------------------------------------------------------------------------
# Action normalization
# ---------------------------------------------------------------------------

# Items matching any of these patterns are meta/non-actionable and must not
# appear in the user-facing required-actions list.
_META_DROPS: list[re.Pattern] = [
    re.compile(r"^pr\s+risk\s*:?\s*(pass|warn|block)", re.IGNORECASE),
    re.compile(r"\bsee\s+pr_risk\.md\b", re.IGNORECASE),
    re.compile(r"\bfor\s+required\s+actions\b", re.IGNORECASE),
    re.compile(r"^review\s+warnings\s+before\s+deploy\.?$", re.IGNORECASE),
]

# Patterns that map to a specific canonical human action (first match wins).
_REWRITE_PATTERNS: list[tuple[re.Pattern, str]] = [
    # Any CI "checks/status must pass" phrasing, with or without "ci:" prefix.
    (
        re.compile(r"(^ci\s*:|ci\s+(checks|required\s+status)\b.*pass)", re.IGNORECASE),
        "CI checks must pass",
    ),
    # Release-readiness warning: risky config/workflow path changed without a note.
    (
        re.compile(r"risky\s+(config|workflow).*changed\s+without\s+validation", re.IGNORECASE),
        MERGED_WORKFLOW_CONFIG_ACTION,
    ),
    # config: prefix specifically about workflow / deploy / go.mod
    (
        re.compile(r"^config:\s*(workflow|deploy|go\.mod)", re.IGNORECASE),
        MERGED_WORKFLOW_CONFIG_ACTION,
    ),
]

# Taxonomy prefixes with explicit handling; the remainder is capitalized and used as-is.
_STRIP_PREFIXES: tuple[str, ...] = ("config:", "test:", "process:")

# Generic catch-all: matches any single lowercase word followed by ": " (e.g. "security: ...")
# so new taxonomy labels from the Go engine are stripped automatically without code changes.
# URL schemes (https://, http://) are safe because they have "//" not " " after the colon.
_GENERIC_PREFIX_RE: re.Pattern = re.compile(r"^([a-z]+):\s+(.+)", re.DOTALL)


def normalize_action(raw: str) -> str | None:
    """
    Translate a raw action string into clean human-readable form.

    Returns None when the item is meta/non-actionable (e.g. "PR Risk: WARN —
    see pr_risk.md") and should be excluded from user-facing output.

    Strips internal taxonomy prefixes (ci:, config:, test:, process:, and any
    future single-word lowercase prefix) and maps known patterns to canonical
    wording so that semantically equivalent items from different sources
    deduplicate cleanly.
    """
    s = raw.strip()
    if not s:
        return None

    # Drop meta / non-actionable items first.
    for pat in _META_DROPS:
        if pat.search(s):
            return None

    # Apply specific rewrites (first match wins).
    for pat, replacement in _REWRITE_PATTERNS:
        if pat.search(s):
            return _finalize_action_display(replacement)

    # Strip known taxonomy prefixes and capitalize the remainder.
    sl = s.lower()
    for prefix in _STRIP_PREFIXES:
        if sl.startswith(prefix):
            rest = s[len(prefix):].strip()
            out = (rest[0].upper() + rest[1:]) if rest else None
            return _finalize_action_display(out) if out else None

    # Generic catch-all: strip any unknown single-word lowercase prefix (e.g. "security:").
    # Ensures new labels from the Go engine never leak into user-facing output.
    m = _GENERIC_PREFIX_RE.match(s)
    if m:
        rest = m.group(2).strip()
        out = (rest[0].upper() + rest[1:]) if rest else None
        return _finalize_action_display(out) if out else None

    return _finalize_action_display(s)


# ---------------------------------------------------------------------------
# Data types
# ---------------------------------------------------------------------------


@dataclass
class PRRiskInput:
    status: str            # PASS | WARN | BLOCK
    score: float
    band: str
    label: str             # e.g. "PASS (low risk)"
    # Test confidence score (0–100) from the test_confidence category in pr-risk.json.
    # None when the upstream engine did not emit categories (legacy results).
    confidence: Optional[int] = None
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
    # SCRUM-197: failing E2E spec titles, threaded through from
    # release_readiness_engine.ReadinessResult so consumers don't need to
    # re-parse playwright-results.json to learn which spec failed.
    critical_failed_titles: list[str] = field(default_factory=list)
    non_critical_failed_titles: list[str] = field(default_factory=list)
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


def derive_rr_confidence(rr: ReadinessInput) -> int:
    """
    Derive a 0–100 confidence score for the release-readiness signal.

    The RR confidence reflects how certain we are that the readiness check is
    a reliable signal — not just whether the score is high.  A high score with
    blockers is contradictory, so blockers cap confidence at 50.  Warnings
    already drive the score down; no extra penalty is applied here.
    """
    base = min(95, int(rr.score))
    if rr.blockers_count > 0:
        base = min(base, 50)
    return base


def classify_gate_confidence(
    risk_conf: Optional[int],
    rr_conf: int,
    gate_status: str,
) -> str:
    """
    Classify combined gate confidence as 'high', 'moderate', or 'low'.

    BLOCK always returns 'low' — something is definitely not ready.
    Otherwise uses the average of risk and RR confidence:
      ≥ 80 → high, ≥ 60 → moderate, < 60 → low.
    When risk confidence is unknown (None), it is treated as 50 (neutral).
    """
    if gate_status == "BLOCK":
        return "low"
    rc = risk_conf if risk_conf is not None else 50
    combined = (rc + rr_conf) // 2
    if combined >= 80:
        return "high"
    if combined >= 60:
        return "moderate"
    return "low"


def _action_key(s: str) -> str:
    """Normalise for deduplication: strip, remove trailing period, lowercase."""
    return s.strip().rstrip(".").lower()


# Canonical phrase polish after taxonomy strip (keys = _action_key of interim text).
_PHRASE_POLISH_BY_KEY: dict[str, str] = {
    _action_key("Tests co-located or explicitly linked for changed code"): (
        "Ensure test coverage is present or clearly linked for changed code"
    ),
    _action_key("Targeted regression for high-churn area touched by diff"): (
        "Run targeted regression for path prefixes with sustained recent commit activity"
    ),
    _action_key(
        "Targeted regression for path prefixes with several recent commits overlapping this diff"
    ): (
        "Run targeted regression for path prefixes with sustained recent commit activity"
    ),
    _action_key("Pr description with scoped, evidence-backed review plan"): (
        "Ensure PR includes a clear, scoped review plan"
    ),
}


def _finalize_action_display(s: str) -> str:
    if not s:
        return s
    return _PHRASE_POLISH_BY_KEY.get(_action_key(s), s)


# Inserted right after STANDARD_ACTIONS when any raw source normalizes to this key.
PRIORITY_ACTION_KEYS: frozenset[str] = frozenset({
    _action_key(MERGED_WORKFLOW_CONFIG_ACTION),
})


def bucket_action_priority(action: str) -> str:
    """
    Deterministic bucket for grouped display: high → medium → supporting.
    Unlisted actions default to medium so they stay visible as substantive work.
    """
    k = _action_key(action)
    high_keys = {
        _action_key("CI checks must pass"),
        _action_key(MERGED_WORKFLOW_CONFIG_ACTION),
    }
    medium_keys = {
        _action_key("Run targeted regression for high-churn areas"),
        _action_key(
            "Run targeted regression for path prefixes with sustained recent commit activity"
        ),
        _action_key("Ensure test coverage is present or clearly linked for changed code"),
    }
    supporting_keys = {
        _action_key("Ensure PR includes a clear, scoped review plan"),
        _action_key("At least one approving review is required"),
    }
    if k in high_keys:
        return "high"
    if k in medium_keys:
        return "medium"
    if k in supporting_keys:
        return "supporting"
    return "medium"


def group_required_actions_for_gate(actions: list[str]) -> dict[str, list[str]]:
    out: dict[str, list[str]] = {"high": [], "medium": [], "supporting": []}
    for a in actions:
        out[bucket_action_priority(a)].append(a)
    return out


def format_required_actions_grouped_markdown(actions: list[str]) -> str:
    """PR Gate markdown / check output — priority sections, omit empty buckets."""
    g = group_required_actions_for_gate(actions)
    lines: list[str] = []
    for key, heading in (
        ("high", "### High priority"),
        ("medium", "### Medium priority"),
        ("supporting", "### Supporting"),
    ):
        items = g.get(key, [])
        if not items:
            continue
        lines.append(heading)
        lines.append("")
        for item in items:
            lines.append(f"- {item}")
        lines.append("")
    if not lines:
        return "_None beyond standard CI and review requirements._"
    return "\n".join(lines).rstrip()


def build_required_actions(risk: PRRiskInput, rr: ReadinessInput) -> list[str]:
    """
    Deduplicated, ordered required-actions list.

    Order:
      1. Standard always-present items (CI, approving review)
      2. Priority items — elevated from any source (e.g. validation-note for config changes)
      3. PR risk validations
      4. RR blockers
      5. RR warnings
      6. RR recommended actions

    Each item is passed through normalize_action() before deduplication:
    - meta/non-actionable items are dropped
    - internal taxonomy prefixes (ci:, config:, test:, process:, any future prefix) are stripped
    - semantically equivalent items from different sources collapse to one
    """
    seen: set[str] = set()
    result: list[str] = []

    def add(item: str) -> None:
        normalized = normalize_action(item)
        if normalized is None:
            return
        key = _action_key(normalized)
        if key and key not in seen:
            seen.add(key)
            result.append(normalized)

    # 1. Standard always-present items.
    for a in STANDARD_ACTIONS:
        add(a)

    # 2. Priority items from any signal source, inserted right after standard items.
    all_signal_items = (
        list(risk.required_validations)
        + list(rr.blocker_messages)
        + list(rr.warning_messages)
        + list(rr.recommended_actions)
    )
    for item in all_signal_items:
        normalized = normalize_action(item)
        if normalized and _action_key(normalized) in PRIORITY_ACTION_KEYS:
            add(item)

    # 3–6. Per-signal ordering.
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
    raw_conf = data.get("test_confidence")
    confidence = int(raw_conf) if raw_conf is not None else None
    return PRRiskInput(
        status=rec,
        score=float(data.get("score") or 0),
        band=str(data.get("band") or "unknown"),
        label=REC_DISPLAY.get(rec, rec),
        confidence=confidence,
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

    critical_failed_titles: list[str] = []
    non_critical_failed_titles: list[str] = []

    if report_path and report_path.exists():
        try:
            report = json.loads(report_path.read_text(encoding="utf-8"))
            blocker_msgs = [str(b) for b in (report.get("blockers") or [])]
            warning_msgs = [str(w) for w in (report.get("warnings") or [])]
            recommended = [str(r) for r in (report.get("recommended_actions") or [])]
            critical_failed_titles = [str(t) for t in (report.get("critical_failed_titles") or [])]
            non_critical_failed_titles = [str(t) for t in (report.get("non_critical_failed_titles") or [])]
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
        critical_failed_titles=critical_failed_titles,
        non_critical_failed_titles=non_critical_failed_titles,
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
    rr_conf = derive_rr_confidence(rr)
    gate_conf = classify_gate_confidence(risk.confidence, rr_conf, gate_status)
    dec_summary = gate_decision_summary(gate_status, gate_conf)
    by_pri = group_required_actions_for_gate(required_actions)

    pr_risk_section: dict = {
        "status": risk.status,
        "label": risk.label,
        "score": round(risk.score, 1),
        "top_risk_factors": risk.top_risk_factors,
    }
    if risk.confidence is not None:
        pr_risk_section["confidence"] = risk.confidence

    return {
        "version": VERSION,
        "pr_risk": pr_risk_section,
        "release_readiness": {
            "status": rr.status,
            "score": round(rr.score, 1),
            "warnings": rr.warnings_count,
            "blockers": rr.blockers_count,
            "confidence": rr_conf,
            # SCRUM-197: failing E2E spec titles. Empty arrays when no failures
            # of that kind. Threaded from release-readiness/report.json so the
            # gate summary tells reviewers which specs failed without a
            # secondary artifact download.
            "critical_failed_titles": list(rr.critical_failed_titles),
            "non_critical_failed_titles": list(rr.non_critical_failed_titles),
        },
        "final_gate": {
            "status": gate_status,
            "confidence": gate_conf,
            "summary": dec_summary,
        },
        "required_actions": required_actions,
        "required_actions_by_priority": by_pri,
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

    rr_conf = derive_rr_confidence(rr)
    gate_conf = classify_gate_confidence(risk.confidence, rr_conf, gate_status)

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
        gate_decision_summary(gate_status, gate_conf),
        "",
        "## Required actions before merge",
        "",
        format_required_actions_grouped_markdown(required_actions),
        "",
    ]

    conf_lines: list[str] = []
    if risk.confidence is not None:
        conf_lines.append(f"- PR Risk test confidence: {risk.confidence} / 100")
    conf_lines.append(f"- Release Readiness confidence: {rr_conf} / 100")
    conf_lines.append(f"- Gate confidence: {gate_conf}")

    lines += [
        "",
        "## Supporting detail",
        "",
        f"- PR Risk score: {risk.score:.1f} / 100 ({risk.band})",
        f"- Release Readiness score: {rr.score:.1f} / 100",
        f"- Release Readiness warnings: {rr.warnings_count}",
        f"- Release Readiness blockers: {rr.blockers_count}",
    ] + conf_lines + [
        "",
        "---",
        GATE_FOOTER_MARKDOWN,
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
             "warnings": rr.warnings_count, "blockers": rr.blockers_count,
             "confidence": derive_rr_confidence(rr)}
            if rr else {"status": "UNKNOWN", "error": "Failed to parse release readiness input"}
        ),
        "final_gate": {
            "status": "BLOCK",
            "confidence": "low",
            "summary": "Gate inputs could not be parsed — treated as BLOCK. " + " | ".join(errors),
        },
        "required_actions": list(STANDARD_ACTIONS),
        "required_actions_by_priority": group_required_actions_for_gate(list(STANDARD_ACTIONS)),
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

    gate_conf = gate.get("confidence", "")
    conf_suffix = f" &nbsp;·&nbsp; confidence: {gate_conf}" if gate_conf else ""
    rr_conf = rr.get("confidence")
    rr_conf_str = f" · conf {rr_conf}/100" if rr_conf is not None else ""
    risk_conf = risk.get("confidence")
    risk_conf_str = f" · conf {risk_conf}/100" if risk_conf is not None else ""

    lines = [
        "## PR Gate",
        "",
        f"{emoji} **Final gate: {status}**{conf_suffix} &nbsp;·&nbsp; {gate.get('summary', '')}",
        "",
        "| PR Risk | Release Readiness |",
        "|---------|-------------------|",
        f"| {risk.get('label', '?')}{risk_conf_str} | {rr_emoji} {rr.get('status', '?')} ({rr.get('score', '?')}/100){rr_conf_str} |",
        "",
        GATE_FOOTER_MARKDOWN,
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
