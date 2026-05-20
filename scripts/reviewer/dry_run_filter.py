#!/usr/bin/env python3
"""SCRUM-510: dry-run the skip filter over the last 30 days of merged PRs.

Run on demand by a maintainer to validate the skip filter's behaviour
before wiring it into a live workflow. Outputs one line per PR:

    PR#NUM <decision> <reason>: <title>

where decision is REVIEW or SKIP and reason is one of the five skip
reasons (or empty for REVIEW). Output is also appended as JSONL to
``ops/define-kpis/reviewer-dry-run.log`` for diffing across runs.

Uses the local ``gh`` CLI to fetch PR metadata in one shot — no
per-PR round trip. Designed to be cheap and re-runnable; safe to run
in CI on a schedule once the contract stabilises.

Usage:
    python3 scripts/reviewer/dry_run_filter.py
    python3 scripts/reviewer/dry_run_filter.py --since 2026-04-01
    python3 scripts/reviewer/dry_run_filter.py --min-source-loc 50
"""

from __future__ import annotations

import argparse
import json
import subprocess
import sys
from datetime import datetime, timedelta, timezone
from pathlib import Path

_REPO_ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(_REPO_ROOT / "scripts"))

from reviewer.skip_filter import PRMetadata, should_skip  # noqa: E402

DEFAULT_LOG_PATH = _REPO_ROOT / "ops" / "define-kpis" / "reviewer-dry-run.log"


def _fetch_prs(since: str, limit: int) -> list[dict]:
    """Fetch merged PRs since ``since`` (YYYY-MM-DD)."""
    cmd = [
        "gh",
        "pr",
        "list",
        "--state",
        "merged",
        "--base",
        "main",
        "--search",
        f"merged:>={since}",
        "--limit",
        str(limit),
        "--json",
        "number,title,author,isDraft,labels,files,additions,deletions",
    ]
    result = subprocess.run(cmd, capture_output=True, text=True, check=True)
    return json.loads(result.stdout or "[]")


def _to_metadata(raw: dict) -> PRMetadata:
    return PRMetadata(
        number=raw["number"],
        title=raw["title"],
        author_login=(raw.get("author") or {}).get("login", "") or "",
        draft=bool(raw.get("isDraft", False)),
        labels=[lbl["name"] for lbl in raw.get("labels", []) or []],
        changed_files=[f["path"] for f in raw.get("files", []) or []],
        additions=int(raw.get("additions", 0) or 0),
        deletions=int(raw.get("deletions", 0) or 0),
    )


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--since",
        default=None,
        help="ISO date (YYYY-MM-DD). Defaults to 30 days ago in UTC.",
    )
    parser.add_argument("--limit", type=int, default=200)
    parser.add_argument(
        "--min-source-loc",
        type=int,
        default=None,
        help="Override the source-LOC skip threshold for this run.",
    )
    def _log_type(value: str) -> Path | None:
        # argparse passes '' for `--log=`; that should mean "disabled",
        # not the cwd Path('.') (which would IsADirectoryError on open).
        if value == "":
            return None
        return Path(value)

    parser.add_argument(
        "--log",
        type=_log_type,
        default=DEFAULT_LOG_PATH,
        help="JSONL log path (default ops/define-kpis/reviewer-dry-run.log). "
        "Pass empty string (--log=) to disable.",
    )
    args = parser.parse_args()

    since = args.since or (
        datetime.now(timezone.utc) - timedelta(days=30)
    ).strftime("%Y-%m-%d")

    prs = _fetch_prs(since, args.limit)
    if not prs:
        print(f"No merged PRs found since {since}", file=sys.stderr)
        return 0

    log_rows: list[dict] = []
    counts = {
        "review": 0,
        "draft": 0,
        "skip_label": 0,
        "bot_author": 0,
        "docs_only": 0,
        "under_loc_threshold": 0,
    }
    for raw in prs:
        meta = _to_metadata(raw)
        skip, reason = should_skip(meta, min_source_loc=args.min_source_loc)
        decision = "SKIP" if skip else "REVIEW"
        bucket = reason if skip else "review"
        counts[bucket] = counts.get(bucket, 0) + 1
        print(f"PR#{meta.number} {decision:6} {reason:20} {meta.title}")
        log_rows.append(
            {
                "ts": datetime.now(timezone.utc).isoformat(),
                "since": since,
                "pr": meta.number,
                "decision": decision,
                "reason": reason,
                "title": meta.title,
                "author": meta.author_login,
                "files": len(meta.changed_files),
                "loc": meta.additions + meta.deletions,
            }
        )

    print(
        "\nSummary: "
        + " ".join(f"{k}={v}" for k, v in counts.items() if v)
        + f"  total={len(prs)}",
        file=sys.stderr,
    )

    if args.log is not None:
        args.log.parent.mkdir(parents=True, exist_ok=True)
        with args.log.open("a") as f:
            for row in log_rows:
                f.write(json.dumps(row) + "\n")
        print(f"Wrote {len(log_rows)} rows to {args.log}", file=sys.stderr)

    return 0


if __name__ == "__main__":
    sys.exit(main())
