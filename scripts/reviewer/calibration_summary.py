#!/usr/bin/env python3
"""SCRUM-516: aggregate the reviewer calibration CSV into a one-glance summary.

Reads ``ops/define-kpis/reviewer-calibration.csv`` (or ``--input``), counts
rows by bucket, and prints percentages + total token cost. Designed to be
run weekly during the Phase 1 calibration period (see
``docs/agent/reviewer-calibration.md``).

No dashboard, no fancy charts — just enough math to read a 20-30 row CSV
and apply the Phase 2 gate thresholds.
"""

from __future__ import annotations

import argparse
import csv
import sys
from collections import Counter
from pathlib import Path

_REPO_ROOT = Path(__file__).resolve().parents[2]
DEFAULT_INPUT = _REPO_ROOT / "ops" / "define-kpis" / "reviewer-calibration.csv"

VALID_BUCKETS = ("useful", "harmless", "noisy", "harmful")


def summarise(rows: list[dict]) -> dict:
    """Return a dict with counts, percentages, and token cost.

    Rows with an empty or invalid ``bucket`` are reported separately as
    ``skipped`` so the maintainer can spot data-entry errors.
    """
    counts: Counter[str] = Counter()
    skipped: list[dict] = []
    tokens = 0
    for row in rows:
        bucket = (row.get("bucket") or "").strip().lower()
        if bucket not in VALID_BUCKETS:
            skipped.append(row)
            continue
        counts[bucket] += 1
        try:
            tokens += int(row.get("tokens_used") or 0)
        except ValueError:
            pass
    total = sum(counts.values())
    pct = {b: (counts[b] / total * 100.0 if total else 0.0) for b in VALID_BUCKETS}
    return {
        "total": total,
        "counts": dict(counts),
        "pct": pct,
        "tokens": tokens,
        "skipped_rows": skipped,
    }


def _gate_decision(pct: dict[str, float], counts: dict[str, int]) -> str:
    """Apply the Phase 2 gate thresholds from reviewer-calibration.md."""
    if counts.get("harmful", 0) > 0:
        return "HALT — at least one harmful review; SCOPE.md / PROMPT.md fix required"
    useful = pct.get("useful", 0.0)
    if useful > 70.0:
        return "Phase 2 can proceed"
    if useful >= 50.0:
        return "Revise SCOPE.md or PROMPT.md and recalibrate"
    return "Reframe the program; reviewer is wrong tool for the surface"


def render(summary: dict) -> str:
    total = summary["total"]
    if total == 0:
        return "No calibration rows yet. Maintainer fills the CSV as reviews come in."
    pct = summary["pct"]
    counts = summary["counts"]
    lines = [
        f"Reviewer calibration — {total} scored row(s), {summary['tokens']:,} tokens consumed",
        "",
        "Buckets:",
    ]
    for bucket in VALID_BUCKETS:
        c = counts.get(bucket, 0)
        lines.append(f"  {bucket:<9} {c:>3}  ({pct[bucket]:>5.1f}%)")
    lines += [
        "",
        f"Phase 2 gate: {_gate_decision(pct, counts)}",
    ]
    if summary["skipped_rows"]:
        lines += [
            "",
            f"WARNING: {len(summary['skipped_rows'])} row(s) had invalid/empty bucket — fix the CSV",
        ]
    return "\n".join(lines)


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--input", type=Path, default=DEFAULT_INPUT)
    args = parser.parse_args(argv)

    if not args.input.exists():
        print(f"Input not found: {args.input}", file=sys.stderr)
        return 2
    with args.input.open(newline="") as f:
        rows = list(csv.DictReader(f))

    print(render(summarise(rows)))
    return 0


if __name__ == "__main__":
    sys.exit(main())


__all__ = ["VALID_BUCKETS", "summarise", "render"]
