#!/usr/bin/env python3
"""SCRUM-528: credential helpers for the FULL_AUTO orchestration scripts.

The FULL_AUTO scripts run in three places — each with different credential
delivery mechanisms but the same code interface:

| Where                       | GitHub creds from        | Jira creds from              |
|-----------------------------|--------------------------|------------------------------|
| Local laptop (default)      | ``gh auth token``        | ``.env.local`` (gitignored)  |
| Webhook listener (future)   | ``GITHUB_TOKEN`` env     | env vars sourced from disk   |
| CI scheduled run (future)   | ``secrets.GITHUB_TOKEN`` | repo secrets injected as env |

All three call the same ``github_token()`` / ``jira_auth()`` helpers below.
The only difference is how the environment variables are populated.

Phase 0 ships *only* this auth module + the ``.env.local.example`` template
+ a runbook. Subsequent phases (close.py, lib/jira.py, lib/github.py) build
on this foundation. See ``docs/agent/full-auto-scripts.md``.
"""

from __future__ import annotations

import os
import subprocess
from functools import lru_cache
from pathlib import Path
from typing import MutableMapping

DEFAULT_ATLASSIAN_BASE_URL = "https://suthar-team.atlassian.net"

GITHUB_TOKEN_ERROR = (
    "No GitHub credentials available.\n"
    "  Local: run `gh auth login`.\n"
    "  CI / webhook: set the GITHUB_TOKEN env var."
)

JIRA_AUTH_ERROR = (
    "Missing Jira credentials.\n"
    "  Set ATLASSIAN_EMAIL and ATLASSIAN_API_TOKEN.\n"
    "  Local: add them to .env.local (gitignored) and source the file before running\n"
    "         (or use direnv).\n"
    "  CI: configure ATLASSIAN_API_TOKEN as a repo secret.\n"
    "  Generate a scoped token at https://id.atlassian.com/manage-profile/security/api-tokens"
)


def _load_dotenv_local(
    start: Path | None = None,
    target_env: MutableMapping[str, str] | None = None,
) -> None:
    """SCRUM-533: populate ``os.environ`` from ``.env.local`` at repo root.

    Walks up from ``start`` (default: this file's directory) looking for a
    ``.git`` directory. If found and a sibling ``.env.local`` exists, parses
    its ``KEY=VALUE`` lines and applies them via :py:meth:`setdefault` so
    existing environment variables always win. Shell-sourced values, CI repo
    secrets, and webhook env injection therefore keep precedence — the file
    is a zero-friction local fallback only. Missing file, malformed lines,
    or filesystem errors are silent no-ops; the credential helpers below
    raise their own actionable errors when creds are truly absent.
    """
    env = os.environ if target_env is None else target_env
    cur = (start or Path(__file__).resolve().parent)
    while True:
        if (cur / ".git").exists():
            break
        if cur.parent == cur:
            return
        cur = cur.parent
    dotenv = cur / ".env.local"
    if not dotenv.is_file():
        return
    try:
        contents = dotenv.read_text()
    except OSError:
        return
    for line in contents.splitlines():
        stripped = line.strip()
        if not stripped or stripped.startswith("#"):
            continue
        if "=" not in stripped:
            continue
        key, _, value = stripped.partition("=")
        key = key.strip()
        if not key:
            continue
        value = value.strip()
        if len(value) >= 2 and value[0] == value[-1] and value[0] in ("'", '"'):
            value = value[1:-1]
        env.setdefault(key, value)


_load_dotenv_local()


@lru_cache(maxsize=1)
def github_token() -> str:
    """Resolve a GitHub token in priority order.

    1. ``GITHUB_TOKEN`` env var (CI, webhook listener, or explicit override).
    2. ``gh auth token`` (the local user is already logged in via ``gh auth login``).

    Raises ``RuntimeError`` with an actionable message if neither path produces
    a usable token. Memoised so repeated calls within a single script invocation
    don't reshell ``gh``.
    """
    token = os.environ.get("GITHUB_TOKEN")
    if token:
        return token
    result = subprocess.run(
        ["gh", "auth", "token"],
        capture_output=True,
        text=True,
        check=False,
    )
    if result.returncode == 0:
        token = result.stdout.strip()
        if token:
            return token
    raise RuntimeError(GITHUB_TOKEN_ERROR)


@lru_cache(maxsize=1)
def jira_auth() -> tuple[str, str]:
    """Resolve Jira (Atlassian) Basic-auth credentials.

    Reads ``ATLASSIAN_EMAIL`` and ``ATLASSIAN_API_TOKEN`` from the environment.
    No fallback — there is no equivalent of ``gh auth token`` for Atlassian,
    so the caller must populate both env vars (locally via ``.env.local``,
    in CI via repo secrets).

    Raises ``RuntimeError`` with a pointer to the Atlassian token-creation
    page if either variable is missing or empty.
    """
    email = os.environ.get("ATLASSIAN_EMAIL", "").strip()
    token = os.environ.get("ATLASSIAN_API_TOKEN", "").strip()
    if not email or not token:
        raise RuntimeError(JIRA_AUTH_ERROR)
    return (email, token)


def atlassian_base_url() -> str:
    """Base URL for the Atlassian instance.

    Defaults to TalkBack's tenant. Override via ``ATLASSIAN_BASE_URL`` env var
    for testing or multi-tenant scenarios. Not memoised because tests may
    monkeypatch the env between calls.
    """
    return os.environ.get("ATLASSIAN_BASE_URL", DEFAULT_ATLASSIAN_BASE_URL).rstrip("/")


__all__ = [
    "DEFAULT_ATLASSIAN_BASE_URL",
    "GITHUB_TOKEN_ERROR",
    "JIRA_AUTH_ERROR",
    "_load_dotenv_local",
    "atlassian_base_url",
    "github_token",
    "jira_auth",
]
