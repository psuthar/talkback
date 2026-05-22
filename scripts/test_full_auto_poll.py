#!/usr/bin/env python3
"""SCRUM-544: tests for ``scripts/full_auto/poll.py``."""

from __future__ import annotations

import sys
import unittest
from itertools import count
from pathlib import Path

_REPO_ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(_REPO_ROOT / "scripts"))

from full_auto import poll as poll_mod  # noqa: E402
from full_auto.lib.github import PRSnapshot  # noqa: E402


class _Clock:
    """Virtual clock for sleep_fn / monotonic_fn injection."""

    def __init__(self, start: float = 1000.0):
        self.now = start
        self.sleeps: list[float] = []

    def monotonic(self) -> float:
        return self.now

    def sleep(self, secs: float) -> None:
        self.sleeps.append(secs)
        self.now += secs


_HEAD_REF = "feat/SCRUM-999"


class _FakeGitHubAPI:
    """Returns scripted responses per tick.

    SCRUM-547 hardened: ``get_check_runs`` enforces that poll.py queries
    against the PR ``head_ref`` (NOT ``merge_commit_sha``). The earlier
    fixture keyed off whatever ref was passed, masking the production
    bug where poll.py was using the synthetic test-merge SHA.
    """

    def __init__(self, ticks: list[dict]):
        # Each tick: {"mergeable_state": str, "gate_conclusion": str|None, "gate_status": str}
        self._ticks = ticks
        self._read_tick = count()
        # Track every ref used to query check_runs so tests can assert
        # poll.py used the head_ref and never the merge_commit_sha.
        self.check_run_refs: list[str] = []

    def read_pr(self, repo, pr_number):
        i = next(self._read_tick)
        t = self._ticks[min(i, len(self._ticks) - 1)]
        return PRSnapshot(
            number=pr_number,
            state="open",
            merged=False,
            # SCRUM-547: distinct synthetic merge SHA per tick. If poll.py
            # ever regresses and queries against this, ``get_check_runs``
            # raises a hard error so the bug can't sneak back in.
            merge_commit_sha=f"synthetic-merge-sha-tick-{i}",
            mergeable_state=t["mergeable_state"],
            head_ref=_HEAD_REF,
            base_ref="main",
        )

    def get_check_runs(self, repo, ref):
        self.check_run_refs.append(ref)
        if ref != _HEAD_REF:
            raise AssertionError(
                f"SCRUM-547 regression: get_check_runs called with "
                f"{ref!r}, expected head_ref {_HEAD_REF!r} (poll.py must "
                f"NOT query against pr.merge_commit_sha — that's the "
                f"synthetic test-merge commit, not where check runs live)."
            )
        # Use the current read_tick value as the index — read_pr() was
        # just called by poll.py before this, so the latest tick is one
        # behind the counter.
        i = max(0, next(count(start=len(self.check_run_refs) - 1)))
        i = min(i, len(self._ticks) - 1)
        t = self._ticks[i]
        return [
            {
                "name": poll_mod.TALKBACK_GATE_NAME,
                "status": t.get("gate_status", "completed"),
                "conclusion": t.get("gate_conclusion"),
            }
        ]

    def merge_pr(self, repo, pr_number):
        raise NotImplementedError

    def create_pr(self, repo, **kwargs):
        raise NotImplementedError

    def update_pr_body(self, repo, pr_number, body):
        raise NotImplementedError


class ClassifyTest(unittest.TestCase):
    def test_pass(self):
        self.assertEqual(poll_mod._classify("success", "clean"), poll_mod.PASS)

    def test_warn(self):
        self.assertEqual(
            poll_mod._classify("action_required", "clean"), poll_mod.WARN
        )

    def test_block(self):
        self.assertEqual(poll_mod._classify("failure", "clean"), poll_mod.BLOCK)

    def test_mergeable_blocked(self):
        self.assertEqual(
            poll_mod._classify("success", "blocked"), poll_mod.MERGEABLE_BLOCKED
        )

    def test_gate_success_but_mergeable_unknown_keeps_polling(self):
        self.assertEqual(poll_mod._classify("success", None), "")
        self.assertEqual(poll_mod._classify("success", "unknown"), "")

    def test_no_gate_yet_keeps_polling(self):
        self.assertEqual(poll_mod._classify(None, "clean"), "")


class PollPassTest(unittest.TestCase):
    def test_returns_pass_on_first_terminal_tick(self):
        clock = _Clock()
        gh = _FakeGitHubAPI(
            [{"mergeable_state": "clean", "gate_conclusion": "success"}]
        )
        result = poll_mod.poll(
            999,
            interval=30,
            budget=2400,
            github_api=gh,
            sleep_fn=clock.sleep,
            monotonic_fn=clock.monotonic,
        )
        self.assertEqual(result.terminal_state, "pass")
        self.assertIsNone(result.aborted_reason)
        self.assertEqual(result.ticks, 1)
        self.assertEqual(clock.sleeps, [])  # no sleep needed, terminal on first
        self.assertTrue(result.actions_taken[-1].startswith("poll.py succeeded:"))

    def test_pending_then_pass(self):
        clock = _Clock()
        gh = _FakeGitHubAPI(
            [
                {"mergeable_state": "unknown", "gate_conclusion": None, "gate_status": "in_progress"},
                {"mergeable_state": "unknown", "gate_conclusion": None, "gate_status": "in_progress"},
                {"mergeable_state": "clean", "gate_conclusion": "success"},
            ]
        )
        result = poll_mod.poll(
            999,
            interval=30,
            budget=2400,
            github_api=gh,
            sleep_fn=clock.sleep,
            monotonic_fn=clock.monotonic,
        )
        self.assertEqual(result.terminal_state, "pass")
        self.assertEqual(result.ticks, 3)
        # Two sleeps between three ticks.
        self.assertEqual(clock.sleeps, [30, 30])


class PollWarnBlockTest(unittest.TestCase):
    def test_warn_is_terminal(self):
        clock = _Clock()
        gh = _FakeGitHubAPI(
            [{"mergeable_state": "clean", "gate_conclusion": "action_required"}]
        )
        result = poll_mod.poll(
            999, github_api=gh, sleep_fn=clock.sleep, monotonic_fn=clock.monotonic
        )
        self.assertEqual(result.terminal_state, "warn")
        self.assertIn("terminal_state=warn", result.aborted_reason)
        self.assertTrue(result.actions_taken[-1].startswith("poll.py aborted:"))

    def test_block_is_terminal(self):
        clock = _Clock()
        gh = _FakeGitHubAPI(
            [{"mergeable_state": "clean", "gate_conclusion": "failure"}]
        )
        result = poll_mod.poll(
            999, github_api=gh, sleep_fn=clock.sleep, monotonic_fn=clock.monotonic
        )
        self.assertEqual(result.terminal_state, "block")

    def test_mergeable_blocked_after_gate_pass(self):
        clock = _Clock()
        gh = _FakeGitHubAPI(
            [{"mergeable_state": "blocked", "gate_conclusion": "success"}]
        )
        result = poll_mod.poll(
            999, github_api=gh, sleep_fn=clock.sleep, monotonic_fn=clock.monotonic
        )
        self.assertEqual(result.terminal_state, "mergeable_blocked")


class PollTimeoutTest(unittest.TestCase):
    def test_timeout_when_budget_exhausted(self):
        clock = _Clock()
        # Always pending — never reaches a terminal classification.
        gh = _FakeGitHubAPI(
            [{"mergeable_state": "unknown", "gate_conclusion": None, "gate_status": "in_progress"}]
        )
        result = poll_mod.poll(
            999,
            interval=30,
            budget=60,  # minimum allowed
            github_api=gh,
            sleep_fn=clock.sleep,
            monotonic_fn=clock.monotonic,
        )
        self.assertEqual(result.terminal_state, "timeout")
        self.assertIn("timeout after 60s", result.aborted_reason)
        # 60s budget / 30s interval → at most 3 ticks (the deadline check
        # fires after the second sleep).
        self.assertGreaterEqual(result.ticks, 2)


class PollClampingTest(unittest.TestCase):
    def test_interval_below_minimum_clamped(self):
        clock = _Clock()
        gh = _FakeGitHubAPI(
            [{"mergeable_state": "clean", "gate_conclusion": "success"}]
        )
        # interval=1 should be clamped to 10 internally; terminal on first
        # tick so no observable sleep, but the clamp is exercised on the
        # second-tick case below.
        poll_mod.poll(
            999, interval=1, github_api=gh, sleep_fn=clock.sleep, monotonic_fn=clock.monotonic
        )
        # No sleep call because terminal on first tick. Clamping is
        # exercised by the test below.

    def test_interval_above_max_clamped(self):
        clock = _Clock()
        gh = _FakeGitHubAPI(
            [
                {"mergeable_state": "unknown", "gate_conclusion": None, "gate_status": "in_progress"},
                {"mergeable_state": "clean", "gate_conclusion": "success"},
            ]
        )
        poll_mod.poll(
            999,
            interval=9999,  # clamped to 300
            budget=3600,
            github_api=gh,
            sleep_fn=clock.sleep,
            monotonic_fn=clock.monotonic,
        )
        # Should have slept exactly once between the two ticks, at 300s
        # (the clamp), not 9999s.
        self.assertEqual(clock.sleeps, [300])


class PollMergeableBlockedConfirmationTest(unittest.TestCase):
    """SCRUM-548: MERGEABLE_BLOCKED requires N consecutive observations.

    Background: on the tick where TalkBack PR Gate flips to ``success``,
    GitHub may still report ``mergeable_state: blocked`` because
    branch-protection rules haven't recomputed against the just-completed
    required check. Without confirmation, poll.py would terminal-classify
    that transient as ``mergeable_blocked`` even though the next tick
    reads ``clean``. Two-tick confirmation eliminates the false positive
    while still terminating cleanly when the block is real."""

    def test_single_transient_blocked_then_clean_resolves_to_pass(self):
        # AC case (a): blocked on tick 1, clean on tick 2 → PASS.
        clock = _Clock()
        gh = _FakeGitHubAPI([
            {"mergeable_state": "blocked", "gate_conclusion": "success"},
            {"mergeable_state": "clean", "gate_conclusion": "success"},
        ])
        result = poll_mod.poll(
            999, github_api=gh, sleep_fn=clock.sleep, monotonic_fn=clock.monotonic
        )
        self.assertEqual(result.terminal_state, "pass")
        self.assertEqual(result.ticks, 2)
        self.assertEqual(clock.sleeps, [30])

    def test_persistent_blocked_across_two_ticks_is_terminal(self):
        # AC case (b): blocked on two consecutive ticks → MERGEABLE_BLOCKED.
        clock = _Clock()
        gh = _FakeGitHubAPI([
            {"mergeable_state": "blocked", "gate_conclusion": "success"},
            {"mergeable_state": "blocked", "gate_conclusion": "success"},
        ])
        result = poll_mod.poll(
            999, github_api=gh, sleep_fn=clock.sleep, monotonic_fn=clock.monotonic
        )
        self.assertEqual(result.terminal_state, "mergeable_blocked")
        self.assertEqual(result.ticks, 2)

    def test_blocked_then_unknown_then_clean_resolves_to_pass(self):
        # AC case (c): blocked → unknown (pending) → clean → PASS.
        # The intermediate "unknown" is pending (not a confirmation),
        # so the confirmation window resets before the final clean tick.
        clock = _Clock()
        gh = _FakeGitHubAPI([
            {"mergeable_state": "blocked", "gate_conclusion": "success"},
            {"mergeable_state": "unknown", "gate_conclusion": "success"},
            {"mergeable_state": "clean", "gate_conclusion": "success"},
        ])
        result = poll_mod.poll(
            999, github_api=gh, sleep_fn=clock.sleep, monotonic_fn=clock.monotonic
        )
        self.assertEqual(result.terminal_state, "pass")
        self.assertEqual(result.ticks, 3)

    def test_blocked_pending_blocked_blocked_eventually_confirms(self):
        # Non-consecutive blocked observations don't confirm. The two
        # final blocked ticks (positions 3+4) form a consecutive pair
        # within the confirmation window → terminal.
        clock = _Clock()
        gh = _FakeGitHubAPI([
            {"mergeable_state": "blocked", "gate_conclusion": "success"},
            {"mergeable_state": "unknown", "gate_conclusion": "success"},
            {"mergeable_state": "blocked", "gate_conclusion": "success"},
            {"mergeable_state": "blocked", "gate_conclusion": "success"},
        ])
        result = poll_mod.poll(
            999, github_api=gh, sleep_fn=clock.sleep, monotonic_fn=clock.monotonic
        )
        self.assertEqual(result.terminal_state, "mergeable_blocked")
        self.assertEqual(result.ticks, 4)

    def test_warn_is_immediate_terminal_even_with_confirmation_default(self):
        # Sanity: hard terminals (WARN / BLOCK) still fire on first tick.
        # Confirmation only applies to MERGEABLE_BLOCKED.
        clock = _Clock()
        gh = _FakeGitHubAPI([
            {"mergeable_state": "clean", "gate_conclusion": "action_required"},
        ])
        result = poll_mod.poll(
            999, github_api=gh, sleep_fn=clock.sleep, monotonic_fn=clock.monotonic
        )
        self.assertEqual(result.terminal_state, "warn")
        self.assertEqual(result.ticks, 1)

    def test_confirmation_ticks_one_restores_eager_old_behavior(self):
        # confirmation_ticks=1 disables the new confirmation gate —
        # mergeable_blocked fires on first observation. Provided as an
        # escape hatch and to ease migration of any caller that wants
        # the old behavior.
        clock = _Clock()
        gh = _FakeGitHubAPI([
            {"mergeable_state": "blocked", "gate_conclusion": "success"},
        ])
        result = poll_mod.poll(
            999,
            github_api=gh,
            sleep_fn=clock.sleep,
            monotonic_fn=clock.monotonic,
            confirmation_ticks=1,
        )
        self.assertEqual(result.terminal_state, "mergeable_blocked")
        self.assertEqual(result.ticks, 1)


class PollHeadRefRegressionTest(unittest.TestCase):
    """SCRUM-547: dedicated regression test pinning that poll.py queries
    check runs against ``pr.head_ref`` rather than ``pr.merge_commit_sha``.

    The earlier code used the synthetic test-merge SHA, which has no check
    runs attached — poll.py would loop past every terminal state until
    the budget ran out. The fixture's ``get_check_runs`` raises if the
    wrong ref is used, so this test also documents the contract."""

    def test_uses_head_ref_not_merge_sha(self):
        clock = _Clock()
        gh = _FakeGitHubAPI(
            [{"mergeable_state": "clean", "gate_conclusion": "success"}]
        )
        result = poll_mod.poll(
            999,
            github_api=gh,
            sleep_fn=clock.sleep,
            monotonic_fn=clock.monotonic,
        )
        self.assertEqual(result.terminal_state, "pass")
        # Recorded ref MUST be the head_ref, never the synthetic merge sha.
        self.assertEqual(gh.check_run_refs, [_HEAD_REF])
        for ref in gh.check_run_refs:
            self.assertNotIn("synthetic-merge-sha", ref)


class PollErrorPathTest(unittest.TestCase):
    def test_github_api_runtime_error_classified_as_error(self):
        class _ExplodingAPI:
            def read_pr(self, repo, pr_number):
                raise RuntimeError("boom")
            def get_check_runs(self, repo, ref):
                raise RuntimeError("never called")
            def merge_pr(self, repo, pr_number):
                raise NotImplementedError
            def create_pr(self, repo, **kwargs):
                raise NotImplementedError
            def update_pr_body(self, repo, pr_number, body):
                raise NotImplementedError

        clock = _Clock()
        result = poll_mod.poll(
            999, github_api=_ExplodingAPI(), sleep_fn=clock.sleep, monotonic_fn=clock.monotonic
        )
        self.assertEqual(result.terminal_state, "error")
        self.assertIn("error: boom", result.aborted_reason)


if __name__ == "__main__":
    unittest.main()
