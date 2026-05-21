#!/usr/bin/env python3
"""SCRUM-530: GitHub REST API wrapper for the FULL_AUTO close-out.

Thin urllib client over the small subset of the GitHub API close.py needs:
read a PR (for ``mergeable_state``), merge a PR (squash). All other PR work
(diff fetch, commenting on a PR for other reasons) is out of scope.

Auth comes from ``lib/auth.github_token()``. Tests inject a ``GitHubAPI``
implementation directly; the real ``HttpGitHubAPI`` only matters at runtime.
"""

from __future__ import annotations

import json
import urllib.error
import urllib.request
from dataclasses import dataclass
from typing import Protocol

GITHUB_API = "https://api.github.com"


@dataclass
class PRSnapshot:
    number: int
    state: str  # "open" | "closed"
    merged: bool
    merge_commit_sha: str | None
    mergeable_state: str  # "clean" | "blocked" | "unknown" | ...
    head_ref: str
    base_ref: str


class GitHubAPI(Protocol):
    def read_pr(self, repo: str, pr_number: int) -> PRSnapshot: ...
    def merge_pr(self, repo: str, pr_number: int) -> str:
        """Squash-merge ``pr_number`` in ``repo``. Returns merge commit SHA."""
        ...


def _request(method: str, url: str, *, token: str, body: dict | None = None) -> tuple[int, dict]:
    req = urllib.request.Request(
        url,
        data=json.dumps(body).encode("utf-8") if body is not None else None,
        method=method,
        headers={
            "Authorization": f"Bearer {token}",
            "Accept": "application/vnd.github+json",
            "X-GitHub-Api-Version": "2022-11-28",
            "Content-Type": "application/json",
        },
    )
    try:
        with urllib.request.urlopen(req) as resp:
            return resp.status, json.loads(resp.read().decode("utf-8") or "{}")
    except urllib.error.HTTPError as e:
        return e.code, json.loads(e.read().decode("utf-8") or "{}")


class HttpGitHubAPI:
    """Production ``GitHubAPI`` implementation. Tests use a fake instead."""

    def __init__(self, token: str):
        self._token = token

    def read_pr(self, repo: str, pr_number: int) -> PRSnapshot:
        status, body = _request(
            "GET", f"{GITHUB_API}/repos/{repo}/pulls/{pr_number}", token=self._token
        )
        if status >= 400:
            raise RuntimeError(f"GET /pulls/{pr_number} -> {status}: {body}")
        return PRSnapshot(
            number=body["number"],
            state=body["state"],
            merged=bool(body.get("merged", False)),
            merge_commit_sha=body.get("merge_commit_sha"),
            mergeable_state=body.get("mergeable_state", "unknown"),
            head_ref=body["head"]["ref"],
            base_ref=body["base"]["ref"],
        )

    def merge_pr(self, repo: str, pr_number: int) -> str:
        status, body = _request(
            "PUT",
            f"{GITHUB_API}/repos/{repo}/pulls/{pr_number}/merge",
            token=self._token,
            body={"merge_method": "squash"},
        )
        if status >= 400:
            raise RuntimeError(f"PUT /pulls/{pr_number}/merge -> {status}: {body}")
        sha = body.get("sha")
        if not sha:
            raise RuntimeError(f"merge response missing sha: {body}")
        return sha


__all__ = ["GITHUB_API", "GitHubAPI", "HttpGitHubAPI", "PRSnapshot"]
