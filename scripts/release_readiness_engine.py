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
    reasons: list[str] = field(default_factory=list)
    warnings: list[str] = field(default_factory=list)
    blockers: list[str] = field(default_factory=list)
    failed_checks: list[str] = field(default_factory=list)
    changed_files: list[str] = field(default_factory=list)
    risks_triggered: list[str] = field(default_factory=list)
    validations_satisfied: dict[str, bool] = field(default_factory=dict)
    validations_required: list[str] = field(default_factory=list)
    evidence: dict[str, Any] = field(default_factory=dict)
    recommended_actions: list[str] = field(default_factory=list)
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
    if blockers:
        return "BLOCK"
    if score < warn_threshold:
        return "BLOCK"
    if not warnings and score >= pass_threshold:
        return "PASS"
    return "WARN"


def compute_readiness(
    *,
    config: dict,
    changed_files: list[str],
    smoke: Optional[dict],
    e2e: Optional[dict],
    coverage: Optional[dict],
    prod_health: Optional[dict],
    migration_validated_cli: bool,
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
    note = (smoke or {}).get("config_validation_note") or (e2e or {}).get("config_validation_note")
    if risky_hits and not note:
        warnings.append("Risky config/workflow paths changed without explicit validation note in evidence")
        score -= float(penalties.get("risky_config_without_note", 10))
        failed_checks.append("risky_config_without_note")

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

    # Clamp score
    score = max(0.0, min(max_score, score))

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

    # Reasons summary (keep concise)
    reasons.append(f"Score={score:.1f}/{max_score} (pass>={pass_th}, warn>={warn_th})")
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

    return ReadinessResult(
        outcome=outcome,
        score=score,
        reasons=reasons,
        warnings=warnings,
        blockers=blockers,
        failed_checks=failed_checks,
        changed_files=sorted(changed_files),
        risks_triggered=sorted(risks),
        validations_satisfied=val_map,
        validations_required=validations_required_list,
        evidence={
            "smoke_present": smoke is not None,
            "e2e_present": e2e is not None,
            "coverage_present": coverage is not None,
            "prod_health_present": prod_health is not None,
        },
        recommended_actions=recommended,
    )

