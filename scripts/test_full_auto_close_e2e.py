#!/usr/bin/env python3
"""SCRUM-537: end-to-end tests for ``scripts/full_auto/close.py``.

The companion ``scripts/test_full_auto_close.py`` mocks every git
operation via ``_patch_git_ops``. That keeps unit tests fast but lets
dirty-tree and porcelain-parser bugs slip through — both prior real
incidents (SCRUM-534 auto-stash, the ``_run().strip()`` parser bug)
were only caught after they surfaced in chat.

These tests run ``close()`` against a real ephemeral git repo created
via ``tempfile.TemporaryDirectory`` plus a bare ``origin`` remote.
Only ``github_api`` and ``jira_api`` are mocked; every ``git_ops.*``
helper executes real git.
"""

from __future__ import annotations

import os
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

_REPO_ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(_REPO_ROOT / "scripts"))

from full_auto import close as close_mod  # noqa: E402
from full_auto.lib import git_ops  # noqa: E402
from full_auto.lib.github import PRSnapshot  # noqa: E402
from full_auto.lib.templates import MANUAL_OVERRIDE, POLLING  # noqa: E402


def _git(args: list[str], cwd: Path, check: bool = True) -> str:
    return subprocess.run(
        args, cwd=str(cwd), capture_output=True, text=True, check=check
    ).stdout.strip()


def _seed_repo(td: Path, ticket: str = "SCRUM-999") -> tuple[Path, Path, str]:
    """Set up a bare origin + working clone with a feat/<TICKET> branch.

    Returns ``(work_dir, origin_bare, feature_sha)``. The working tree
    starts on ``main`` so close.py's ``git checkout main`` is a no-op,
    matching what happens post-PR-merge in real usage.
    """
    origin = td / "origin.git"
    origin.mkdir()
    _git(["git", "init", "-q", "--bare", "-b", "main"], cwd=origin)

    work = td / "work"
    work.mkdir()
    _git(["git", "init", "-q", "-b", "main"], cwd=work)
    _git(["git", "config", "user.email", "test@example.com"], cwd=work)
    _git(["git", "config", "user.name", "Test"], cwd=work)
    _git(["git", "config", "commit.gpgsign", "false"], cwd=work)
    _git(["git", "remote", "add", "origin", str(origin)], cwd=work)

    # Seed main with the lint log file so dirty-tree scenarios have a
    # tracked path to modify (matches the real repo layout).
    log_path = work / git_ops.LINT_LOG_PATH
    log_path.parent.mkdir(parents=True, exist_ok=True)
    log_path.write_text("seed-row\n")
    _git(["git", "add", "."], cwd=work)
    _git(["git", "commit", "-q", "-m", "seed"], cwd=work)
    _git(["git", "push", "-q", "origin", "main"], cwd=work)

    # Build a feature branch with one commit ahead and push to origin.
    branch = f"feat/{ticket}"
    _git(["git", "checkout", "-q", "-b", branch], cwd=work)
    (work / "feature.txt").write_text("ticket work\n")
    _git(["git", "add", "feature.txt"], cwd=work)
    _git(["git", "commit", "-q", "-m", f"{ticket}: feature work"], cwd=work)
    feature_sha = _git(["git", "rev-parse", "HEAD"], cwd=work)
    _git(["git", "push", "-q", "-u", "origin", branch], cwd=work)
    # Leave the working tree ON the feature branch — that matches what
    # close.py actually sees post-PR-merge (the user pushed + opened the
    # PR, never left the feature branch). The ``git checkout main`` step
    # inside close.py is then a real branch switch, which is what
    # exercises the dirty-tree refusal in the SCRUM-534 scenario.
    return work, origin, feature_sha


class _FakeGitHubAPI:
    """Mirror the real GitHub API surface close.py uses, with a hook to
    promote main on the bare origin when ``merge_pr`` is called — that
    keeps the post-merge state realistic for the working clone's pull."""

    def __init__(
        self,
        snapshot: PRSnapshot,
        *,
        work: Path | None = None,
        feature_sha: str | None = None,
    ):
        self._snap = snapshot
        self._work = work
        self._feature_sha = feature_sha
        self.read_calls = 0
        self.merge_calls: list[tuple[str, int]] = []

    def read_pr(self, repo, pr_number):
        self.read_calls += 1
        return self._snap

    def merge_pr(self, repo, pr_number):
        self.merge_calls.append((repo, pr_number))
        if self._work is not None and self._feature_sha is not None:
            # Simulate the squash merge by clobbering origin/main with the
            # feature commit. ``--force`` because the test scenario that
            # exercises the dirty-tree abort has its own baseline commit
            # on main ahead of the feature SHA, and a real squash merge
            # would have included those commits.
            _git(
                [
                    "git",
                    "push",
                    "-q",
                    "--force",
                    "origin",
                    f"{self._feature_sha}:main",
                ],
                cwd=self._work,
            )
        return self._feature_sha or "deadbeefdeadbeef"


class _FakeJiraAPI:
    def __init__(self):
        self.transition_calls: list[tuple[str, str]] = []
        self.comments: list[tuple[str, str]] = []
        self._next = 9000

    def get_transitions(self, key):
        return [{"id": "21", "name": "In Progress"}, {"id": "51", "name": "Done"}]

    def transition(self, key, transition_id):
        self.transition_calls.append((key, transition_id))

    def add_comment(self, key, body):
        self.comments.append((key, body))
        self._next += 1
        return self._next


def _open_pr_clean(feature_sha: str) -> PRSnapshot:
    return PRSnapshot(
        number=999,
        state="open",
        merged=False,
        merge_commit_sha=None,
        mergeable_state="clean",
        head_ref="feat/SCRUM-999",
        base_ref="main",
    )


def _open_pr_blocked() -> PRSnapshot:
    return PRSnapshot(
        number=999,
        state="open",
        merged=False,
        merge_commit_sha=None,
        mergeable_state="blocked",
        head_ref="feat/SCRUM-999",
        base_ref="main",
    )


def _merged_pr(sha: str) -> PRSnapshot:
    return PRSnapshot(
        number=999,
        state="closed",
        merged=True,
        merge_commit_sha=sha,
        mergeable_state="unknown",
        head_ref="feat/SCRUM-999",
        base_ref="main",
    )


class CloseEndToEndTest(unittest.TestCase):
    def test_clean_tree_polling_path_succeeds(self):
        with tempfile.TemporaryDirectory() as td:
            work, _origin, feature_sha = _seed_repo(Path(td))
            gh = _FakeGitHubAPI(
                _open_pr_clean(feature_sha), work=work, feature_sha=feature_sha
            )
            jira = _FakeJiraAPI()
            result = close_mod.close(
                "SCRUM-999",
                pr_number=999,
                path_indicator=POLLING,
                github_api=gh,
                jira_api=jira,
                repo_root=work,
            )
            self.assertIsNone(result.aborted_reason, result.actions_taken)
            self.assertEqual(gh.merge_calls, [(close_mod.DEFAULT_REPO, 999)])
            self.assertEqual(result.merged_sha, feature_sha)
            # SCRUM-536 summary line is present and well-formed.
            self.assertTrue(result.actions_taken[-1].startswith("close.py succeeded:"))
            # Feature branch is gone locally; main is at the feature SHA.
            branches = _git(["git", "branch"], cwd=work)
            self.assertNotIn("feat/SCRUM-999", branches)
            self.assertEqual(_git(["git", "rev-parse", "HEAD"], cwd=work), feature_sha)

    def test_dirty_lint_log_only_auto_stash_preserves_row(self):
        with tempfile.TemporaryDirectory() as td:
            work, _origin, feature_sha = _seed_repo(Path(td))
            gh = _FakeGitHubAPI(
                _open_pr_clean(feature_sha), work=work, feature_sha=feature_sha
            )
            jira = _FakeJiraAPI()
            # Dirty the lint log on the work tree (post-feature-commit state).
            new_row = "seed-row\n{\"ticket\": \"SCRUM-537\", \"issue_type\": \"PR\"}\n"
            (work / git_ops.LINT_LOG_PATH).write_text(new_row)
            self.assertTrue(git_ops.lint_log_only_dirty(cwd=work))

            result = close_mod.close(
                "SCRUM-999",
                pr_number=999,
                path_indicator=POLLING,
                github_api=gh,
                jira_api=jira,
                repo_root=work,
            )
            self.assertIsNone(result.aborted_reason)
            # Row survived stash → checkout → pop.
            self.assertEqual((work / git_ops.LINT_LOG_PATH).read_text(), new_row)
            # actions_taken records the auto-stash.
            stash_line = next(
                (a for a in result.actions_taken if "stashed and restored" in a),
                None,
            )
            self.assertIsNotNone(stash_line, result.actions_taken)

    def test_dirty_lint_log_plus_other_tracked_aborts(self):
        with tempfile.TemporaryDirectory() as td:
            work, _origin, feature_sha = _seed_repo(Path(td))
            gh = _FakeGitHubAPI(
                _open_pr_clean(feature_sha), work=work, feature_sha=feature_sha
            )
            jira = _FakeJiraAPI()
            # Dirty BOTH the lint log and an unrelated tracked file.
            # Need to commit a baseline file first so it's "tracked" when
            # we dirty it.
            other = work / "other.txt"
            other.write_text("seed\n")
            _git(["git", "add", "other.txt"], cwd=work)
            _git(["git", "commit", "-q", "-m", "add other.txt"], cwd=work)
            _git(["git", "push", "-q", "origin", "main"], cwd=work)
            # Now dirty both files.
            (work / git_ops.LINT_LOG_PATH).write_text("seed-row\nrow2\n")
            other.write_text("seed\nmodified by user\n")
            self.assertFalse(git_ops.lint_log_only_dirty(cwd=work))

            # close.py should not auto-stash; the real `git checkout main`
            # will fail because of the dirty `other.txt`.
            with self.assertRaises(RuntimeError) as cm:
                close_mod.close(
                    "SCRUM-999",
                    pr_number=999,
                    path_indicator=POLLING,
                    github_api=gh,
                    jira_api=jira,
                    repo_root=work,
                )
            # The failure happens inside git_ops, before the post-merge
            # local-cleanup step completes. merge_pr did fire, which is
            # correct — the auto-stash gate only applies to local cleanup.
            self.assertIn("git checkout main failed", str(cm.exception))
            self.assertIn("other.txt", str(cm.exception))

    def test_manual_override_path_skips_merge_and_finalises(self):
        with tempfile.TemporaryDirectory() as td:
            work, origin, feature_sha = _seed_repo(Path(td))
            # Simulate the user having already squash-merged the PR by
            # advancing origin/main to the feature commit.
            _git(
                ["git", "push", "-q", "origin", f"{feature_sha}:main"],
                cwd=work,
            )
            gh = _FakeGitHubAPI(_merged_pr(feature_sha))
            jira = _FakeJiraAPI()
            result = close_mod.close(
                "SCRUM-999",
                pr_number=999,
                path_indicator=MANUAL_OVERRIDE,
                github_api=gh,
                jira_api=jira,
                repo_root=work,
            )
            self.assertIsNone(result.aborted_reason)
            # The manual-override path MUST NOT call merge_pr.
            self.assertEqual(gh.merge_calls, [])
            self.assertEqual(result.merged_sha, feature_sha)
            # Feature branch deleted locally; main has the feature SHA.
            branches = _git(["git", "branch"], cwd=work)
            self.assertNotIn("feat/SCRUM-999", branches)
            self.assertEqual(_git(["git", "rev-parse", "HEAD"], cwd=work), feature_sha)
            self.assertTrue(result.actions_taken[-1].startswith("close.py succeeded:"))

    def test_pre_merge_guard_aborts_on_non_clean_state(self):
        with tempfile.TemporaryDirectory() as td:
            work, _origin, feature_sha = _seed_repo(Path(td))
            gh = _FakeGitHubAPI(_open_pr_blocked())
            jira = _FakeJiraAPI()
            result = close_mod.close(
                "SCRUM-999",
                pr_number=999,
                path_indicator=POLLING,
                github_api=gh,
                jira_api=jira,
                repo_root=work,
            )
            # Pre-merge guard fires; no merge, no local cleanup.
            self.assertIsNotNone(result.aborted_reason)
            self.assertIn("blocked", result.aborted_reason)
            self.assertEqual(gh.merge_calls, [])
            # SCRUM-536: abort path still emits a summary line.
            self.assertTrue(result.actions_taken[-1].startswith("close.py aborted:"))
            # Feature branch still exists locally — close.py bailed before
            # deleting it.
            branches = _git(["git", "branch"], cwd=work)
            self.assertIn("feat/SCRUM-999", branches)


if __name__ == "__main__":
    unittest.main()
