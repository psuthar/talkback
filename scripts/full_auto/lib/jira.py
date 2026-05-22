#!/usr/bin/env python3
"""SCRUM-530: Atlassian Jira REST API wrapper for the FULL_AUTO close-out.

Thin urllib client over the small subset close.py needs: list-transitions,
transition-issue, add-comment. Basic-auth from ``lib/auth.jira_auth()``.
"""

from __future__ import annotations

import base64
import json
import urllib.error
import urllib.request
from typing import Protocol

DONE_TRANSITION_NAME = "Done"


class JiraAPI(Protocol):
    def get_transitions(self, key: str) -> list[dict]:
        """Return the list of available transitions for ``key`` as dicts with
        at least ``id`` and ``name`` fields."""
        ...

    def transition(self, key: str, transition_id: str) -> None: ...

    def add_comment(self, key: str, body: str) -> int:
        """Add a comment with body ``body`` to ``key``. Returns comment id."""
        ...

    def get_issue(self, key: str) -> dict:
        """SCRUM-542: fetch the full issue payload (summary, description,
        labels, issuetype, status). Returns the parsed JSON ``fields`` dict
        plus ``key``."""
        ...

    def update_issue(self, key: str, fields: dict) -> None:
        """SCRUM-542: PUT /issue/<key> with the given ``fields`` dict.
        Used by start.py's auto-fix patch loop to rewrite the description."""
        ...

    def search_issues(self, jql: str, *, max_results: int = 50) -> list[dict]:
        """SCRUM-551: POST /rest/api/3/search/jql with the given JQL.
        Returns a list of issue dicts with at minimum ``key`` and
        ``fields`` (containing the fields requested). The caller
        projects to the lean shape it needs."""
        ...


def _basic_auth(email: str, token: str) -> str:
    raw = f"{email}:{token}".encode("utf-8")
    return f"Basic {base64.b64encode(raw).decode('ascii')}"


def _request(
    method: str, url: str, *, auth_header: str, body: dict | None = None
) -> tuple[int, dict]:
    req = urllib.request.Request(
        url,
        data=json.dumps(body).encode("utf-8") if body is not None else None,
        method=method,
        headers={
            "Authorization": auth_header,
            "Accept": "application/json",
            "Content-Type": "application/json",
        },
    )
    try:
        with urllib.request.urlopen(req) as resp:
            text = resp.read().decode("utf-8") or "{}"
            return resp.status, json.loads(text) if text.strip() else {}
    except urllib.error.HTTPError as e:
        text = e.read().decode("utf-8") or "{}"
        return e.code, json.loads(text) if text.strip() else {}


class HttpJiraAPI:
    """Production ``JiraAPI`` implementation."""

    def __init__(self, base_url: str, email: str, token: str):
        self._base = base_url.rstrip("/")
        self._auth = _basic_auth(email, token)

    def get_transitions(self, key: str) -> list[dict]:
        status, body = _request(
            "GET",
            f"{self._base}/rest/api/3/issue/{key}/transitions",
            auth_header=self._auth,
        )
        if status >= 400:
            raise RuntimeError(f"GET transitions for {key} -> {status}: {body}")
        return body.get("transitions", [])

    def transition(self, key: str, transition_id: str) -> None:
        status, body = _request(
            "POST",
            f"{self._base}/rest/api/3/issue/{key}/transitions",
            auth_header=self._auth,
            body={"transition": {"id": transition_id}},
        )
        if status >= 400:
            raise RuntimeError(f"POST transition {transition_id} on {key} -> {status}: {body}")

    def get_issue(self, key: str) -> dict:
        status, body = _request(
            "GET",
            f"{self._base}/rest/api/3/issue/{key}",
            auth_header=self._auth,
        )
        if status >= 400:
            raise RuntimeError(f"GET issue {key} -> {status}: {body}")
        fields = body.get("fields", {}) or {}
        return {
            "key": body.get("key", key),
            "summary": fields.get("summary", ""),
            "description": fields.get("description"),
            "labels": fields.get("labels", []) or [],
            "issuetype": (fields.get("issuetype") or {}).get("name", ""),
            "status": (fields.get("status") or {}).get("name", ""),
        }

    def update_issue(self, key: str, fields: dict) -> None:
        status, resp = _request(
            "PUT",
            f"{self._base}/rest/api/3/issue/{key}",
            auth_header=self._auth,
            body={"fields": fields},
        )
        if status >= 400:
            raise RuntimeError(f"PUT issue {key} -> {status}: {resp}")

    def search_issues(self, jql: str, *, max_results: int = 50) -> list[dict]:
        status, resp = _request(
            "POST",
            f"{self._base}/rest/api/3/search/jql",
            auth_header=self._auth,
            body={
                "jql": jql,
                "maxResults": int(max_results),
                "fields": ["summary", "status", "issuetype", "priority", "labels"],
            },
        )
        if status >= 400:
            raise RuntimeError(f"POST search/jql -> {status}: {resp}")
        return list(resp.get("issues", []) or [])

    def add_comment(self, key: str, body: str) -> int:
        # ADF body shape — single paragraph node with the comment text. Mirrors
        # what the Atlassian MCP produces; readers see plain text in Jira UI.
        payload = {
            "body": {
                "type": "doc",
                "version": 1,
                "content": [
                    {
                        "type": "paragraph",
                        "content": [{"type": "text", "text": body}],
                    }
                ],
            }
        }
        status, resp = _request(
            "POST",
            f"{self._base}/rest/api/3/issue/{key}/comment",
            auth_header=self._auth,
            body=payload,
        )
        if status >= 400:
            raise RuntimeError(f"POST comment on {key} -> {status}: {resp}")
        return int(resp.get("id", 0))


def resolve_done_transition_id(api: JiraAPI, key: str) -> str:
    """Look up the transition id for the "Done" transition on ``key``.

    SCRUM project uses id "51" but resolving by name is more portable.
    """
    return resolve_transition_id_by_name(api, key, DONE_TRANSITION_NAME)


def resolve_transition_id_by_name(api: JiraAPI, key: str, name: str) -> str:
    """SCRUM-542: generic version of ``resolve_done_transition_id`` used by
    ``start.py`` ("In Progress") and ``review.py`` ("In Review")."""
    for t in api.get_transitions(key):
        if t.get("name", "").lower() == name.lower():
            return str(t["id"])
    raise RuntimeError(f"No {name!r} transition available on {key}")


__all__ = [
    "DONE_TRANSITION_NAME",
    "HttpJiraAPI",
    "JiraAPI",
    "resolve_done_transition_id",
    "resolve_transition_id_by_name",
]
