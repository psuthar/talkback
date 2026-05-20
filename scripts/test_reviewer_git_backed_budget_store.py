#!/usr/bin/env python3
"""Unit tests for scripts/reviewer/git_backed_budget_store.py (SCRUM-515)."""

from __future__ import annotations

import base64
import json
import sys
import unittest
from datetime import datetime, timezone
from pathlib import Path

_REPO_ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(_REPO_ROOT / "scripts"))

from reviewer.git_backed_budget_store import (  # noqa: E402
    CASConflict,
    GitBackedBudgetStore,
    _FileSnapshot,
)


class FakeContentsHTTP:
    def __init__(self, initial_state: dict | None = None):
        self._sha = "sha0"
        self._state = initial_state or {}
        self._cas_misses_remaining = 0
        self.put_calls: list[tuple[str, dict]] = []

    def get_file(self, repo, branch, path):
        return _FileSnapshot(sha=self._sha, state=dict(self._state))

    def put_file(self, repo, branch, path, sha, content_b64, message):
        if sha != self._sha:
            raise CASConflict(f"sha mismatch: expected {self._sha}, got {sha}")
        if self._cas_misses_remaining > 0:
            # Simulate a concurrent writer winning the race.
            self._cas_misses_remaining -= 1
            self._sha = f"sha-mid-{self._cas_misses_remaining}"
            raise CASConflict("simulated concurrent writer")
        decoded = json.loads(base64.b64decode(content_b64))
        self._state = decoded
        new_sha = f"sha-after-{len(self.put_calls)}"
        self.put_calls.append((message, decoded))
        self._sha = new_sha
        return new_sha


class GitBackedBudgetStoreTest(unittest.TestCase):
    def test_first_call_consumes(self):
        http = FakeContentsHTTP()
        s = GitBackedBudgetStore(http, "owner/repo")
        r = s.try_consume("k1", 100, 1_000, 3600)
        self.assertTrue(r.allowed)
        self.assertEqual(r.remaining, 900)
        self.assertEqual(len(http.put_calls), 1)
        msg, payload = http.put_calls[0]
        self.assertIn("k1 += 100", msg)
        self.assertEqual(payload["k1"]["value"], 100)

    def test_over_cap_does_not_write(self):
        http = FakeContentsHTTP(initial_state={
            "k1": {"value": 950, "expires_at": _future()}
        })
        s = GitBackedBudgetStore(http, "owner/repo")
        r = s.try_consume("k1", 100, 1_000, 3600)
        self.assertFalse(r.allowed)
        self.assertEqual(r.remaining, 50)
        self.assertEqual(http.put_calls, [])

    def test_expired_entry_resets(self):
        http = FakeContentsHTTP(initial_state={
            "k1": {"value": 1_000_000, "expires_at": _past()}
        })
        s = GitBackedBudgetStore(http, "owner/repo")
        r = s.try_consume("k1", 100, 1_000, 3600)
        self.assertTrue(r.allowed)
        self.assertEqual(r.used, 100)

    def test_cas_conflict_retries(self):
        http = FakeContentsHTTP()
        http._cas_misses_remaining = 2  # Two concurrent-writer races, then we win
        s = GitBackedBudgetStore(http, "owner/repo")
        r = s.try_consume("k1", 100, 1_000, 3600)
        self.assertTrue(r.allowed)
        self.assertEqual(len(http.put_calls), 1)

    def test_cas_exhaustion_raises(self):
        http = FakeContentsHTTP()
        http._cas_misses_remaining = 99  # never converges within MAX_CAS_RETRIES
        s = GitBackedBudgetStore(http, "owner/repo")
        with self.assertRaises(RuntimeError):
            s.try_consume("k1", 100, 1_000, 3600)

    def test_snapshot_returns_current(self):
        http = FakeContentsHTTP(initial_state={
            "k1": {"value": 250, "expires_at": _future()}
        })
        s = GitBackedBudgetStore(http, "owner/repo")
        self.assertEqual(s.snapshot("k1"), 250)

    def test_snapshot_expired_returns_zero(self):
        http = FakeContentsHTTP(initial_state={
            "k1": {"value": 250, "expires_at": _past()}
        })
        s = GitBackedBudgetStore(http, "owner/repo")
        self.assertEqual(s.snapshot("k1"), 0)


def _future() -> int:
    return int(datetime.now(timezone.utc).timestamp()) + 3600


def _past() -> int:
    return int(datetime.now(timezone.utc).timestamp()) - 3600


if __name__ == "__main__":
    unittest.main()
