#!/usr/bin/env python3
"""SCRUM-552: tests for ``scripts/full_auto/get_issue.py``."""

from __future__ import annotations

import sys
import unittest
from pathlib import Path

_REPO_ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(_REPO_ROOT / "scripts"))

from full_auto import get_issue as get_issue_mod  # noqa: E402


class _FakeJiraAPI:
    def __init__(self, payload: dict | None = None, raise_on_get: Exception | None = None):
        self._payload = payload or {}
        self._raise = raise_on_get
        self.get_calls: list[str] = []

    def get_issue(self, key):
        self.get_calls.append(key)
        if self._raise is not None:
            raise self._raise
        return dict(self._payload)

    # Protocol stubs.
    def get_transitions(self, key):
        raise NotImplementedError
    def transition(self, key, transition_id):
        raise NotImplementedError
    def add_comment(self, key, body):
        raise NotImplementedError
    def update_issue(self, key, fields):
        raise NotImplementedError
    def search_issues(self, jql, *, max_results=50):
        raise NotImplementedError


def _adf_paragraph(text: str) -> dict:
    return {
        "type": "doc",
        "version": 1,
        "content": [{"type": "paragraph", "content": [{"type": "text", "text": text}]}],
    }


class GetIssueDefaultOutputTest(unittest.TestCase):
    def test_returns_lean_fields_without_description(self):
        jira = _FakeJiraAPI(payload={
            "summary": "test ticket",
            "issuetype": "Task",
            "status": "In Progress",
            "labels": ["agent-authored"],
            "description": _adf_paragraph("## Context\n\nbody.\n"),
        })
        result = get_issue_mod.get_issue("SCRUM-999", jira_api=jira)
        self.assertIsNone(result.aborted_reason)
        self.assertEqual(result.summary, "test ticket")
        self.assertEqual(result.issuetype, "Task")
        self.assertEqual(result.status, "In Progress")
        self.assertEqual(result.labels, ["agent-authored"])
        self.assertIsNone(result.description_md)
        self.assertTrue(result.actions_taken[-1].startswith("get_issue.py succeeded:"))


class GetIssueDescriptionFlagTest(unittest.TestCase):
    def test_include_description_adds_markdown(self):
        jira = _FakeJiraAPI(payload={
            "summary": "x",
            "issuetype": "Task",
            "status": "To Do",
            "labels": [],
            "description": _adf_paragraph("## AC\n\n- [ ] x"),
        })
        result = get_issue_mod.get_issue(
            "SCRUM-999", include_description=True, jira_api=jira
        )
        self.assertIsNotNone(result.description_md)
        self.assertIn("## AC", result.description_md)


class GetIssueFieldRestrictTest(unittest.TestCase):
    def test_project_to_fields_keeps_only_named_fields(self):
        result = get_issue_mod.GetIssueResult(
            ticket="SCRUM-999",
            summary="x",
            issuetype="Task",
            status="To Do",
            labels=["a"],
            description_md=None,
            actions_taken=["fetched", "get_issue.py succeeded: 1 actions, no aborts"],
        )
        out = get_issue_mod._project_to_fields(result, ["status"])
        self.assertEqual(set(out.keys()), {"ticket", "actions_taken", "status"})
        self.assertEqual(out["status"], "To Do")

    def test_project_to_fields_with_multiple_fields(self):
        result = get_issue_mod.GetIssueResult(
            ticket="SCRUM-999",
            summary="x",
            issuetype="Task",
            status="To Do",
            labels=["a"],
            actions_taken=["fetched", "get_issue.py succeeded: 1 actions, no aborts"],
        )
        out = get_issue_mod._project_to_fields(result, ["summary", "labels"])
        self.assertEqual(set(out.keys()), {"ticket", "actions_taken", "summary", "labels"})

    def test_project_to_fields_no_restriction_returns_full(self):
        result = get_issue_mod.GetIssueResult(
            ticket="SCRUM-999",
            summary="x",
            status="To Do",
            actions_taken=["x"],
        )
        out = get_issue_mod._project_to_fields(result, None)
        # All dataclass fields present.
        self.assertIn("summary", out)
        self.assertIn("labels", out)
        self.assertIn("issuetype", out)


class GetIssueErrorTest(unittest.TestCase):
    def test_missing_ticket_propagates_as_abort(self):
        jira = _FakeJiraAPI(raise_on_get=RuntimeError("GET issue SCRUM-X -> 404: {}"))
        result = get_issue_mod.get_issue("SCRUM-X", jira_api=jira)
        self.assertIsNotNone(result.aborted_reason)
        self.assertIn("404", result.aborted_reason)
        self.assertTrue(result.actions_taken[-1].startswith("get_issue.py aborted:"))


if __name__ == "__main__":
    unittest.main()
