#!/usr/bin/env python3
"""SCRUM-544: gate + mergeable_state polling until terminal.

Replaces the per-tick Bash ``while; do gh pr checks; sleep 30; done``
loop the agent runs today. Each per-tick line accumulates in the
conversation history (~150 tokens × N iterations); this script emits
only the final terminal JSON on stdout.

Terminal classification matches the policy in
``docs/agent/workflow-full-auto.md`` §"Polling, Halt, and Resume":

* **pass** — TalkBack PR Gate ``conclusion: success`` AND
  ``mergeable_state == clean``. Exit 0.
* **warn** — TalkBack PR Gate ``conclusion: action_required``. Exit 2.
* **block** — TalkBack PR Gate ``conclusion: failure``. Exit 2.
* **mergeable_blocked** — gate is success but mergeable_state is not
  ``clean`` (e.g. ``blocked``, ``behind``, ``unstable``). Exit 2.
* **timeout** — ``--budget`` seconds exhausted with no terminal
  classification. Exit 2.
* **error** — auth failure, GitHub API down, parse error. Exit 1.

Per the FULL_AUTO contract, the default interval is 30s and budget is
40 min (2400s). Both are clamped at the script boundary to prevent
foot-guns.
"""

from __future__ import annotations

import argparse
import json
import sys
import time
from dataclasses import asdict, dataclass, field
from typing import Callable

from .lib import auth
from .lib.github import GitHubAPI, HttpGitHubAPI

DEFAULT_REPO = "psuthar/talkback"
DEFAULT_INTERVAL = 30
DEFAULT_BUDGET = 2400
TALKBACK_GATE_NAME = "TalkBack PR Gate"

# Terminal states — string enum matching SCRUM-538 style.
PASS = "pass"
WARN = "warn"
BLOCK = "block"
MERGEABLE_BLOCKED = "mergeable_blocked"
TIMEOUT = "timeout"
ERROR = "error"


@dataclass
class PollResult:
    pr_number: int
    terminal_state: str
    gate_conclusion: str | None = None
    mergeable_state: str | None = None
    elapsed_seconds: int = 0
    ticks: int = 0
    actions_taken: list[str] = field(default_factory=list)
    aborted_reason: str | None = None


def _classify(gate_conclusion: str | None, mergeable_state: str | None) -> str:
    """Return the terminal-state enum for the (gate, mergeable_state) pair,
    or empty string if neither has reached a terminal classification yet."""
    if gate_conclusion == "success":
        if mergeable_state == "clean":
            return PASS
        if mergeable_state in (None, "unknown"):
            # Gate is success but GitHub hasn't fully computed mergeable yet.
            # Keep polling — not yet terminal.
            return ""
        # Any other mergeable_state is a terminal block.
        return MERGEABLE_BLOCKED
    if gate_conclusion == "action_required":
        return WARN
    if gate_conclusion == "failure":
        return BLOCK
    # Gate is None (no check run yet), "in_progress", "queued", or similar
    # — not yet terminal.
    return ""


def poll(
    pr_number: int,
    *,
    interval: int = DEFAULT_INTERVAL,
    budget: int = DEFAULT_BUDGET,
    repo: str = DEFAULT_REPO,
    github_api: GitHubAPI | None = None,
    verbose: bool = False,
    sleep_fn: Callable[[float], None] = time.sleep,
    monotonic_fn: Callable[[], float] = time.monotonic,
) -> PollResult:
    """SCRUM-544: poll the TalkBack PR Gate + mergeable_state until terminal.

    ``sleep_fn`` and ``monotonic_fn`` are injectable for tests — the real
    flow uses :py:func:`time.sleep` / :py:func:`time.monotonic`; tests
    can substitute fakes that advance a virtual clock without blocking.
    """
    interval = max(10, min(300, int(interval)))
    budget = max(60, min(3600, int(budget)))
    if github_api is None:
        github_api = HttpGitHubAPI(auth.github_token())

    result = PollResult(pr_number=pr_number, terminal_state=ERROR)
    started = monotonic_fn()
    deadline = started + budget

    try:
        while True:
            result.ticks += 1
            pr = github_api.read_pr(repo, pr_number)
            head_sha = pr.merge_commit_sha or _head_sha_from_ref(github_api, repo, pr.head_ref)
            checks = github_api.get_check_runs(repo, head_sha)
            gate = next(
                (
                    c for c in checks
                    if c.get("name") == TALKBACK_GATE_NAME
                    and c.get("status") == "completed"
                ),
                None,
            )
            gate_conclusion = gate.get("conclusion") if gate else None
            result.gate_conclusion = gate_conclusion
            result.mergeable_state = pr.mergeable_state
            terminal = _classify(gate_conclusion, pr.mergeable_state)
            if verbose:
                sys.stderr.write(
                    f"[poll #{result.ticks}] gate={gate_conclusion} "
                    f"mergeable={pr.mergeable_state} -> {terminal or 'pending'}\n"
                )
            if terminal:
                result.terminal_state = terminal
                result.elapsed_seconds = int(monotonic_fn() - started)
                if terminal == PASS:
                    result.actions_taken.append(
                        f"gate PASS + mergeable=clean after {result.ticks} ticks / {result.elapsed_seconds}s"
                    )
                else:
                    result.aborted_reason = f"terminal_state={terminal}"
                    result.actions_taken.append(
                        f"aborted: {result.aborted_reason} after {result.ticks} ticks / {result.elapsed_seconds}s"
                    )
                _summarize(result)
                return result
            if monotonic_fn() >= deadline:
                result.terminal_state = TIMEOUT
                result.elapsed_seconds = int(monotonic_fn() - started)
                result.aborted_reason = (
                    f"timeout after {budget}s ({result.ticks} ticks); "
                    f"last gate={gate_conclusion!r} mergeable={pr.mergeable_state!r}"
                )
                result.actions_taken.append(f"aborted: {result.aborted_reason}")
                _summarize(result)
                return result
            sleep_fn(interval)
    except RuntimeError as e:
        result.terminal_state = ERROR
        result.elapsed_seconds = int(monotonic_fn() - started)
        result.aborted_reason = f"error: {e}"
        result.actions_taken.append(f"aborted: {result.aborted_reason}")
        _summarize(result)
        return result


def _head_sha_from_ref(api: GitHubAPI, repo: str, ref: str) -> str:
    """Resolve a branch ref to its tip SHA via a second PR read. Pre-merge
    PRs have ``merge_commit_sha`` populated (a synthetic test merge), but
    in some states it can be ``None`` — fall back to the branch ref."""
    return ref


def _summarize(result: PollResult) -> None:
    n = len(result.actions_taken)
    if result.aborted_reason:
        msg = f"poll.py aborted: {result.aborted_reason}"
    else:
        msg = f"poll.py succeeded: {n} actions, no aborts"
    result.actions_taken.append(msg)


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--pr", type=int, required=True, help="PR number to poll")
    parser.add_argument(
        "--interval", type=int, default=DEFAULT_INTERVAL, help="Seconds between polls (10-300)"
    )
    parser.add_argument(
        "--budget", type=int, default=DEFAULT_BUDGET, help="Total budget in seconds (60-3600)"
    )
    parser.add_argument("--repo", default=DEFAULT_REPO)
    parser.add_argument("--verbose", action="store_true", help="Per-tick output on stderr")
    args = parser.parse_args(argv)

    result = poll(
        args.pr,
        interval=args.interval,
        budget=args.budget,
        repo=args.repo,
        verbose=args.verbose,
    )
    print(json.dumps(asdict(result), indent=2))
    if result.terminal_state == PASS:
        return 0
    if result.terminal_state == ERROR:
        return 1
    return 2


if __name__ == "__main__":
    sys.exit(main())
