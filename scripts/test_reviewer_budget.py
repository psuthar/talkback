#!/usr/bin/env python3
"""Unit + integration tests for scripts/reviewer/budget.py (SCRUM-511)."""

from __future__ import annotations

import json
import multiprocessing
import os
import sys
import tempfile
import unittest
from datetime import datetime, timedelta, timezone
from pathlib import Path

_REPO_ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(_REPO_ROOT / "scripts"))

from reviewer.budget import (  # noqa: E402
    BUDGET_EXHAUSTED_COMMENT,
    ConsumeResult,
    InMemoryBudgetStore,
    SQLiteBudgetStore,
    _today_key,
    check_and_increment,
)


class InMemoryStoreTest(unittest.TestCase):
    def test_first_call_under_cap_allowed(self):
        store = InMemoryBudgetStore()
        r = check_and_increment(store, 1000, cap=10_000, audit_log=None)
        self.assertEqual(r, ConsumeResult(True, 9_000, 1_000))

    def test_accumulates_across_calls(self):
        store = InMemoryBudgetStore()
        check_and_increment(store, 3_000, cap=10_000, audit_log=None)
        r = check_and_increment(store, 4_000, cap=10_000, audit_log=None)
        self.assertEqual(r, ConsumeResult(True, 3_000, 7_000))

    def test_exact_boundary_allowed(self):
        store = InMemoryBudgetStore()
        check_and_increment(store, 9_000, cap=10_000, audit_log=None)
        r = check_and_increment(store, 1_000, cap=10_000, audit_log=None)
        self.assertTrue(r.allowed)
        self.assertEqual(r.remaining, 0)

    def test_over_cap_denied_and_counter_unchanged(self):
        store = InMemoryBudgetStore()
        check_and_increment(store, 9_500, cap=10_000, audit_log=None)
        r = check_and_increment(store, 1_000, cap=10_000, audit_log=None)
        self.assertFalse(r.allowed)
        self.assertEqual(r.remaining, 500)
        # Counter must NOT have been incremented on denial.
        self.assertEqual(store.snapshot(_today_key()), 9_500)

    def test_day_rollover_resets(self):
        store = InMemoryBudgetStore()
        yesterday = datetime.now(timezone.utc) - timedelta(days=1)
        today = datetime.now(timezone.utc)
        check_and_increment(store, 9_000, cap=10_000, now=yesterday, audit_log=None)
        r = check_and_increment(store, 8_000, cap=10_000, now=today, audit_log=None)
        self.assertTrue(r.allowed)
        self.assertEqual(r.used, 8_000)

    def test_env_var_drives_default_cap(self):
        os.environ["REVIEWER_DAILY_TOKEN_CAP"] = "5000"
        try:
            store = InMemoryBudgetStore()
            r = check_and_increment(store, 4_500, audit_log=None)
            self.assertTrue(r.allowed)
            r2 = check_and_increment(store, 1_000, audit_log=None)
            self.assertFalse(r2.allowed)
        finally:
            os.environ.pop("REVIEWER_DAILY_TOKEN_CAP", None)

    def test_audit_log_records_every_call(self):
        store = InMemoryBudgetStore()
        with tempfile.TemporaryDirectory() as tmp:
            log = Path(tmp) / "budget.log"
            check_and_increment(store, 100, cap=1_000, audit_log=log, pr_number=42)
            check_and_increment(store, 1_500, cap=1_000, audit_log=log, pr_number=43)
            rows = [json.loads(line) for line in log.read_text().splitlines()]
            self.assertEqual(len(rows), 2)
            self.assertTrue(rows[0]["allowed"])
            self.assertFalse(rows[1]["allowed"])
            self.assertEqual(rows[0]["pr_number"], 42)
            self.assertEqual(rows[1]["remaining"], 900)

    def test_exhausted_comment_template_is_stable(self):
        # Other tools render this string verbatim; pinning it is the contract.
        self.assertIn("daily budget exhausted", BUDGET_EXHAUSTED_COMMENT)
        self.assertIn("/talkback-review", BUDGET_EXHAUSTED_COMMENT)
        self.assertIn("00:00 UTC", BUDGET_EXHAUSTED_COMMENT)


def _worker_consume(args):
    db_path, amount, cap, ttl = args
    store = SQLiteBudgetStore(db_path)
    try:
        return store.try_consume(_today_key(), amount, cap, ttl).allowed
    finally:
        store.close()


class SQLiteCrossProcessTest(unittest.TestCase):
    """Atomicity test: many processes hammering the same DB must NEVER let the
    sum of allowed-amounts exceed the cap. This catches lock-region bugs that
    pure-thread tests miss because of the GIL.
    """

    def test_concurrent_processes_respect_cap(self):
        with tempfile.TemporaryDirectory() as tmp:
            db_path = str(Path(tmp) / "budget.sqlite")
            # Seed the schema by opening once.
            SQLiteBudgetStore(db_path).close()

            cap = 10_000
            ttl = 3600
            amount = 1_000
            n_workers = 20  # would overshoot by 10_000 if non-atomic

            with multiprocessing.Pool(processes=8) as pool:
                results = pool.map(
                    _worker_consume,
                    [(db_path, amount, cap, ttl)] * n_workers,
                )

            allowed_count = sum(1 for r in results if r)
            self.assertEqual(
                allowed_count,
                10,
                f"Expected exactly 10 allows (cap={cap}, amount={amount}); "
                f"got {allowed_count}. Atomicity broken.",
            )

            final = SQLiteBudgetStore(db_path)
            try:
                self.assertEqual(final.snapshot(_today_key()), cap)
            finally:
                final.close()


if __name__ == "__main__":
    unittest.main()
