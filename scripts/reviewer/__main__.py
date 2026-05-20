#!/usr/bin/env python3
"""SCRUM-515: CLI entry point for the talkback-reviewer.

Invoked from ``.github/workflows/talkback-reviewer.yml`` as::

    python -m scripts.reviewer --pr <N> --repo owner/repo

Reads secrets/config from env vars:
  GITHUB_TOKEN              workflow token (default: ``GITHUB_TOKEN``)
  ANTHROPIC_API_KEY         Anthropic API key (required)
  REVIEWER_MODEL            model id (default: claude-sonnet-4-6)
  REVIEWER_DAILY_TOKEN_CAP  daily token cap (default 500_000)

All real-network code lives here. The orchestration module
(``scripts/reviewer/run.py``) stays pure-logic + injectable seams.
"""

from __future__ import annotations

import argparse
import base64
import json
import os
import subprocess
import sys
import urllib.error
import urllib.request
from pathlib import Path

_REPO_ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(_REPO_ROOT / "scripts"))

from reviewer.git_backed_budget_store import (  # noqa: E402
    CASConflict,
    DEFAULT_BRANCH,
    GitBackedBudgetStore,
    STATE_FILE,
    _FileSnapshot,
)
from reviewer.run import PRContent, run_review  # noqa: E402


GITHUB_API = "https://api.github.com"
ANTHROPIC_API = "https://api.anthropic.com/v1/messages"
ANTHROPIC_VERSION = "2023-06-01"


def _gh_api(method: str, path: str, *, token: str, body: dict | None = None) -> tuple[int, dict]:
    """Minimal urllib wrapper for the GitHub REST API."""
    req = urllib.request.Request(
        f"{GITHUB_API}{path}",
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


class _GhGitHubClient:
    """``GitHubClient`` impl over the GitHub REST API."""

    def __init__(self, token: str):
        self._token = token

    def fetch_pr(self, repo: str, pr_number: int) -> PRContent:
        _, pr = _gh_api("GET", f"/repos/{repo}/pulls/{pr_number}", token=self._token)
        _, files = _gh_api(
            "GET", f"/repos/{repo}/pulls/{pr_number}/files?per_page=300", token=self._token
        )
        diff_url = pr.get("diff_url", "")
        diff_req = urllib.request.Request(
            diff_url, headers={"Authorization": f"Bearer {self._token}"}
        )
        diff_text = urllib.request.urlopen(diff_req).read().decode("utf-8", errors="replace")
        return PRContent(
            number=pr["number"],
            title=pr["title"],
            description=pr.get("body") or "",
            diff=diff_text,
            changed_files=[f["filename"] for f in files],
            head_sha=pr["head"]["sha"],
        )

    def post_pr_comment(self, repo: str, pr_number: int, body: str) -> int:
        _, resp = _gh_api(
            "POST",
            f"/repos/{repo}/issues/{pr_number}/comments",
            token=self._token,
            body={"body": body},
        )
        return int(resp.get("id", 0))


class _ContentsHTTP:
    """``ContentsHTTP`` impl over the GitHub Contents API."""

    def __init__(self, token: str):
        self._token = token

    def get_file(self, repo: str, branch: str, path: str) -> _FileSnapshot:
        status, resp = _gh_api(
            "GET", f"/repos/{repo}/contents/{path}?ref={branch}", token=self._token
        )
        if status == 404:
            return _FileSnapshot(sha="", state={})
        content = base64.b64decode(resp["content"]).decode("utf-8")
        return _FileSnapshot(sha=resp["sha"], state=json.loads(content) if content.strip() else {})

    def put_file(
        self, repo: str, branch: str, path: str, sha: str, content_b64: str, message: str
    ) -> str:
        body: dict = {"message": message, "content": content_b64, "branch": branch}
        if sha:
            body["sha"] = sha
        status, resp = _gh_api(
            "PUT", f"/repos/{repo}/contents/{path}", token=self._token, body=body
        )
        if status == 409 or (status == 422 and "sha" in json.dumps(resp).lower()):
            raise CASConflict(f"PUT {path} on {branch} returned {status}: {resp}")
        if status >= 400:
            raise RuntimeError(f"PUT {path} failed with {status}: {resp}")
        return resp.get("content", {}).get("sha", "")


def _anthropic_call(api_key: str):
    """Build a model_client callable from an Anthropic API key."""

    def _call(model: str, system_prompt: str, user_message: str) -> str:
        req = urllib.request.Request(
            ANTHROPIC_API,
            data=json.dumps(
                {
                    "model": model,
                    "max_tokens": 1024,
                    "system": system_prompt,
                    "messages": [{"role": "user", "content": user_message}],
                }
            ).encode("utf-8"),
            method="POST",
            headers={
                "x-api-key": api_key,
                "anthropic-version": ANTHROPIC_VERSION,
                "content-type": "application/json",
            },
        )
        with urllib.request.urlopen(req) as resp:
            payload = json.loads(resp.read().decode("utf-8"))
        blocks = payload.get("content", []) or []
        return "".join(b.get("text", "") for b in blocks if b.get("type") == "text")

    return _call


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--pr", type=int, required=True)
    parser.add_argument("--repo", default=os.environ.get("GITHUB_REPOSITORY"))
    parser.add_argument("--source", default="manual", choices=["manual", "auto"])
    parser.add_argument("--state-branch", default=DEFAULT_BRANCH)
    parser.add_argument("--state-path", default=STATE_FILE)
    args = parser.parse_args()

    if not args.repo:
        print("--repo or GITHUB_REPOSITORY required", file=sys.stderr)
        return 2

    gh_token = os.environ.get("GITHUB_TOKEN", "")
    anthropic_key = os.environ.get("ANTHROPIC_API_KEY", "")
    if not gh_token or not anthropic_key:
        print("GITHUB_TOKEN and ANTHROPIC_API_KEY env vars are required", file=sys.stderr)
        return 2

    github_client = _GhGitHubClient(gh_token)
    contents_http = _ContentsHTTP(gh_token)
    store = GitBackedBudgetStore(
        contents_http, args.repo, args.state_branch, args.state_path
    )
    model_client = _anthropic_call(anthropic_key)

    result = run_review(
        args.pr,
        args.repo,
        github_client=github_client,
        model_client=model_client,
        store=store,
        source=args.source,
    )
    print(
        json.dumps(
            {
                "posted": result.posted,
                "skipped": result.skipped,
                "reason": result.reason,
                "used_tokens": result.used_tokens,
                "model": result.model,
                "comment_id": result.comment_id,
            }
        )
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
