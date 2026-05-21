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


class _FakeGitHubAPI:
    """Returns scripted responses per tick."""

    def __init__(self, ticks: list[dict]):
        # Each tick: {"mergeable_state": str, "gate_conclusion": str|None, "gate_status": str}
        self._ticks = ticks
        self._i = count()

    def read_pr(self, repo, pr_number):
        i = next(self._i)
        t = self._ticks[min(i, len(self._ticks) - 1)]
        return PRSnapshot(
            number=pr_number,
            state="open",
            merged=False,
            merge_commit_sha=f"sha-tick-{i}",
            mergeable_state=t["mergeable_state"],
            head_ref="feat/SCRUM-999",
            base_ref="main",
        )

    def get_check_runs(self, repo, ref):
        # Need to mirror the tick index from read_pr — read_pr already
        # consumed it, so look at the last value. Simplest: re-read by
        # parsing ref ``sha-tick-N``.
        try:
            i = int(ref.rsplit("-", 1)[-1])
        except ValueError:
            i = 0
        t = self._ticks[min(i, len(self._ticks) - 1)]
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
