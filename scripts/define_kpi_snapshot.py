#!/usr/bin/env python3
"""SCRUM-488: DEFINE-domain KPI snapshot aggregator.

Reads pre-fetched Jira issues (the agent invokes the Atlassian MCP and writes
the result to a file), the local ``.epic-run/`` state directory, and the
lint-runs log to produce a dated snapshot under ``ops/define-kpis/``.

The script is a pure aggregator: it does not make network calls. The agent
runtime pulls Jira data via MCP and passes it on disk. This keeps the script
testable, credential-free, and re-runnable.

Typical agent-runtime invocation::

    # 1. Pull Jira data via MCP (jira_search_issues with KPI JQL) → /tmp/jira.json
    # 2. Run the aggregator:
    python3 scripts/define_kpi_snapshot.py --jira-issues /tmp/jira.json

KPI fields written (six top-level + raw breakdown):

  - timestamp                            ISO-8601 Zulu
  - lint_pass_rate                       float in [0,1] or null if no log
  - time_in_progress_to_pr_median_hours  float or null (changelog parse deferred)
  - agent_authoring_pct                  float in [0,1]
  - source_obs_agent_count               int
  - spec_halt_count                      int (from .epic-run/<EPIC>.json files)

See the parent Epic SCRUM-485 and plan ``.cursor/plans/define-domain-uplift.plan.md``.
"""

from __future__ import annotations

import argparse
import json
import sys
from datetime import datetime, timezone
from pathlib import Path


REQUIRED_FIELDS = (
    "timestamp",
    "lint_pass_rate",
    "time_in_progress_to_pr_median_hours",
    "agent_authoring_pct",
    "source_obs_agent_count",
    "spec_halt_count",
)

AGENT_AUTHORED_LABEL = "agent-authored"
OBS_AGENT_LABEL = "source:obs-agent"


def _read_jira_issues(path: Path) -> list[dict]:
    data = json.loads(path.read_text())
    issues = data.get("issues") if isinstance(data, dict) else data
    if not isinstance(issues, list):
        raise ValueError(
            f"expected issues list at {path}, got {type(issues).__name__}"
        )
    return issues


def _read_epic_run_files(epic_run_dir: Path) -> list[dict]:
    if not epic_run_dir.is_dir():
        return []
    out: list[dict] = []
    for f in sorted(epic_run_dir.glob("*.json")):
        try:
            out.append(json.loads(f.read_text()))
        except (json.JSONDecodeError, OSError):
            continue
    return out


def _lint_pass_rate(lint_log: Path) -> tuple[float | None, int, int]:
    if not lint_log.is_file():
        return None, 0, 0
    total = 0
    passes = 0
    for line in lint_log.read_text().splitlines():
        line = line.strip()
        if not line:
            continue
        try:
            row = json.loads(line)
        except json.JSONDecodeError:
            continue
        if not isinstance(row, dict):
            continue
        exit_code = row.get("exit")
        if exit_code is None:
            continue
        total += 1
        if int(exit_code) == 0:
            passes += 1
    rate = (passes / total) if total else None
    return rate, passes, total


def _label_count(issues: list[dict], label: str) -> int:
    n = 0
    for issue in issues:
        labels = (issue.get("fields") or {}).get("labels") or []
        if label in labels:
            n += 1
    return n


def _agent_authoring_pct(issues: list[dict]) -> tuple[float, int, int]:
    if not issues:
        return 0.0, 0, 0
    total = len(issues)
    agent_authored = _label_count(issues, AGENT_AUTHORED_LABEL)
    return agent_authored / total, agent_authored, total


def _spec_halts(epic_run_states: list[dict]) -> tuple[int, dict[str, int]]:
    by_cat: dict[str, int] = {}
    spec_missing = 0
    for state in epic_run_states:
        for ticket in state.get("tickets") or []:
            cat = ticket.get("halt_category")
            if isinstance(cat, str):
                by_cat[cat] = by_cat.get(cat, 0) + 1
                if cat == "spec_missing":
                    spec_missing += 1
        root_cat = state.get("halt_category")
        if isinstance(root_cat, str):
            by_cat[root_cat] = by_cat.get(root_cat, 0) + 1
            if root_cat == "spec_missing":
                spec_missing += 1
    return spec_missing, by_cat


def build_snapshot(
    issues: list[dict],
    epic_run_states: list[dict],
    lint_log: Path,
    now: datetime | None = None,
) -> dict:
    now = now or datetime.now(timezone.utc)
    lint_rate, lint_passes, lint_total = _lint_pass_rate(lint_log)
    auth_pct, auth_count, auth_total = _agent_authoring_pct(issues)
    spec_missing, halts_by_cat = _spec_halts(epic_run_states)
    obs_count = _label_count(issues, OBS_AGENT_LABEL)
    return {
        "timestamp": now.strftime("%Y-%m-%dT%H:%M:%SZ"),
        "lint_pass_rate": lint_rate,
        # KPI deferred: requires Jira changelog parsing per ticket. The field is
        # kept in the schema so consumers can rely on its presence; population
        # comes when In-Progress → In-Review changelog scraping lands.
        "time_in_progress_to_pr_median_hours": None,
        "agent_authoring_pct": auth_pct,
        "source_obs_agent_count": obs_count,
        "spec_halt_count": spec_missing,
        "raw": {
            "lint_runs_total": lint_total,
            "lint_passes": lint_passes,
            "agent_authored_count": auth_count,
            "total_tickets_sampled": auth_total,
            "halts_by_category": halts_by_cat,
        },
    }


def _resolve_output_path(output: Path | None, now: datetime) -> Path:
    if output is not None:
        return output
    base = Path("ops/define-kpis") / f"snapshot-{now.strftime('%Y-%m-%d')}.json"
    if not base.exists():
        return base
    return base.with_name(f"snapshot-{now.strftime('%Y-%m-%dT%H%M%SZ')}.json")


def main(argv: list[str] | None = None) -> int:
    p = argparse.ArgumentParser(
        description="Aggregate DEFINE-domain KPIs into a dated snapshot.",
    )
    p.add_argument(
        "--jira-issues",
        type=Path,
        required=True,
        help="Path to JSON with pre-fetched Jira issues (from MCP).",
    )
    p.add_argument("--epic-run-dir", type=Path, default=Path(".epic-run"))
    p.add_argument(
        "--lint-log",
        type=Path,
        default=Path("ops/define-kpis/lint-runs.log"),
    )
    p.add_argument(
        "--output",
        type=Path,
        default=None,
        help=(
            "Output path. Defaults to ops/define-kpis/snapshot-YYYY-MM-DD.json "
            "(timestamped suffix on same-day collision; never overwrites)."
        ),
    )
    args = p.parse_args(argv)

    issues = _read_jira_issues(args.jira_issues)
    epic_run_states = _read_epic_run_files(args.epic_run_dir)
    now = datetime.now(timezone.utc)
    snapshot = build_snapshot(issues, epic_run_states, args.lint_log, now=now)

    out = _resolve_output_path(args.output, now)
    out.parent.mkdir(parents=True, exist_ok=True)
    out.write_text(json.dumps(snapshot, indent=2) + "\n")
    lint_rate_str = (
        f"{snapshot['lint_pass_rate']:.2f}"
        if snapshot["lint_pass_rate"] is not None
        else "n/a"
    )
    print(
        f"DEFINE KPI snapshot v1: "
        f"agent_authoring_pct={snapshot['agent_authoring_pct']:.2f} "
        f"lint_pass_rate={lint_rate_str} "
        f"source_obs_agent_count={snapshot['source_obs_agent_count']} "
        f"spec_halt_count={snapshot['spec_halt_count']} -> {out}"
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
