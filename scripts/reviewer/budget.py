#!/usr/bin/env python3
"""SCRUM-511: daily token-budget cap for the talkback-reviewer agent.

Cost runaway is the most dangerous failure mode for the reviewer Epic
(SCRUM-508) — invisible until the monthly bill arrives. This module
enforces a daily token ceiling. Callers ask ``try_consume(estimated)``
before any model invocation; if the ceiling would be exceeded, the
reviewer posts a one-line note and skips the call.

Design (Phase 0c — pure logic, deployment backend is Phase 1):
* ``BudgetStore`` Protocol — three methods: ``try_consume``, ``snapshot``,
  ``close``. Two concrete implementations:
  - ``InMemoryBudgetStore``: process-local dict + lock. For unit tests.
  - ``SQLiteBudgetStore``: file-backed, ``BEGIN IMMEDIATE`` transaction
    for atomic check-and-increment across processes. Used by the
    cross-process atomicity integration test; also a viable production
    backend on a host with a persistent volume.
* ``check_and_increment`` — the high-level helper. Reads the daily cap
  from ``REVIEWER_DAILY_TOKEN_CAP`` (default 500000), composes the
  date-keyed counter key (``reviewer-budget:YYYY-MM-DD``), calls the
  store's atomic primitive, and emits a JSONL audit row.

Production backend choice (Render KV vs Postgres vs file) is intentionally
deferred to Phase 1 when the reviewer workflow is wired — the deployment
topology drives that choice. The ``BudgetStore`` Protocol means swapping
implementations later is a single file change.

Env vars:
  REVIEWER_DAILY_TOKEN_CAP   integer; default 500000
  REVIEWER_BUDGET_TTL_HOURS  integer; default 48 (yesterday keys for retro)

Audit log: ``ops/define-kpis/reviewer-budget.log`` (JSONL, append-only).
"""

from __future__ import annotations

import json
import os
import sqlite3
import threading
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path
from typing import Protocol

_REPO_ROOT = Path(__file__).resolve().parents[2]
DEFAULT_AUDIT_LOG = _REPO_ROOT / "ops" / "define-kpis" / "reviewer-budget.log"

DEFAULT_DAILY_CAP = 500_000
DEFAULT_TTL_HOURS = 48
KEY_PREFIX = "reviewer-budget"

BUDGET_EXHAUSTED_COMMENT = (
    "reviewer daily budget exhausted — request manually with "
    "`/talkback-review` after 00:00 UTC."
)


@dataclass
class ConsumeResult:
    allowed: bool
    remaining: int
    used: int


class BudgetStore(Protocol):
    def try_consume(self, key: str, amount: int, cap: int, ttl_seconds: int) -> ConsumeResult: ...
    def snapshot(self, key: str) -> int: ...
    def close(self) -> None: ...


class InMemoryBudgetStore:
    """Process-local store. Test-only — does not survive restarts."""

    def __init__(self) -> None:
        self._counters: dict[str, int] = {}
        self._lock = threading.Lock()

    def try_consume(self, key: str, amount: int, cap: int, ttl_seconds: int) -> ConsumeResult:
        with self._lock:
            current = self._counters.get(key, 0)
            if current + amount > cap:
                return ConsumeResult(False, max(0, cap - current), current)
            self._counters[key] = current + amount
            return ConsumeResult(True, cap - self._counters[key], self._counters[key])

    def snapshot(self, key: str) -> int:
        with self._lock:
            return self._counters.get(key, 0)

    def close(self) -> None:
        pass


class SQLiteBudgetStore:
    """File-backed atomic store. ``BEGIN IMMEDIATE`` guarantees cross-process
    serialisation: two processes attempting concurrent ``try_consume`` are
    ordered by the OS file lock, with the loser seeing the winner's value.
    """

    _SCHEMA = """
        CREATE TABLE IF NOT EXISTS budget (
            key        TEXT PRIMARY KEY,
            value      INTEGER NOT NULL DEFAULT 0,
            expires_at INTEGER NOT NULL
        );
    """

    def __init__(self, db_path: str | Path) -> None:
        self._path = str(db_path)
        # check_same_thread=False because the file lock is the real serialiser.
        self._conn = sqlite3.connect(self._path, isolation_level=None, check_same_thread=False)
        self._conn.execute("PRAGMA journal_mode=WAL")
        self._conn.execute(self._SCHEMA)

    def _expired(self, expires_at: int) -> bool:
        return expires_at <= int(datetime.now(timezone.utc).timestamp())

    def try_consume(self, key: str, amount: int, cap: int, ttl_seconds: int) -> ConsumeResult:
        now = int(datetime.now(timezone.utc).timestamp())
        cur = self._conn.cursor()
        cur.execute("BEGIN IMMEDIATE")
        try:
            row = cur.execute(
                "SELECT value, expires_at FROM budget WHERE key = ?", (key,)
            ).fetchone()
            current = 0 if row is None or self._expired(row[1]) else int(row[0])
            if current + amount > cap:
                cur.execute("COMMIT")
                return ConsumeResult(False, max(0, cap - current), current)
            new_value = current + amount
            cur.execute(
                "INSERT INTO budget(key, value, expires_at) VALUES(?, ?, ?) "
                "ON CONFLICT(key) DO UPDATE SET value = excluded.value, "
                "expires_at = excluded.expires_at",
                (key, new_value, now + ttl_seconds),
            )
            cur.execute("COMMIT")
            return ConsumeResult(True, cap - new_value, new_value)
        except Exception:
            cur.execute("ROLLBACK")
            raise

    def snapshot(self, key: str) -> int:
        row = self._conn.execute(
            "SELECT value, expires_at FROM budget WHERE key = ?", (key,)
        ).fetchone()
        if row is None or self._expired(row[1]):
            return 0
        return int(row[0])

    def close(self) -> None:
        self._conn.close()


def _today_key(now: datetime | None = None) -> str:
    now = now or datetime.now(timezone.utc)
    return f"{KEY_PREFIX}:{now.strftime('%Y-%m-%d')}"


def _cap_from_env() -> int:
    return int(os.environ.get("REVIEWER_DAILY_TOKEN_CAP", DEFAULT_DAILY_CAP))


def _ttl_seconds_from_env() -> int:
    return int(os.environ.get("REVIEWER_BUDGET_TTL_HOURS", DEFAULT_TTL_HOURS)) * 3600


def _audit(row: dict, log_path: Path | None) -> None:
    if log_path is None:
        return
    log_path.parent.mkdir(parents=True, exist_ok=True)
    with log_path.open("a") as f:
        f.write(json.dumps(row) + "\n")


def check_and_increment(
    store: BudgetStore,
    estimated_tokens: int,
    *,
    pr_number: int | None = None,
    cap: int | None = None,
    ttl_seconds: int | None = None,
    now: datetime | None = None,
    audit_log: Path | None = DEFAULT_AUDIT_LOG,
) -> ConsumeResult:
    """Enforce the daily budget for ``estimated_tokens``.

    Returns ``ConsumeResult(allowed, remaining, used)``. When
    ``allowed`` is False, the caller MUST post ``BUDGET_EXHAUSTED_COMMENT``
    on the PR (or equivalent) instead of running the model.

    Every call (allowed or denied) writes a JSONL row to ``audit_log``.
    Pass ``audit_log=None`` to disable (tests).
    """
    cap = cap if cap is not None else _cap_from_env()
    ttl_seconds = ttl_seconds if ttl_seconds is not None else _ttl_seconds_from_env()
    key = _today_key(now)
    result = store.try_consume(key, estimated_tokens, cap, ttl_seconds)
    _audit(
        {
            "ts": (now or datetime.now(timezone.utc)).isoformat(),
            "key": key,
            "estimated_tokens": estimated_tokens,
            "allowed": result.allowed,
            "remaining": result.remaining,
            "used": result.used,
            "cap": cap,
            "pr_number": pr_number,
        },
        audit_log,
    )
    return result


__all__ = [
    "BudgetStore",
    "BUDGET_EXHAUSTED_COMMENT",
    "ConsumeResult",
    "DEFAULT_AUDIT_LOG",
    "DEFAULT_DAILY_CAP",
    "DEFAULT_TTL_HOURS",
    "InMemoryBudgetStore",
    "SQLiteBudgetStore",
    "check_and_increment",
]
