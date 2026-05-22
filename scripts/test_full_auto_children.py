#!/usr/bin/env python3
"""SCRUM-551: tests for ``scripts/full_auto/children.py``."""

from __future__ import annotations

import sys
import unittest
from pathlib import Path

_REPO_ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(_REPO_ROOT / "scripts"))

from full_auto import children as children_mod  # noqa: E402


def _issue_payload(key, summary, status="To Do", issuetype="Task", priority="Medium", labels=None):
    """Build an Atlassian-shaped issue dict (the kind HttpJiraAPI.search_issues returns)."""
    return {
        "key": key,
        "fields": {
            "summary": summary,
            "status": {"name": status, "iconUrl": "https://...", "id": "X", "statusCategory": {"colorName": "yellow"}},
            "issuetype": {"name": issuetype, "iconUrl": "https://...", "avatarId": 42},
            "priority": {"name": priority, "iconUrl": "https://..."},
            "labels": labels or [],
        },
    }


class _FakeJiraAPI:
    def __init__(self, issues: list[dict] | None = None):
        self._issues = list(issues or [])
        self.search_calls: list[tuple[str, int]] = []

    def search_issues(self, jql, *, max_results=50):
        self.search_calls.append((jql, max_results))
        return list(self._issues)

    # Protocol stubs — not used by children.py.
    def get_transitions(self, key):
        raise NotImplementedError
    def transition(self, key, transition_id):
        raise NotImplementedError
    def add_comment(self, key, body):
        raise NotImplementedError
    def get_issue(self, key):
        raise NotImplementedError
    def update_issue(self, key, fields):
        raise NotImplementedError


class EpicPresetTest(unittest.TestCase):
    def test_default_jql_filters_done(self):
        jira = _FakeJiraAPI()
        result = children_mod.children(epic="SCRUM-549", jira_api=jira)
        self.assertIsNone(result.aborted_reason)
        self.assertEqual(
            result.jql_used,
            "parent = SCRUM-549 AND statusCategory != Done ORDER BY created ASC",
        )

    def test_include_done_drops_filter(self):
        jira = _FakeJiraAPI()
        result = children_mod.children(epic="SCRUM-549", include_done=True, jira_api=jira)
        self.assertEqual(
            result.jql_used,
            "parent = SCRUM-549 ORDER BY created ASC",
        )

    def test_projection_drops_avatars_and_categories(self):
        jira = _FakeJiraAPI(issues=[
            _issue_payload("SCRUM-550", "comment.py", status="Done"),
            _issue_payload("SCRUM-551", "children.py", status="In Progress"),
        ])
        result = children_mod.children(epic="SCRUM-549", jira_api=jira)
        self.assertEqual(result.count, 2)
        self.assertEqual(
            result.children,
            [
                {"key": "SCRUM-550", "summary": "comment.py", "status": "Done",
                 "issuetype": "Task", "priority": "Medium", "labels": []},
                {"key": "SCRUM-551", "summary": "children.py", "status": "In Progress",
                 "issuetype": "Task", "priority": "Medium", "labels": []},
            ],
        )
        # No iconUrl, statusCategory, avatarId, etc. anywhere in the output.
        for child in result.children:
            for key in child.keys():
                self.assertNotIn("Url", key)
                self.assertNotIn("Category", key)


class JqlPassthroughTest(unittest.TestCase):
    def test_passthrough_with_project_clause(self):
        jira = _FakeJiraAPI(issues=[_issue_payload("SCRUM-100", "x")])
        result = children_mod.children(
            jql="project = SCRUM AND status = 'In Progress'", jira_api=jira
        )
        self.assertIsNone(result.aborted_reason)
        self.assertEqual(result.count, 1)
        self.assertEqual(jira.search_calls[0][0], "project = SCRUM AND status = 'In Progress'")

    def test_passthrough_with_parent_clause(self):
        jira = _FakeJiraAPI()
        result = children_mod.children(jql="parent = SCRUM-549", jira_api=jira)
        self.assertIsNone(result.aborted_reason)

    def test_passthrough_without_scrum_reference_is_rejected(self):
        jira = _FakeJiraAPI()
        result = children_mod.children(jql="status = 'In Progress'", jira_api=jira)
        self.assertIsNotNone(result.aborted_reason)
        self.assertIn("must reference the SCRUM project", result.aborted_reason)
        self.assertEqual(jira.search_calls, [])

    def test_passthrough_for_other_project_is_rejected(self):
        jira = _FakeJiraAPI()
        result = children_mod.children(jql="project = OTHER", jira_api=jira)
        self.assertIsNotNone(result.aborted_reason)
        self.assertEqual(jira.search_calls, [])


class ArgumentValidationTest(unittest.TestCase):
    def test_neither_epic_nor_jql_aborts(self):
        jira = _FakeJiraAPI()
        result = children_mod.children(jira_api=jira)
        self.assertIsNotNone(result.aborted_reason)
        self.assertIn("exactly one", result.aborted_reason)

    def test_both_epic_and_jql_aborts(self):
        jira = _FakeJiraAPI()
        result = children_mod.children(
            epic="SCRUM-549", jql="project = SCRUM", jira_api=jira
        )
        self.assertIsNotNone(result.aborted_reason)


class EmptyResultTest(unittest.TestCase):
    def test_no_children_returns_count_zero(self):
        jira = _FakeJiraAPI(issues=[])
        result = children_mod.children(epic="SCRUM-549", jira_api=jira)
        self.assertIsNone(result.aborted_reason)
        self.assertEqual(result.count, 0)
        self.assertEqual(result.children, [])
        self.assertTrue(result.actions_taken[-1].startswith("children.py succeeded:"))


if __name__ == "__main__":
    unittest.main()
