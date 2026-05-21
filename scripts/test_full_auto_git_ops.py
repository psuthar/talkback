#!/usr/bin/env python3
"""SCRUM-534: tests for the lint-log auto-stash helpers in
``scripts/full_auto/lib/git_ops.py``.

Uses a real ephemeral git repo per test (no remote) instead of mocking
``subprocess.run`` — porcelain status parsing is the whole point of these
helpers, so exercising the actual git plumbing keeps the test honest.
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

from full_auto.lib import git_ops  # noqa: E402


def _run(args: list[str], cwd: Path) -> str:
    return subprocess.run(
        args, cwd=str(cwd), capture_output=True, text=True, check=True
    ).stdout.strip()


def _make_repo(td: Path) -> Path:
    _run(["git", "init", "-q", "-b", "main"], cwd=td)
    _run(["git", "config", "user.email", "test@example.com"], cwd=td)
    _run(["git", "config", "user.name", "Test"], cwd=td)
    _run(["git", "config", "commit.gpgsign", "false"], cwd=td)
    log_path = td / git_ops.LINT_LOG_PATH
    log_path.parent.mkdir(parents=True, exist_ok=True)
    log_path.write_text("seed row\n")
    other = td / "other.txt"
    other.write_text("seed\n")
    _run(["git", "add", "."], cwd=td)
    _run(["git", "commit", "-q", "-m", "seed"], cwd=td)
    return td


class LintLogOnlyDirtyTest(unittest.TestCase):
    def test_clean_tree_returns_false(self):
        with tempfile.TemporaryDirectory() as td:
            repo = _make_repo(Path(td))
            self.assertFalse(git_ops.lint_log_only_dirty(cwd=repo))

    def test_lint_log_dirty_alone_returns_true(self):
        with tempfile.TemporaryDirectory() as td:
            repo = _make_repo(Path(td))
            (repo / git_ops.LINT_LOG_PATH).write_text("seed row\nnew row\n")
            self.assertTrue(git_ops.lint_log_only_dirty(cwd=repo))

    def test_lint_log_plus_other_tracked_returns_false(self):
        with tempfile.TemporaryDirectory() as td:
            repo = _make_repo(Path(td))
            (repo / git_ops.LINT_LOG_PATH).write_text("seed row\nnew row\n")
            (repo / "other.txt").write_text("seed\nmodified\n")
            self.assertFalse(git_ops.lint_log_only_dirty(cwd=repo))

    def test_untracked_files_are_ignored(self):
        with tempfile.TemporaryDirectory() as td:
            repo = _make_repo(Path(td))
            (repo / "ignored_untracked.txt").write_text("xx\n")
            self.assertFalse(git_ops.lint_log_only_dirty(cwd=repo))
            # Now dirty the lint log too — should still be True because the
            # untracked file doesn't count.
            (repo / git_ops.LINT_LOG_PATH).write_text("seed row\nrow2\n")
            self.assertTrue(git_ops.lint_log_only_dirty(cwd=repo))

    def test_only_other_tracked_dirty_returns_false(self):
        with tempfile.TemporaryDirectory() as td:
            repo = _make_repo(Path(td))
            (repo / "other.txt").write_text("seed\nmodified\n")
            self.assertFalse(git_ops.lint_log_only_dirty(cwd=repo))

    def test_lint_log_staged_returns_true(self):
        # The PR-mode lint writes to the working tree, but a future change
        # might stage the file before close.py runs. Pin both code paths.
        with tempfile.TemporaryDirectory() as td:
            repo = _make_repo(Path(td))
            (repo / git_ops.LINT_LOG_PATH).write_text("seed row\nrow2\n")
            _run(["git", "add", git_ops.LINT_LOG_PATH], cwd=repo)
            self.assertTrue(git_ops.lint_log_only_dirty(cwd=repo))


class StashAndPopTest(unittest.TestCase):
    def test_stash_then_pop_restores_lint_log_content(self):
        with tempfile.TemporaryDirectory() as td:
            repo = _make_repo(Path(td))
            new_content = "seed row\nrow2\nrow3\n"
            (repo / git_ops.LINT_LOG_PATH).write_text(new_content)
            # Sanity: dirty
            self.assertTrue(git_ops.lint_log_only_dirty(cwd=repo))
            git_ops.stash_lint_log(cwd=repo)
            # After stash, the file should match HEAD again.
            self.assertEqual(
                (repo / git_ops.LINT_LOG_PATH).read_text(), "seed row\n"
            )
            self.assertFalse(git_ops.lint_log_only_dirty(cwd=repo))
            git_ops.pop_stash(cwd=repo)
            # After pop, the row reappears in the working tree.
            self.assertEqual((repo / git_ops.LINT_LOG_PATH).read_text(), new_content)
            self.assertTrue(git_ops.lint_log_only_dirty(cwd=repo))

    def test_stash_pop_survives_branch_switch(self):
        # The full close.py sequence: stash → checkout main → pop. The pop
        # must restore the row on the *new* branch.
        with tempfile.TemporaryDirectory() as td:
            repo = _make_repo(Path(td))
            # Create a feature branch with a commit so we can switch.
            _run(["git", "checkout", "-q", "-b", "feat/test"], cwd=repo)
            (repo / "feat.txt").write_text("feature work\n")
            _run(["git", "add", "feat.txt"], cwd=repo)
            _run(["git", "commit", "-q", "-m", "feature"], cwd=repo)
            # Dirty the lint log AFTER the feature commit.
            new_content = "seed row\npost-commit row\n"
            (repo / git_ops.LINT_LOG_PATH).write_text(new_content)
            # Stash, checkout main, pop — the close.py auto-handle sequence.
            git_ops.stash_lint_log(cwd=repo)
            _run(["git", "checkout", "-q", "main"], cwd=repo)
            git_ops.pop_stash(cwd=repo)
            # Row survives on main.
            self.assertEqual(
                (repo / git_ops.LINT_LOG_PATH).read_text(), new_content
            )


if __name__ == "__main__":
    unittest.main()
