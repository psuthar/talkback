#!/usr/bin/env python3
"""SCRUM-531: dry-run close.py against historical PR metadata for Phase 2 validation.

Constructs FakeGitHubAPI / FakeJiraAPI from `gh pr view` output so the
validation runs without live Jira credentials, while still exercising the
real close() logic + template rendering.

  python -m scripts.full_auto.validate_dryrun SCRUM-XX --pr N \\
        [--path polling|webhook|manual-override] \\
        --out ops/full-auto-validation/SCRUM-XX.dryrun.json

Output JSON includes the PR snapshot, the planned actions, the rendered
closure comment, and the merge SHA close.py would have used. SUMMARY.md
diffs the rendered comment against the closure comment Claude actually
posted on each historical ticket.
"""

from __future__ import annotations

import argparse
import json
import subprocess
import sys
from pathlib import Path

_REPO_ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(_REPO_ROOT / "scripts"))

from full_auto.close import DEFAULT_REPO, close  # noqa: E402
from full_auto.lib.github import PRSnapshot  # noqa: E402
from full_auto.lib.templates import (  # noqa: E402
    MANUAL_OVERRIDE,
    POLLING,
    VALID_PATHS,
    ClosureContext,
    render,
)


class _HistoricalGitHub:
    def __init__(self, snapshot: PRSnapshot):
        self._snap = snapshot

    def read_pr(self, repo: str, pr_number: int) -> PRSnapshot:
        return self._snap

    def merge_pr(self, repo: str, pr_number: int) -> str:
        raise AssertionError("merge_pr should never be called in dry-run mode")


class _HistoricalJira:
    """Returns the canonical SCRUM transition table; never executes mutations."""

    def get_transitions(self, key: str):
        return [
            {"id": "21", "name": "In Progress"},
            {"id": "31", "name": "In Review"},
            {"id": "51", "name": "Done"},
        ]

    def transition(self, key: str, transition_id: str) -> None:
        raise AssertionError("transition should never be called in dry-run mode")

    def add_comment(self, key: str, body: str) -> int:
        raise AssertionError("add_comment should never be called in dry-run mode")


def _fetch_pr_snapshot(repo: str, pr_number: int) -> PRSnapshot:
    """Pull PR metadata via `gh pr view` and convert to PRSnapshot.

    Uses --repo to avoid relying on the current working tree's remote.
    """
    # gh's PR JSON doesn't expose a literal "merged" field — derive it from
    # mergedAt (set only when merged). state == "MERGED" also indicates merged.
    fields = (
        "number,state,mergeCommit,mergeable,mergeStateStatus,"
        "mergedAt,headRefName,baseRefName"
    )
    result = subprocess.run(
        ["gh", "pr", "view", str(pr_number), "--repo", repo, "--json", fields],
        capture_output=True,
        text=True,
        check=True,
    )
    data = json.loads(result.stdout)
    merge_commit = data.get("mergeCommit") or {}
    mergeable_state = (data.get("mergeStateStatus") or "unknown").lower()
    merged = bool(data.get("mergedAt")) or data.get("state") == "MERGED"
    return PRSnapshot(
        number=data["number"],
        state=data["state"].lower(),
        merged=merged,
        merge_commit_sha=merge_commit.get("oid"),
        mergeable_state=mergeable_state,
        head_ref=data["headRefName"],
        base_ref=data["baseRefName"],
    )


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("ticket")
    parser.add_argument("--pr", type=int, required=True)
    parser.add_argument("--repo", default=DEFAULT_REPO)
    parser.add_argument("--path", default=POLLING, choices=VALID_PATHS)
    parser.add_argument("--out", type=Path, required=True)
    args = parser.parse_args(argv)

    snap = _fetch_pr_snapshot(args.repo, args.pr)
    gh = _HistoricalGitHub(snap)
    jira = _HistoricalJira()

    result = close(
        args.ticket,
        pr_number=args.pr,
        path_indicator=args.path,
        dry_run=True,
        repo=args.repo,
        github_api=gh,
        jira_api=jira,
    )

    ctx = ClosureContext(
        ticket=args.ticket,
        pr_number=args.pr,
        merged_sha=(snap.merge_commit_sha or result.merged_sha or "<unknown>"),
        main_sha_after="<main-sha-after-pull>",
        final_gate_status=("manual_override" if args.path == MANUAL_OVERRIDE else "PASS"),
        branch_name=snap.head_ref,
    )
    would_post = render(args.path, ctx)

    out = {
        "ticket": args.ticket,
        "pr_number": args.pr,
        "path": args.path,
        "snapshot": {
            "number": snap.number,
            "state": snap.state,
            "merged": snap.merged,
            "merge_commit_sha": snap.merge_commit_sha,
            "mergeable_state": snap.mergeable_state,
            "head_ref": snap.head_ref,
            "base_ref": snap.base_ref,
        },
        "actions_taken": result.actions_taken,
        "aborted_reason": result.aborted_reason,
        "merged_sha_in_dryrun": result.merged_sha,
        "would_post_comment": would_post,
    }

    args.out.parent.mkdir(parents=True, exist_ok=True)
    args.out.write_text(json.dumps(out, indent=2) + "\n")
    print(f"Wrote {args.out}")
    return 0


if __name__ == "__main__":
    sys.exit(main() or 0)
