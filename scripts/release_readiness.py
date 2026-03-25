#!/usr/bin/env python3
"""
TalkBack release-readiness evaluator (deterministic).
Evidence in → score + PASS/WARN/BLOCK out. No LLM in the decision path.
"""
from __future__ import annotations

import argparse
import json
import os
import subprocess
import sys
from dataclasses import asdict
from datetime import datetime, timezone
from pathlib import Path
from typing import Any, Optional

try:
    import yaml
except ImportError:
    print("ERROR: PyYAML required. pip install -r ops/release-readiness/requirements.txt", file=sys.stderr)
    sys.exit(2)

try:
    from release_readiness_engine import ReadinessResult, compute_readiness
except ImportError:
    # When invoked as `python scripts/release_readiness.py` from repo root, scripts/ is on sys.path.
    # Fallback for invocation styles that place repo root first.
    import importlib.util, sys as _sys
    _spec = importlib.util.spec_from_file_location(
        "release_readiness_engine",
        Path(__file__).parent / "release_readiness_engine.py",
    )
    _mod = importlib.util.module_from_spec(_spec)  # type: ignore[arg-type]
    _spec.loader.exec_module(_mod)  # type: ignore[union-attr]
    ReadinessResult = _mod.ReadinessResult
    compute_readiness = _mod.compute_readiness


def _read_json(path: Path) -> Optional[dict]:
    if not path or not path.is_file():
        return None
    try:
        with open(path, encoding="utf-8") as f:
            return json.load(f)
    except (json.JSONDecodeError, OSError) as e:
        return {"_parse_error": str(e)}


def git_changed_files(repo_root: Path, base_ref: str) -> list[str]:
    try:
        r = subprocess.run(
            ["git", "-C", str(repo_root), "diff", "--name-only", f"{base_ref}...HEAD"],
            capture_output=True,
            text=True,
            check=False,
        )
        if r.returncode != 0:
            # Fallback: compare to merge-base
            r = subprocess.run(
                ["git", "-C", str(repo_root), "diff", "--name-only", base_ref],
                capture_output=True,
                text=True,
                check=False,
            )
        if r.returncode != 0:
            return []
        return [ln.strip() for ln in r.stdout.splitlines() if ln.strip()]
    except OSError:
        return []


def evaluate(
    repo_root: Path,
    config: dict,
    base_ref: str,
    smoke: Optional[dict],
    e2e: Optional[dict],
    coverage: Optional[dict],
    prod_health: Optional[dict],
    migration_validated_cli: bool,
    empty_diff: bool = False,
) -> ReadinessResult:
    changed = [] if empty_diff else git_changed_files(repo_root, base_ref)
    return compute_readiness(
        config=config,
        changed_files=changed,
        smoke=smoke,
        e2e=e2e,
        coverage=coverage,
        prod_health=prod_health,
        migration_validated_cli=migration_validated_cli,
    )


def render_markdown(r: ReadinessResult, config_version: Any) -> str:
    lines = [
        "# TalkBack release readiness report",
        "",
        f"**Generated:** {datetime.now(timezone.utc).isoformat()}",
        f"**Config version:** {config_version}",
        "",
        f"## Result: **{r.outcome}** (score {r.score:.1f})",
        "",
        "### Summary",
        "",
    ]
    for x in r.reasons:
        lines.append(f"- {x}")
    if r.blockers:
        lines.extend(["", "### Blockers", ""])
        for b in r.blockers:
            lines.append(f"- {b}")
    if r.warnings:
        lines.extend(["", "### Warnings", ""])
        for w in r.warnings:
            lines.append(f"- {w}")
    lines.extend(["", "### Risks from changed paths", ""])
    if r.risks_triggered:
        for x in r.risks_triggered:
            lines.append(f"- `{x}`")
    else:
        lines.append("- (none)")
    lines.extend(["", "### Validations", ""])
    lines.append("| Key | Satisfied |")
    lines.append("|-----|-----------|")
    for k in sorted(set(r.validations_required) | set(r.validations_satisfied.keys())):
        sat = "yes" if r.validations_satisfied.get(k) else "no"
        req = "required" if k in r.validations_required else ""
        lines.append(f"| {k} | {sat} {req}|")
    if r.failed_checks:
        lines.extend(["", "### Failed checks", ""])
        for f in r.failed_checks:
            lines.append(f"- `{f}`")
    if r.recommended_actions:
        lines.extend(["", "### Recommended actions", ""])
        for a in r.recommended_actions:
            lines.append(f"- {a}")
    lines.extend(["", "---", "*Deterministic scoring only (no LLM in the decision path).*", ""])
    return "\n".join(lines)


def main() -> int:
    ap = argparse.ArgumentParser(description="TalkBack release readiness (deterministic)")
    ap.add_argument("--repo-root", type=Path, default=Path("."))
    ap.add_argument("--config", type=Path, default=Path("ops/release-readiness/config.yaml"))
    ap.add_argument("--base-ref", default=os.environ.get("RELEASE_READINESS_BASE_REF", "origin/main"))
    ap.add_argument("--smoke-results", type=Path, help="JSON smoke summary")
    ap.add_argument("--e2e-results", type=Path, help="JSON E2E summary")
    ap.add_argument("--coverage", type=Path, help="JSON coverage summary")
    ap.add_argument("--prod-health", type=Path, help="JSON prod health snapshot")
    ap.add_argument("--migration-validated", action="store_true", help="CI ran migrations successfully")
    ap.add_argument("--output-dir", type=Path, default=Path("artifacts/release-readiness"))
    ap.add_argument(
        "--fixture-mode",
        action="store_true",
        help="Load fixtures from ops/release-readiness/fixtures/sample_pass",
    )
    ap.add_argument(
        "--fixture-variant",
        default="pass",
        choices=["pass", "warn", "block"],
        help="Which fixture variant to load (only used with --fixture-mode)",
    )
    ap.add_argument(
        "--empty-diff",
        action="store_true",
        help="Ignore git diff (demo determinism: no path-based risks)",
    )
    args = ap.parse_args()

    repo_root = args.repo_root.resolve()
    config_path = args.config if args.config.is_absolute() else repo_root / args.config
    with open(config_path, encoding="utf-8") as f:
        config = yaml.safe_load(f)

    if args.fixture_mode:
        fix_root = repo_root / "ops" / "release-readiness" / "fixtures"
        if args.fixture_variant == "block":
            fix = fix_root / "sample_block_smoke"
        elif args.fixture_variant == "warn":
            fix = fix_root / "sample_warn"
        else:
            fix = fix_root / "sample_pass"
        smoke = _read_json(fix / "smoke_results.json")
        e2e = _read_json(fix / "e2e_results.json")
        coverage = _read_json(fix / "coverage.json")
        prod_health = _read_json(fix / "prod_health.json")
    else:
        smoke = _read_json(args.smoke_results) if args.smoke_results else None
        e2e = _read_json(args.e2e_results) if args.e2e_results else None
        coverage = _read_json(args.coverage) if args.coverage else None
        prod_health = _read_json(args.prod_health) if args.prod_health else None

    result = evaluate(
        repo_root,
        config,
        args.base_ref,
        smoke,
        e2e,
        coverage,
        prod_health,
        args.migration_validated,
        empty_diff=args.empty_diff or args.fixture_mode,
    )

    out_dir = args.output_dir if args.output_dir.is_absolute() else repo_root / args.output_dir
    out_dir.mkdir(parents=True, exist_ok=True)

    payload = asdict(result)
    payload["config_path"] = str(config_path)
    payload["base_ref"] = args.base_ref
    payload["timestamp_utc"] = datetime.now(timezone.utc).isoformat()
    payload["deterministic_summary"] = (
        f"{result.outcome}: score={result.score:.1f}, blockers={len(result.blockers)}, warnings={len(result.warnings)}"
    )

    report_json = out_dir / "report.json"
    with open(report_json, "w", encoding="utf-8") as f:
        json.dump(payload, f, indent=2)

    md = render_markdown(result, config.get("version"))
    report_md = out_dir / "report.md"
    with open(report_md, "w", encoding="utf-8") as f:
        f.write(md)

    print(md)
    print(f"\nWrote {report_json} and {report_md}", file=sys.stderr)

    return 0 if result.outcome != "BLOCK" else 1


if __name__ == "__main__":
    sys.exit(main())
