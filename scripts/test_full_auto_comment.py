#!/usr/bin/env python3
"""SCRUM-550: tests for ``scripts/full_auto/comment.py``."""

from __future__ import annotations

import sys
import unittest
from pathlib import Path

_REPO_ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(_REPO_ROOT / "scripts"))

from full_auto import comment as comment_mod  # noqa: E402


class _FakeJiraAPI:
    def __init__(self, *, next_id: int = 9000, raise_on_add: Exception | None = None):
        self._next_id = next_id
        self._raise = raise_on_add
        self.comments: list[tuple[str, str]] = []

    def add_comment(self, key, body):
        if self._raise is not None:
            raise self._raise
        self.comments.append((key, body))
        self._next_id += 1
        return self._next_id

    # Protocol stubs — not used by comment.py.
    def get_transitions(self, key):
        raise NotImplementedError

    def transition(self, key, transition_id):
        raise NotImplementedError

    def get_issue(self, key):
        raise NotImplementedError

    def update_issue(self, key, fields):
        raise NotImplementedError


class CommentHappyPathTest(unittest.TestCase):
    def test_posts_comment_and_returns_id(self):
        jira = _FakeJiraAPI(next_id=9000)
        result = comment_mod.comment(
            "SCRUM-999",
            body="Some comment body.",
            jira_api=jira,
        )
        self.assertIsNone(result.aborted_reason, result.actions_taken)
        self.assertEqual(result.comment_id, 9001)
        self.assertEqual(jira.comments, [("SCRUM-999", "Some comment body.")])
        self.assertTrue(result.actions_taken[-1].startswith("comment.py succeeded:"))

    def test_body_chars_recorded(self):
        jira = _FakeJiraAPI()
        body = "a" * 250
        result = comment_mod.comment("SCRUM-999", body=body, jira_api=jira)
        self.assertEqual(result.body_chars, 250)


class CommentEmptyBodyTest(unittest.TestCase):
    def test_empty_body_aborts(self):
        jira = _FakeJiraAPI()
        result = comment_mod.comment("SCRUM-999", body="", jira_api=jira)
        self.assertIsNotNone(result.aborted_reason)
        self.assertEqual(jira.comments, [])
        self.assertTrue(result.actions_taken[-1].startswith("comment.py aborted:"))

    def test_whitespace_only_body_aborts(self):
        jira = _FakeJiraAPI()
        result = comment_mod.comment("SCRUM-999", body="   \n\t  \n", jira_api=jira)
        self.assertIsNotNone(result.aborted_reason)
        self.assertEqual(jira.comments, [])


class CommentDryRunTest(unittest.TestCase):
    def test_dry_run_no_mutations(self):
        jira = _FakeJiraAPI()
        result = comment_mod.comment(
            "SCRUM-999",
            body="hi",
            dry_run=True,
            jira_api=jira,
        )
        self.assertIsNone(result.aborted_reason)
        self.assertIsNone(result.comment_id)
        self.assertEqual(jira.comments, [])
        self.assertTrue(result.actions_taken[-1].startswith("comment.py dry-run:"))


class CommentApiErrorTest(unittest.TestCase):
    def test_runtime_error_propagates(self):
        # Errors from the REST layer should propagate — the agent sees a
        # clean Python traceback rather than a silently-swallowed failure.
        # Mirrors close.py's posture (per-step failures exit non-zero).
        jira = _FakeJiraAPI(raise_on_add=RuntimeError("POST -> 500: server"))
        with self.assertRaises(RuntimeError):
            comment_mod.comment("SCRUM-999", body="hi", jira_api=jira)


if __name__ == "__main__":
    unittest.main()
