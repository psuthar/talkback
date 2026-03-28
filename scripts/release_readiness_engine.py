from __future__ import annotations

import fnmatch
import os
from dataclasses import dataclass, field
from datetime import datetime, timezone
from typing import Any, Optional


@dataclass
class ReadinessResult:
    outcome: str  # PASS | WARN | BLOCK
    score: float
    max_score: float = 100.0
    pass_threshold: float = 85.0
    warn_threshold: float = 60.0
    reasons: list[str] = field(default_factory=list)
    warnings: list[str] = field(default_factory=list)
    blockers: list[str] = field(default_factory=list)
    failed_checks: list[str] = field(default_factory=list)
    changed_files: list[str] = field(default_factory=list)
    risks_triggered: list[str] = field(default_factory=list)
    validations: dict[str, str] = field(default_factory=dict)  # satisfied | missing | not_required | not_evaluated
    validations_required: list[str] = field(default_factory=list)
    evidence: dict[str, Any] = field(default_factory=dict)
    recommended_actions: list[str] = field(default_factory=list)
    # Structured per-check remediation guidance, populated from config.yaml remediation mapping.
    remediation_items: list[dict] = field(default_factory=list)
    # Explains when the final outcome differs from what score alone would produce.
    # Empty when score-only and final outcome agree (e.g. score < warn_threshold → BLOCK).
    outcome_overrides: list[str] = field(default_factory=list)
    timestamp_utc: str = field(default_factory=lambda: datetime.now(timezone.utc).isoformat())


def _normalize(rel_path: str) -> str:
    return rel_path.replace("\\", "/")


def match_patterns(rel_path: str, patterns: list[str]) -> bool:
    """
    Deterministic glob matching for config patterns.

    Supports:
    - normal fnmatch patterns
    - directory-prefix patterns like "internal/processing/**" (treated as prefix match)
    """
    normalized = _normalize(rel_path)
    base = os.path.basename(normalized)
    for p in patterns:
        if fnmatch.fnmatch(normalized, p) or fnmatch.fnmatch(base, p):
            return True
        # Directory prefix style, e.g. internal/processing/**
        pref = p.rstrip("*").rstrip("/")
        if pref:
            if normalized == pref or normalized.startswith(pref + "/"):
                return True
    return False


def risks_for_files(changed_files: list[str], config: dict) -> set[str]:
    risks: set[str] = set()
    rules = config.get("risk_from_paths", [])
    for rule in rules:
        cats = rule.get("categories", []) or []
        pats = rule.get("patterns", []) or []
        for f in changed_files:
            if match_patterns(f, pats):
                risks.update(cats)
    return risks


def _smoke_passed(smoke: Optional[dict]) -> bool:
    if not smoke or not isinstance(smoke, dict) or smoke.get("_parse_error"):
        return False
    st = str(smoke.get("status", "")).lower()
    if smoke.get("passed") is True:
        return True
    return st in ("passed", "pass", "success")


def _e2e_passed(e2e: Optional[dict]) -> bool:
    if not e2e or not isinstance(e2e, dict) or e2e.get("_parse_error"):
        return False
    st = str(e2e.get("status", "")).lower()
    if st == "skipped":
        return False
    if e2e.get("failed_count", 0) and int(e2e.get("failed_count", 0) or 0) > 0:
        return False
    return st in ("passed", "pass", "success") or e2e.get("passed") is True


def _failure_is_critical(title: str, patterns: list[str]) -> bool:
    t = (title or "").lower()
    return any(p.lower() in t for p in patterns)


def merge_validations(smoke: Optional[dict], e2e: Optional[dict], config: dict) -> dict[str, bool]:
    """
    Merge explicit validation booleans from evidence JSON with deterministic inference.

    Explicit False from evidence always wins (no override by inference).
    """
    out: dict[str, bool] = {}

    for data in (("smoke", smoke), ("e2e", e2e)):
        _, evid = data
        if not evid or not isinstance(evid, dict):
            continue
        v = evid.get("validations")
        if isinstance(v, dict):
            for k, val in v.items():
                if val is True:
                    out[k] = True
                elif val is False:
                    out[k] = False

        for k in ("auth_session", "upload_extraction", "nav_assets", "viewer_materials", "qa_rag", "migrations_validated"):
            if evid.get(k) is True:
                out[k] = True

    infer = config.get("infer_validations_when_pass", {}) or {}
    if _smoke_passed(smoke):
        for k in infer.get("smoke", []) or []:
            if out.get(k) is not False:
                out[k] = True
    if _e2e_passed(e2e):
        for k in infer.get("e2e", []) or []:
            if out.get(k) is not False:
                out[k] = True

    return out


def _classify_e2e_failures(e2e: Optional[dict], critical_patterns: list[str]) -> tuple[list[str], list[str], int]:
    """
    Returns (critical_titles, non_critical_titles, retries_count).
    """
    if not e2e or not isinstance(e2e, dict):
        return [], [], 0
    retries = int(e2e.get("retries", e2e.get("flaky_retried", 0)) or 0)
    critical: list[str] = []
    non_critical: list[str] = []

    failures = e2e.get("failures")
    if not isinstance(failures, list):
        failures = []
    for f in failures:
        if not isinstance(f, dict):
            continue
        title = str(f.get("title", f.get("name", "")))
        if _failure_is_critical(title, critical_patterns):
            critical.append(title)
        else:
            non_critical.append(title)

    # Some runners provide `critical_failures` list; treat as critical.
    extra_crit = e2e.get("critical_failures")
    if isinstance(extra_crit, list):
        for c in extra_crit:
            critical.append(str(c))

    status = str(e2e.get("status", "")).lower()
    failed_count = int(e2e.get("failed_count", 0) or 0)
    # If the runner says failed but provides no failure records, treat as critical.
    if (status in ("failed", "fail") or failed_count > 0) and not failures and not critical and not non_critical:
        critical.append("e2e_unlisted_failures")

    return critical, non_critical, retries


def decide_outcome(score: float, blockers: list[str], warnings: list[str], pass_threshold: float, warn_threshold: float) -> str:
    # PASS requires BOTH: score >= pass_threshold AND no warnings.
    # Warnings alone (without blockers) demote an otherwise-qualifying score to WARN.
    if blockers:
        return "BLOCK"
    if score < warn_threshold:
        return "BLOCK"
    if not warnings and score >= pass_threshold:
        return "PASS"
    return "WARN"


def _score_only_outcome(score: float, pass_threshold: float, warn_threshold: float) -> str:
    """What the outcome would be based purely on score, ignoring blockers and warnings."""
    if score < warn_threshold:
        return "BLOCK"
    if score >= pass_threshold:
        return "PASS"
    return "WARN"


def compute_outcome_overrides(
    score: float,
    warnings: list[str],
    outcome: str,
    pass_threshold: float,
    warn_threshold: float,
) -> list[str]:
    """
    Returns strings explaining why the final outcome differs from what score alone would produce.
    Empty when score-only outcome already matches the final outcome (no override occurred).

    Note: when hard blockers are present the score is already floored to block_score (0) before
    this function is called, so score_only will agree with outcome==BLOCK and no override fires.
    The only remaining override is warnings suppressing a score-qualifying PASS to WARN.
    """
    score_only = _score_only_outcome(score, pass_threshold, warn_threshold)
    if score_only == outcome:
        return []
    overrides: list[str] = []
    if score_only == "PASS" and outcome == "WARN":
        overrides.append(
            f"warnings_suppress_pass: {len(warnings)} warning(s) present; "
            f"score {score:.1f} qualifies for PASS (>={pass_threshold:.0f}) "
            f"but outcome demoted to WARN"
        )
    return overrides


def build_remediation_items(failed_checks: list[str], config: dict) -> list[dict]:
    """Map each failed check key to its remediation entry from config.yaml.

    Returns a list of dicts with keys: check, severity, likely_cause,
    recommended_action, fix_type.  Unknown keys get a minimal fallback entry.
    """
    remediation_map = config.get("remediation", {}) or {}
    items: list[dict] = []
    for check in failed_checks:
        entry = remediation_map.get(check) or {}
        items.append({
            "check": check,
            "severity": entry.get("severity", "warn"),
            "likely_cause": entry.get("likely_cause", ""),
            "recommended_action": entry.get("recommended_action", f"Investigate check: {check}"),
            "fix_type": entry.get("fix_type", ""),
        })
    return items


def compute_readiness(
    *,
    config: dict,
    changed_files: list[str],
    smoke: Optional[dict],
    e2e: Optional[dict],
    coverage: Optional[dict],
    prod_health: Optional[dict],
    migration_validated_cli: bool,
    commit_validation_note: bool = False,
    commit_validation_snippet: str = "",
    pr_risk: Optional[dict] = None,
) -> ReadinessResult:
    penalties = config.get("scoring", {}).get("penalties", {}) or {}
    max_score = float(config.get("scoring", {}).get("max_score", 100))
    pass_th = float(config.get("scoring", {}).get("pass_threshold", 80))
    warn_th = float(config.get("scoring", {}).get("warn_threshold", 60))
    crit_patterns = config.get("e2e_critical_name_patterns", []) or []

    risks = risks_for_files(changed_files, config)
    val_map = merge_validations(smoke, e2e, config)
    if migration_validated_cli:
        val_map["migrations_validated"] = True

    reasons: list[str] = []
    warnings: list[str] = []
    blockers: list[str] = []
    failed_checks: list[str] = []
    recommended: list[str] = []
    score = max_score

    # Evidence gaps (soft penalties + warnings)
    if smoke is None:
        warnings.append("Smoke results artifact missing or unreadable")
        score -= float(penalties.get("missing_smoke_artifact", 25))
        failed_checks.append("smoke_artifact")
    if e2e is None:
        warnings.append("E2E test results artifact missing or unreadable")
        score -= float(penalties.get("missing_e2e_artifact", 15))
        failed_checks.append("e2e_artifact")
    else:
        st = str(e2e.get("status", "")).lower()
        if st == "skipped":
            warnings.append("E2E was skipped in CI (no inference from E2E)")
            score -= float(penalties.get("missing_e2e_artifact", 10))
            failed_checks.append("e2e_skipped")

    if coverage is None:
        warnings.append("Coverage summary not provided (confidence reduced)")
        score -= float(penalties.get("missing_coverage_artifact", 5))
    if prod_health is None:
        warnings.append("Production health snapshot not provided (optional)")
        score -= float(penalties.get("missing_prod_health_artifact", 5))

    # Hard rule: smoke must pass
    if smoke is not None:
        if smoke.get("_parse_error"):
            blockers.append(f"Smoke results parse error: {smoke.get('_parse_error')}")
            failed_checks.append("smoke_parse_error")
        elif not _smoke_passed(smoke):
            blockers.append("Smoke tests did not pass")
            failed_checks.append("smoke_failed")
    smoke_failed = any(x == "smoke_failed" for x in failed_checks) or any("Smoke tests did not pass" in b for b in blockers)

    # Hard rule: critical E2E blocks
    critical_titles, non_critical_titles, retries = _classify_e2e_failures(e2e, crit_patterns)
    if critical_titles:
        blockers.append(f"Critical E2E failures: {len(critical_titles)}")
        failed_checks.append("e2e_critical")
    if non_critical_titles:
        warnings.append(f"Non-critical E2E failures: {len(non_critical_titles)}")
        score -= float(penalties.get("non_critical_e2e_failure", 15))
        failed_checks.append("e2e_non_critical")
    if retries > 0:
        warnings.append(f"E2E retries recorded: {retries}")
        score -= float(penalties.get("e2e_retries_or_flaky", 10))
        failed_checks.append("e2e_retries")

    # Coverage regression warning
    if coverage and isinstance(coverage, dict):
        line = coverage.get("line_percent")
        base = coverage.get("baseline_percent")
        if line is not None and base is not None and float(line) < float(base):
            warnings.append(f"Coverage regression: {line}% vs baseline {base}%")
            score -= float(penalties.get("coverage_regression", 12))
            failed_checks.append("coverage_regression")

    # Risky config without validation note warning
    risky_hits = [f for f in changed_files if match_patterns(f, config.get("risky_config_patterns", []) or [])]
    evidence_json_note = (smoke or {}).get("config_validation_note") or (e2e or {}).get("config_validation_note")
    note = commit_validation_note or evidence_json_note
    validation_note_source = (
        "commit_message" if commit_validation_note
        else ("evidence_json" if evidence_json_note else "none")
    )
    if risky_hits and not note:
        files_note = ", ".join(sorted(risky_hits)[:3])
        if len(risky_hits) > 3:
            files_note += f" (+{len(risky_hits) - 3} more)"
        warnings.append(f"Risky config/workflow paths changed without validation note: {files_note}")
        score -= float(penalties.get("risky_config_without_note", 10))
        failed_checks.append("risky_config_without_note")

    # PR Risk integration: cap readiness outcome by PR Risk merge recommendation.
    # When pr_risk.json is absent or unparseable, this block is skipped (graceful degradation).
    if pr_risk and isinstance(pr_risk, dict) and not pr_risk.get("_parse_error"):
        enforcement = pr_risk.get("enforcement") or {}
        pr_rec = str(enforcement.get("merge_recommendation") or "").lower()
        risk_band = pr_risk.get("risk_band", "unknown")
        risk_score = pr_risk.get("risk_score", 0)
        if pr_rec == "block":
            blockers.append(
                f"PR Risk merge recommendation is BLOCK "
                f"(band={risk_band}, score={risk_score:.0f}) — "
                "resolve PR Risk required actions before release"
            )
            failed_checks.append("pr_risk_block")
        elif pr_rec == "warn":
            warnings.append(
                f"PR Risk merge recommendation is WARN "
                f"(band={risk_band}, score={risk_score:.0f}) — "
                "review PR Risk required actions before release"
            )
            failed_checks.append("pr_risk_warn")
        # Surface evidence summary in reasons for report visibility.
        ev_summary = enforcement.get("evidence_summary") or {}
        ev_total = sum(ev_summary.get(k, 0) for k in ("pass_count", "missing_count", "unknown_count", "fail_count"))
        if ev_total > 0:
            reasons.append(
                f"PR Risk evidence: "
                f"{ev_summary.get('pass_count', 0)} pass · "
                f"{ev_summary.get('missing_count', 0)} missing · "
                f"{ev_summary.get('unknown_count', 0)} unknown · "
                f"{ev_summary.get('fail_count', 0)} fail"
            )

    # Risk validation blockers
    validations_required: set[str] = set()
    for r in risks:
        if r == "migrations":
            validations_required.add("migrations_validated")
        else:
            validations_required.add(r)

    validations_required_list = sorted(validations_required)
    missing_vals = [v for v in validations_required_list if v and not val_map.get(v)]
    if missing_vals and risks:
        blockers.append(f"Changed areas require validation evidence missing: {', '.join(missing_vals)}")
        failed_checks.append("risk_without_validation")

    # Compute explicit per-validation statuses for output.
    # Rule: a validation is "satisfied" only when it was required AND evidenced.
    # Inferred-but-not-required validations are "not_required", not "satisfied".
    no_evidence = smoke is None and e2e is None
    known_keys = set(config.get("validations", {}).keys())
    # Include all known keys, all required keys, and any explicitly evidenced keys.
    # Keys that are neither required nor evidenced are omitted (pure noise).
    keys_to_report = known_keys | set(validations_required_list) | {k for k, v in val_map.items() if v is True}
    validation_statuses: dict[str, str] = {}
    for k in sorted(keys_to_report):
        is_required = k in validations_required_list
        evidenced_true = val_map.get(k) is True
        if is_required:
            if evidenced_true:
                validation_statuses[k] = "satisfied"
            elif no_evidence:
                validation_statuses[k] = "not_evaluated"
            else:
                validation_statuses[k] = "missing"
        else:
            if evidenced_true:
                validation_statuses[k] = "not_required"
            # Not required + not evidenced → omit

    # Clamp score to [0, max_score].
    score = max(0.0, min(max_score, score))

    # When hard blockers exist, floor score to block_score (config default: 0).
    # Hard blockers (smoke failure, critical E2E, missing required validation) carry no
    # soft penalty, so without this step score could read 90/100 while outcome is BLOCK.
    # Flooring to block_score (0) makes score consistent with the BLOCK outcome.
    block_score_val = float(config.get("scoring", {}).get("block_score", 0))
    if blockers:
        score = min(score, block_score_val)

    # Outcome decision (explicit thresholds, no ambiguity)
    outcome = decide_outcome(
        score=score,
        blockers=blockers,
        warnings=warnings,
        pass_threshold=pass_th,
        warn_threshold=warn_th,
    )

    if outcome == "PASS" and score < pass_th:
        # Defensive: should be impossible due to decide_outcome ordering.
        outcome = "WARN"

    outcome_overrides = compute_outcome_overrides(
        score=score,
        warnings=warnings,
        outcome=outcome,
        pass_threshold=pass_th,
        warn_threshold=warn_th,
    )

    # Reasons summary — document both PASS conditions so the threshold line is never misleading.
    # PASS requires score >= pass_threshold AND no warnings; score alone is not sufficient.
    reasons.append(
        f"Score={score:.1f}/{max_score} "
        f"(PASS: score>={pass_th} AND 0 warnings; WARN: score>={warn_th} or warnings present)"
    )
    reasons.append(f"Analyzed {len(changed_files)} changed file(s)")

    if risks:
        reasons.append(f"Risks triggered: {', '.join(sorted(risks))}")
    else:
        reasons.append("No risk categories triggered (skip validation gating)")

    # Recommended actions
    if outcome == "BLOCK":
        recommended.append("Fix blocking items before deploy")
    elif outcome == "WARN":
        recommended.append("Review warnings before deploy")

    remediation_items = build_remediation_items(failed_checks, config)

    return ReadinessResult(
        outcome=outcome,
        score=score,
        max_score=max_score,
        pass_threshold=pass_th,
        warn_threshold=warn_th,
        reasons=reasons,
        warnings=warnings,
        blockers=blockers,
        failed_checks=failed_checks,
        changed_files=sorted(changed_files),
        risks_triggered=sorted(risks),
        validations=validation_statuses,
        validations_required=validations_required_list,
        evidence={
            "smoke_present": smoke is not None,
            "e2e_present": e2e is not None,
            "coverage_present": coverage is not None,
            "prod_health_present": prod_health is not None,
            "validation_note_present": bool(note),
            "validation_note_source": validation_note_source,
            "validation_note_snippet": commit_validation_snippet if commit_validation_note else "",
        },
        recommended_actions=recommended,
        remediation_items=remediation_items,
        outcome_overrides=outcome_overrides,
    )

