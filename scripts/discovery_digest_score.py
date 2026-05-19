#!/usr/bin/env python3
"""SCRUM-499: CLI for running v2 clustering against a GitHub issues JSON.

The agent fetches obs-agent issues via ``mcp__github__list_issues`` and writes
the JSON to disk; this CLI consumes that file, runs the v2 clustering
algorithm from ``scripts/discovery_digest_cluster.py``, and emits a structured
cluster report for manual precision/recall scoring.

The CLI is read-only and dependency-free (stdlib only). Output is either
JSON (for further processing) or Markdown (for paste into the calibration
doc).
"""

from __future__ import annotations

import argparse
import json
import sys
from datetime import datetime
from pathlib import Path

_REPO_ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(_REPO_ROOT / "scripts"))

from discovery_digest_cluster import (  # noqa: E402
    ObsIssue,
    cluster,
)


def _parse_issues(data) -> list[ObsIssue]:
    """Accept either a top-level list or a dict with an 'issues' field."""
    if isinstance(data, dict):
        items = data.get("issues") or data.get("data") or []
    else:
        items = data
    issues: list[ObsIssue] = []
    for item in items:
        number = item.get("number")
        if number is None:
            continue
        created_at_str = item.get("createdAt") or item.get("created_at")
        if not created_at_str:
            continue
        # Python <3.11 fromisoformat doesn't accept 'Z'; normalise.
        if isinstance(created_at_str, str) and created_at_str.endswith("Z"):
            created_at_str = created_at_str[:-1] + "+00:00"
        try:
            created_at = datetime.fromisoformat(created_at_str)
        except ValueError:
            continue
        body = item.get("body") or ""
        issues.append(
            ObsIssue(number=int(number), created_at=created_at, body=body)
        )
    return issues


def build_report(issues: list[ObsIssue], max_days: int = 7) -> dict:
    """Run the v2 clustering algorithm and return a structured report."""
    clusters = cluster(issues, max_days=max_days)
    by_number = {i.number: i for i in issues}
    out_clusters = []
    for idx, members in enumerate(clusters, start=1):
        member_issues = [by_number[n] for n in members if n in by_number]
        if member_issues:
            shared_endpoints = sorted(
                set.intersection(*(i.endpoints for i in member_issues))
            )
        else:
            shared_endpoints = []
        colors = sorted({i.color for i in member_issues if i.color})
        dates = sorted(i.created_at.date().isoformat() for i in member_issues)
        out_clusters.append(
            {
                "cluster_id": idx,
                "size": len(members),
                "members": members,
                "shared_endpoints": shared_endpoints,
                "status_colors": colors,
                "date_range": [dates[0], dates[-1]] if dates else [],
            }
        )
    multi = sum(1 for c in out_clusters if c["size"] >= 2)
    singletons = sum(1 for c in out_clusters if c["size"] == 1)
    return {
        "version": "v3",
        "total_issues": len(issues),
        "cluster_count": len(out_clusters),
        "multi_member_clusters": multi,
        "singleton_clusters": singletons,
        "clusters": out_clusters,
    }


def render_markdown(report: dict) -> str:
    lines = [
        "# Discovery-digest v2 cluster report",
        "",
        f"- **Version:** {report['version']}",
        f"- **Total issues:** {report['total_issues']}",
        f"- **Clusters (≥ 2 members):** {report['multi_member_clusters']}",
        f"- **Singletons:** {report['singleton_clusters']}",
        "",
        "## Multi-member clusters",
        "",
    ]
    multi = [c for c in report["clusters"] if c["size"] >= 2]
    if not multi:
        lines.append("_(none)_")
    else:
        lines.append("| # | Size | Members | Shared endpoints | Colours | Date range |")
        lines.append("|---|---|---|---|---|---|")
        for c in multi:
            members = ", ".join(f"#{n}" for n in c["members"])
            eps = ", ".join(c["shared_endpoints"]) or "_(none)_"
            colors = ", ".join(c["status_colors"]) or "_(none)_"
            dr = (
                f"{c['date_range'][0]} → {c['date_range'][1]}"
                if c["date_range"]
                else "_(n/a)_"
            )
            lines.append(
                f"| {c['cluster_id']} | {c['size']} | {members} | {eps} | {colors} | {dr} |"
            )
    lines.extend(["", "## Singletons", ""])
    singletons = [c for c in report["clusters"] if c["size"] == 1]
    if not singletons:
        lines.append("_(none)_")
    else:
        for c in singletons:
            lines.append(f"- #{c['members'][0]}")
    return "\n".join(lines) + "\n"


def main(argv: list[str] | None = None) -> int:
    p = argparse.ArgumentParser(
        description=(
            "Run v2 discovery-digest clustering against an issues JSON file "
            "from mcp__github__list_issues."
        )
    )
    p.add_argument(
        "--issues",
        type=Path,
        required=True,
        help="Path to JSON file with GitHub issues.",
    )
    p.add_argument(
        "--max-days",
        type=int,
        default=7,
        help="Date-proximity gate in calendar days (default 7).",
    )
    p.add_argument(
        "--format",
        choices=["json", "markdown"],
        default="json",
        help="Output format.",
    )
    args = p.parse_args(argv)

    data = json.loads(args.issues.read_text())
    issues = _parse_issues(data)
    report = build_report(issues, max_days=args.max_days)
    if args.format == "json":
        print(json.dumps(report, indent=2))
    else:
        print(render_markdown(report))
    return 0


if __name__ == "__main__":
    sys.exit(main())
