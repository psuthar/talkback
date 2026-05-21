#!/usr/bin/env python3
"""SCRUM-543: tests for ``scripts/full_auto/review.py``."""

from __future__ import annotations

import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

_REPO_ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(_REPO_ROOT / "scripts"))

from full_auto import review as review_mod  # noqa: E402


class _FakeGitHubAPI:
    def __init__(self, *, pr_number: int = 999, html_url: str = "https://x/pr/999"):
        self._pr_number = pr_number
        self._html_url = html_url
        self.create_calls: list[dict] = []
        self.update_calls: list[tuple[int, str]] = []

    def create_pr(self, repo, *, title, head, base, body):
        self.create_calls.append(
            {"repo": repo, "title": title, "head": head, "base": base, "body": body}
        )
        return self._pr_number, self._html_url

    def update_pr_body(self, repo, pr_number, body):
        self.update_calls.append((pr_number, body))

    # Not used by review.py but required by the GitHubAPI Protocol.
    def read_pr(self, repo, pr_number):
        raise NotImplementedError

    def merge_pr(self, repo, pr_number):
        raise NotImplementedError


class _FakeJiraAPI:
    def __init__(self, labels: list[str]):
        self._labels = list(labels)
        self.comments: list[tuple[str, str]] = []
        self.transitions_taken: list[tuple[str, str]] = []

    def get_issue(self, key):
        return {"key": key, "labels": list(self._labels), "issuetype": "Task"}

    def get_transitions(self, key):
        return [
            {"id": "21", "name": "In Progress"},
            {"id": "31", "name": "In Review"},
            {"id": "51", "name": "Done"},
        ]

    def transition(self, key, transition_id):
        self.transitions_taken.append((key, transition_id))

    def add_comment(self, key, body):
        self.comments.append((key, body))
        return 11000 + len(self.comments)

    def update_issue(self, key, fields):
        raise NotImplementedError


def _seed_repo(td: Path, ticket: str = "SCRUM-999") -> Path:
    """Bare origin + work clone, on the feature branch."""
    origin = td / "origin.git"
    origin.mkdir()
    subprocess.run(["git", "init", "-q", "--bare", "-b", "main"], cwd=str(origin), check=True)
    work = td / "work"
    work.mkdir()
    for args in [
        ["git", "init", "-q", "-b", "main"],
        ["git", "config", "user.email", "test@example.com"],
        ["git", "config", "user.name", "Test"],
        ["git", "config", "commit.gpgsign", "false"],
        ["git", "remote", "add", "origin", str(origin)],
    ]:
        subprocess.run(args, cwd=str(work), check=True)
    (work / "seed.txt").write_text("seed\n")
    subprocess.run(["git", "add", "."], cwd=str(work), check=True)
    subprocess.run(["git", "commit", "-q", "-m", "seed"], cwd=str(work), check=True)
    subprocess.run(["git", "push", "-q", "origin", "main"], cwd=str(work), check=True)
    subprocess.run(["git", "checkout", "-q", "-b", f"feat/{ticket}"], cwd=str(work), check=True)
    return work


GOOD_PR_BODY = """\
## Jira

SCRUM-999

## Summary

- Did the thing.

## Test plan

- [x] Ran the tests
"""


BAD_PR_BODY_MISSING_TEST_PLAN = """\
## Jira

SCRUM-999

## Summary

- Did the thing.
"""


class ReviewHappyPathTest(unittest.TestCase):
    def test_create_pr_lint_comment_transition(self):
        with tempfile.TemporaryDirectory() as td:
            work = _seed_repo(Path(td))
            gh = _FakeGitHubAPI(pr_number=480)
            jira = _FakeJiraAPI(labels=["agent-authored"])
            result = review_mod.review(
                "SCRUM-999",
                title="SCRUM-999: thing",
                pr_body=GOOD_PR_BODY,
                completion_comment="implementation complete.",
                github_api=gh,
                jira_api=jira,
                repo_root=work,
                ticket_labels=["agent-authored"],
            )
        self.assertIsNone(result.aborted_reason, result.actions_taken)
        self.assertEqual(result.pr_number, 480)
        self.assertEqual(result.pr_body_lint_status, "pass")
        self.assertEqual(len(jira.comments), 1)
        self.assertEqual(jira.transitions_taken, [("SCRUM-999", "31")])
        self.assertTrue(result.actions_taken[-1].startswith("review.py succeeded:"))


class ReviewBranchGuardTest(unittest.TestCase):
    def test_aborts_when_branch_does_not_match(self):
        with tempfile.TemporaryDirectory() as td:
            work = _seed_repo(Path(td))
            # Switch to a non-feature branch.
            subprocess.run(["git", "checkout", "-q", "main"], cwd=str(work), check=True)
            gh = _FakeGitHubAPI()
            jira = _FakeJiraAPI(labels=["agent-authored"])
            result = review_mod.review(
                "SCRUM-999",
                title="x",
                pr_body=GOOD_PR_BODY,
                completion_comment="x",
                github_api=gh,
                jira_api=jira,
                repo_root=work,
                ticket_labels=["agent-authored"],
            )
        self.assertIsNotNone(result.aborted_reason)
        self.assertIn("does not start with", result.aborted_reason)
        # Nothing else ran.
        self.assertEqual(gh.create_calls, [])
        self.assertEqual(jira.comments, [])
        self.assertEqual(jira.transitions_taken, [])

    def test_worktree_branch_suffix_is_allowed(self):
        # The prefix check tolerates worktree-style names like
        # feat/SCRUM-999-worktree per the ticket's "Risks" section.
        with tempfile.TemporaryDirectory() as td:
            work = _seed_repo(Path(td))
            subprocess.run(
                ["git", "checkout", "-q", "-b", "feat/SCRUM-999-worktree"],
                cwd=str(work),
                check=True,
            )
            gh = _FakeGitHubAPI()
            jira = _FakeJiraAPI(labels=["agent-authored"])
            result = review_mod.review(
                "SCRUM-999",
                title="x",
                pr_body=GOOD_PR_BODY,
                completion_comment="x",
                github_api=gh,
                jira_api=jira,
                repo_root=work,
                ticket_labels=["agent-authored"],
            )
        self.assertIsNone(result.aborted_reason, result.actions_taken)


class ReviewLintFailTest(unittest.TestCase):
    def test_human_authored_pr_body_exit2_halts(self):
        with tempfile.TemporaryDirectory() as td:
            work = _seed_repo(Path(td))
            gh = _FakeGitHubAPI(pr_number=481)
            jira = _FakeJiraAPI(labels=[])  # NOT agent-authored
            result = review_mod.review(
                "SCRUM-999",
                title="x",
                pr_body=BAD_PR_BODY_MISSING_TEST_PLAN,
                completion_comment="x",
                github_api=gh,
                jira_api=jira,
                repo_root=work,
                ticket_labels=[],
            )
        self.assertIsNotNone(result.aborted_reason)
        self.assertEqual(result.pr_body_lint_status, "halted_gaps")
        # PR was opened but no comment + no transition.
        self.assertEqual(len(gh.create_calls), 1)
        self.assertEqual(jira.comments, [])
        self.assertEqual(jira.transitions_taken, [])

    def test_agent_authored_pr_body_exit2_patches_then_passes(self):
        with tempfile.TemporaryDirectory() as td:
            work = _seed_repo(Path(td))
            gh = _FakeGitHubAPI(pr_number=482)
            jira = _FakeJiraAPI(labels=["agent-authored"])
            result = review_mod.review(
                "SCRUM-999",
                title="x",
                pr_body=BAD_PR_BODY_MISSING_TEST_PLAN,
                completion_comment="x",
                github_api=gh,
                jira_api=jira,
                repo_root=work,
                ticket_labels=["agent-authored"],
            )
        self.assertIsNone(result.aborted_reason, result.actions_taken)
        self.assertEqual(result.pr_body_lint_status, "patched_then_pass")
        self.assertEqual(len(gh.update_calls), 1)
        # PR body PATCH carried the patched body.
        patched_body = gh.update_calls[0][1]
        self.assertIn("Test plan", patched_body)


class ReviewDryRunTest(unittest.TestCase):
    def test_dry_run_no_mutations(self):
        with tempfile.TemporaryDirectory() as td:
            work = _seed_repo(Path(td))
            gh = _FakeGitHubAPI()
            jira = _FakeJiraAPI(labels=["agent-authored"])
            result = review_mod.review(
                "SCRUM-999",
                title="x",
                pr_body=GOOD_PR_BODY,
                completion_comment="x",
                dry_run=True,
                github_api=gh,
                jira_api=jira,
                repo_root=work,
                ticket_labels=["agent-authored"],
            )
        self.assertIsNone(result.aborted_reason)
        self.assertEqual(gh.create_calls, [])
        self.assertEqual(gh.update_calls, [])
        self.assertEqual(jira.comments, [])
        self.assertEqual(jira.transitions_taken, [])
        self.assertTrue(result.actions_taken[-1].startswith("review.py dry-run:"))


if __name__ == "__main__":
    unittest.main()
