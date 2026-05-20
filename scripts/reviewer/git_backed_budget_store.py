#!/usr/bin/env python3
"""SCRUM-515: git-backed BudgetStore via the GitHub Contents API.

Stores the daily budget counter as a JSON blob on a tracking branch
(`reviewer-state` by default). Atomic check-and-increment is achieved
via the Contents API's SHA-based optimistic locking: the PUT request
must include the file's current blob SHA, and the API returns 409 if
the SHA has moved since the read.

Why this and not a local KV: GitHub Actions runners are ephemeral; a
file on the runner FS resets every workflow run, so the daily cap could
be circumvented by simply running again. The tracking-branch approach
survives across runs without any new infrastructure.

Why not a GitHub repo variable: repo vars are not atomic — two
concurrent setters race. Tracking-branch SHA-CAS is the cheapest atomic
primitive available to a GitHub Actions workflow.

See ``.github/talkback-reviewer/BACKEND.md`` for backend choice
rationale + bootstrap steps.
"""

from __future__ import annotations

import base64
import json
from dataclasses import dataclass
from datetime import datetime, timezone
from typing import Protocol

from reviewer.budget import ConsumeResult


STATE_FILE = "state.json"
DEFAULT_BRANCH = "reviewer-state"
MAX_CAS_RETRIES = 4


class CASConflict(RuntimeError):
    """The remote SHA moved between read and write — caller should retry."""


@dataclass
class _FileSnapshot:
    sha: str
    state: dict


class ContentsHTTP(Protocol):
    """Minimal HTTP seam over the GitHub Contents API.

    Tests inject a mock implementation; production wires a urllib-based
    impl that talks to api.github.com with the workflow's GITHUB_TOKEN.
    """

    def get_file(self, repo: str, branch: str, path: str) -> _FileSnapshot: ...
    def put_file(
        self, repo: str, branch: str, path: str, sha: str, content_b64: str, message: str
    ) -> str: ...


class GitBackedBudgetStore:
    """``BudgetStore`` impl backed by ``state.json`` on a tracking branch."""

    def __init__(
        self,
        http: ContentsHTTP,
        repo: str,
        branch: str = DEFAULT_BRANCH,
        path: str = STATE_FILE,
    ):
        self._http = http
        self._repo = repo
        self._branch = branch
        self._path = path

    def _read(self) -> _FileSnapshot:
        return self._http.get_file(self._repo, self._branch, self._path)

    def _write(self, sha: str, state: dict, *, message: str) -> str:
        encoded = base64.b64encode(
            json.dumps(state, indent=2, sort_keys=True).encode("utf-8")
        ).decode("ascii")
        return self._http.put_file(
            self._repo, self._branch, self._path, sha, encoded, message
        )

    def try_consume(
        self, key: str, amount: int, cap: int, ttl_seconds: int
    ) -> ConsumeResult:
        now = int(datetime.now(timezone.utc).timestamp())
        last_error: Exception | None = None
        for _ in range(MAX_CAS_RETRIES):
            snap = self._read()
            entry = snap.state.get(key) or {}
            expired = (
                not entry or entry.get("expires_at", 0) <= now
            )
            current = 0 if expired else int(entry.get("value", 0))

            if current + amount > cap:
                return ConsumeResult(False, max(0, cap - current), current)

            new_value = current + amount
            new_state = dict(snap.state)
            new_state[key] = {"value": new_value, "expires_at": now + ttl_seconds}
            try:
                self._write(
                    snap.sha,
                    new_state,
                    message=f"reviewer-budget: {key} += {amount} (now {new_value}/{cap})",
                )
                return ConsumeResult(True, cap - new_value, new_value)
            except CASConflict as exc:
                last_error = exc
                continue
        raise RuntimeError(
            f"Budget CAS exhausted after {MAX_CAS_RETRIES} retries; last error: {last_error}"
        )

    def snapshot(self, key: str) -> int:
        snap = self._read()
        entry = snap.state.get(key) or {}
        now = int(datetime.now(timezone.utc).timestamp())
        if not entry or entry.get("expires_at", 0) <= now:
            return 0
        return int(entry.get("value", 0))

    def close(self) -> None:
        pass


__all__ = [
    "CASConflict",
    "ContentsHTTP",
    "DEFAULT_BRANCH",
    "GitBackedBudgetStore",
    "STATE_FILE",
    "_FileSnapshot",
]
