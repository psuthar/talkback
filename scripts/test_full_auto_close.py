#!/usr/bin/env python3
"""Unit tests for scripts/full_auto/close.py + lib modules (SCRUM-530)."""

from __future__ import annotations

import json
import sys
import tempfile
import unittest
from pathlib import Path
from unittest import mock

_REPO_ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(_REPO_ROOT / "scripts"))

from full_auto import close as close_mod  # noqa: E402
from full_auto.lib import state as state_mod  # noqa: E402
from full_auto.lib import templates as templates_mod  # noqa: E402
from full_auto.lib.github import PRSnapshot  # noqa: E402
from full_auto.lib.templates import (  # noqa: E402
    MANUAL_OVERRIDE,
    POLLING,
    WEBHOOK,
    ClosureContext,
)


# ── fakes ─────────────────────────────────────────────────────────────────────

class FakeGitHubAPI:
    def __init__(self, snapshot: PRSnapshot, post_merge_sha: str = "abc123def456"):
        self._snap = snapshot
        self._post_merge_sha = post_merge_sha
        self.read_calls = 0
        self.merge_calls: list[tuple[str, int]] = []

    def read_pr(self, repo, pr_number):
        self.read_calls += 1
        return self._snap

    def merge_pr(self, repo, pr_number):
        self.merge_calls.append((repo, pr_number))
        return self._post_merge_sha


class FakeJiraAPI:
    def __init__(self, transitions=None):
        self._transitions = transitions or [
            {"id": "21", "name": "In Progress"},
            {"id": "51", "name": "Done"},
        ]
        self.transition_calls: list[tuple[str, str]] = []
        self.comments: list[tuple[str, str]] = []
        self._next_comment_id = 9000

    def get_transitions(self, key):
        return self._transitions

    def transition(self, key, transition_id):
        self.transition_calls.append((key, transition_id))

    def add_comment(self, key, body):
        self.comments.append((key, body))
        cid = self._next_comment_id
        self._next_comment_id += 1
        return cid


def _open_pr_clean(head="feat/SCRUM-999") -> PRSnapshot:
    return PRSnapshot(
        number=999,
        state="open",
        merged=False,
        merge_commit_sha=None,
        mergeable_state="clean",
        head_ref=head,
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


def _merged_pr(sha="user_merged_sha_aaa") -> PRSnapshot:
    return PRSnapshot(
        number=999,
        state="closed",
        merged=True,
        merge_commit_sha=sha,
        mergeable_state="unknown",
        head_ref="feat/SCRUM-999",
        base_ref="main",
    )


def _patch_git_ops():
    """Patch git_ops.* on the close_mod's binding so close.py sees fakes."""
    p1 = mock.patch.object(close_mod.git_ops, "fetch_main")
    p2 = mock.patch.object(
        close_mod.git_ops, "checkout_and_pull_main", return_value="fffeeeddd000"
    )
    p3 = mock.patch.object(close_mod.git_ops, "delete_branch")
    # SCRUM-534: close.py now probes the working tree for a dirty lint log
    # before checkout. Default the probe to "clean" so the existing test
    # fixtures (which use a non-git tmp dir as repo_root) keep working.
    p4 = mock.patch.object(
        close_mod.git_ops, "lint_log_only_dirty", return_value=False
    )
    p5 = mock.patch.object(close_mod.git_ops, "stash_lint_log")
    p6 = mock.patch.object(close_mod.git_ops, "pop_stash")
    return p1, p2, p3, p4, p5, p6


# ── close() tests ────────────────────────────────────────────────────────────

class CloseHappyPathTest(unittest.TestCase):
    def test_happy_path_merges_and_finalises(self):
        gh = FakeGitHubAPI(_open_pr_clean(), post_merge_sha="merged_sha_xyz")
        jira = FakeJiraAPI()
        with tempfile.TemporaryDirectory() as tmp:
            repo_root = Path(tmp)
            p1, p2, p3, p4, p5, p6 = _patch_git_ops()
            with p1, p2, p3, p4, p5, p6:
                r = close_mod.close(
                    "SCRUM-999",
                    pr_number=999,
                    path_indicator=POLLING,
                    github_api=gh,
                    jira_api=jira,
                    repo_root=repo_root,
                )
        self.assertEqual(r.merged_sha, "merged_sha_xyz")
        self.assertEqual(gh.merge_calls, [(close_mod.DEFAULT_REPO, 999)])
        self.assertEqual(jira.transition_calls, [("SCRUM-999", "51")])
        self.assertTrue(r.jira_transitioned)
        self.assertTrue(r.branch_deleted)
        self.assertEqual(len(jira.comments), 1)
        # Closure comment matches the polling template.
        self.assertIn("polling path (default)", jira.comments[0][1])
        self.assertIn("merged_sha_xyz", jira.comments[0][1])


class CloseLintLogAutoStashTest(unittest.TestCase):
    """SCRUM-534: dirty lint log triggers stash + restore around checkout."""

    def test_actions_taken_records_stash_when_lint_log_dirty(self):
        gh = FakeGitHubAPI(_open_pr_clean(), post_merge_sha="merged_sha_lint")
        jira = FakeJiraAPI()
        with tempfile.TemporaryDirectory() as tmp:
            repo_root = Path(tmp)
            p1, p2, p3, p4, p5, p6 = _patch_git_ops()
            # Pretend the working tree has a dirty lint-runs.log; the patched
            # stash/pop are no-ops. close.py should call both and add the
            # explanatory line to actions_taken.
            p4 = mock.patch.object(
                close_mod.git_ops, "lint_log_only_dirty", return_value=True
            )
            with p1, p2, p3, p4 as ld_mock, p5 as stash_mock, p6 as pop_mock:
                r = close_mod.close(
                    "SCRUM-999",
                    pr_number=999,
                    path_indicator=POLLING,
                    github_api=gh,
                    jira_api=jira,
                    repo_root=repo_root,
                )
        ld_mock.assert_called_once()
        stash_mock.assert_called_once()
        pop_mock.assert_called_once()
        # Stash precedes checkout, pop follows it — the explanatory line must
        # appear in actions_taken after the SHA-change line.
        stash_line = next(
            (a for a in r.actions_taken if "stashed and restored" in a), None
        )
        self.assertIsNotNone(stash_line)
        self.assertIn("ops/define-kpis/lint-runs.log", stash_line)
        self.assertIn("SCRUM-534", stash_line)

    def test_clean_tree_skips_stash_path(self):
        gh = FakeGitHubAPI(_open_pr_clean(), post_merge_sha="merged_sha_clean")
        jira = FakeJiraAPI()
        with tempfile.TemporaryDirectory() as tmp:
            repo_root = Path(tmp)
            p1, p2, p3, p4, p5, p6 = _patch_git_ops()
            with p1, p2, p3, p4, p5 as stash_mock, p6 as pop_mock:
                r = close_mod.close(
                    "SCRUM-999",
                    pr_number=999,
                    path_indicator=POLLING,
                    github_api=gh,
                    jira_api=jira,
                    repo_root=repo_root,
                )
        stash_mock.assert_not_called()
        pop_mock.assert_not_called()
        stash_line = next(
            (a for a in r.actions_taken if "stashed and restored" in a), None
        )
        self.assertIsNone(stash_line)


class CloseAbortTest(unittest.TestCase):
    def test_pre_merge_guard_aborts_on_blocked(self):
        gh = FakeGitHubAPI(_open_pr_blocked())
        jira = FakeJiraAPI()
        with tempfile.TemporaryDirectory() as tmp:
            r = close_mod.close(
                "SCRUM-999",
                pr_number=999,
                github_api=gh,
                jira_api=jira,
                repo_root=Path(tmp),
            )
        self.assertIsNotNone(r.aborted_reason)
        self.assertIn("blocked", r.aborted_reason)
        self.assertEqual(gh.merge_calls, [])
        self.assertEqual(jira.transition_calls, [])
        self.assertEqual(jira.comments, [])
        # Pre-merge guard ran exactly once.
        self.assertEqual(gh.read_calls, 1)


class CloseManualOverrideTest(unittest.TestCase):
    def test_already_merged_pr_reconciles_without_calling_merge(self):
        gh = FakeGitHubAPI(_merged_pr(sha="user_pre_merged"))
        jira = FakeJiraAPI()
        with tempfile.TemporaryDirectory() as tmp:
            p1, p2, p3, p4, p5, p6 = _patch_git_ops()
            with p1, p2, p3, p4, p5, p6:
                r = close_mod.close(
                    "SCRUM-999",
                    pr_number=999,
                    path_indicator=MANUAL_OVERRIDE,
                    github_api=gh,
                    jira_api=jira,
                    repo_root=Path(tmp),
                )
        self.assertEqual(r.merged_sha, "user_pre_merged")
        self.assertEqual(gh.merge_calls, [])  # Did NOT call merge_pr
        self.assertEqual(jira.transition_calls, [("SCRUM-999", "51")])
        self.assertIn("user-override squash-merge", jira.comments[0][1])


class CloseDryRunTest(unittest.TestCase):
    def test_dry_run_makes_no_mutations(self):
        gh = FakeGitHubAPI(_open_pr_clean())
        jira = FakeJiraAPI()
        with tempfile.TemporaryDirectory() as tmp:
            r = close_mod.close(
                "SCRUM-999",
                pr_number=999,
                dry_run=True,
                github_api=gh,
                jira_api=jira,
                repo_root=Path(tmp),
            )
        # No mutations on either side.
        self.assertEqual(gh.merge_calls, [])
        self.assertEqual(jira.transition_calls, [])
        self.assertEqual(jira.comments, [])
        # But actions_taken describes the plan.
        joined = " | ".join(r.actions_taken)
        self.assertIn("would call merge_pr", joined)
        self.assertIn("would transition", joined)
        self.assertIn("would post closure comment", joined)
        self.assertTrue(r.dry_run)
        self.assertEqual(r.merged_sha, "<dry-run-merge-sha>")


class CloseInvalidPathTest(unittest.TestCase):
    def test_unknown_path_indicator_raises(self):
        with self.assertRaises(ValueError):
            close_mod.close("SCRUM-999", pr_number=1, path_indicator="bogus")


class CloseSummaryLineTest(unittest.TestCase):
    """SCRUM-536: actions_taken ends with a single grep-able summary line."""

    def test_pass_path_ends_with_succeeded_summary(self):
        gh = FakeGitHubAPI(_open_pr_clean(), post_merge_sha="merged_sha_536p")
        jira = FakeJiraAPI()
        with tempfile.TemporaryDirectory() as tmp:
            repo_root = Path(tmp)
            p1, p2, p3, p4, p5, p6 = _patch_git_ops()
            with p1, p2, p3, p4, p5, p6:
                r = close_mod.close(
                    "SCRUM-999",
                    pr_number=999,
                    path_indicator=POLLING,
                    github_api=gh,
                    jira_api=jira,
                    repo_root=repo_root,
                )
        last = r.actions_taken[-1]
        self.assertTrue(
            last.startswith("close.py succeeded:") and "no aborts" in last,
            f"unexpected summary line: {last!r}",
        )
        # N reflects the preceding entries, not including the summary itself.
        self.assertIn(f"{len(r.actions_taken) - 1} actions", last)

    def test_abort_path_ends_with_aborted_summary(self):
        gh = FakeGitHubAPI(_open_pr_blocked())
        jira = FakeJiraAPI()
        with tempfile.TemporaryDirectory() as tmp:
            r = close_mod.close(
                "SCRUM-999",
                pr_number=999,
                path_indicator=POLLING,
                github_api=gh,
                jira_api=jira,
                repo_root=Path(tmp),
            )
        last = r.actions_taken[-1]
        self.assertTrue(
            last.startswith("close.py aborted:"),
            f"unexpected summary line: {last!r}",
        )
        # The summary should embed the abort reason verbatim.
        self.assertIn(r.aborted_reason, last)

    def test_dry_run_ends_with_dry_run_summary(self):
        gh = FakeGitHubAPI(_open_pr_clean(), post_merge_sha="never_merges_in_dry_run")
        jira = FakeJiraAPI()
        with tempfile.TemporaryDirectory() as tmp:
            r = close_mod.close(
                "SCRUM-999",
                pr_number=999,
                path_indicator=POLLING,
                github_api=gh,
                jira_api=jira,
                repo_root=Path(tmp),
                dry_run=True,
            )
        last = r.actions_taken[-1]
        self.assertTrue(
            last.startswith("close.py dry-run:") and "previewed" in last,
            f"unexpected summary line: {last!r}",
        )

    def test_manual_override_ends_with_succeeded_summary(self):
        gh = FakeGitHubAPI(_merged_pr(sha="user_squashed_536"))
        jira = FakeJiraAPI()
        with tempfile.TemporaryDirectory() as tmp:
            p1, p2, p3, p4, p5, p6 = _patch_git_ops()
            with p1, p2, p3, p4, p5, p6:
                r = close_mod.close(
                    "SCRUM-999",
                    pr_number=999,
                    path_indicator=MANUAL_OVERRIDE,
                    github_api=gh,
                    jira_api=jira,
                    repo_root=Path(tmp),
                )
        last = r.actions_taken[-1]
        self.assertTrue(
            last.startswith("close.py succeeded:"),
            f"unexpected summary line: {last!r}",
        )


# ── state-file tests ─────────────────────────────────────────────────────────

class StateFileTest(unittest.TestCase):
    def _write_state(self, root: Path, epic: str, items: list[dict]) -> Path:
        state_dir = root / ".epic-run"
        state_dir.mkdir(parents=True, exist_ok=True)
        path = state_dir / f"{epic}.json"
        path.write_text(json.dumps({"epic": epic, "work_list": items}))
        return path

    def test_find_state_file_returns_match(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            self._write_state(root, "SCRUM-999",
                              [{"key": "SCRUM-1001", "status": "pending"}])
            self._write_state(root, "SCRUM-998",
                              [{"key": "SCRUM-2001", "status": "done"}])
            found = state_mod.find_state_file("SCRUM-1001", repo_root=root)
            self.assertIsNotNone(found)
            self.assertEqual(found.name, "SCRUM-999.json")

    def test_find_state_file_returns_none_when_no_match(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            self._write_state(root, "SCRUM-999", [{"key": "OTHER-1"}])
            self.assertIsNone(state_mod.find_state_file("MISSING-X", repo_root=root))

    def test_find_state_file_handles_no_dir(self):
        with tempfile.TemporaryDirectory() as tmp:
            self.assertIsNone(state_mod.find_state_file("ANY", repo_root=Path(tmp)))

    def test_mark_done_mutates_matching_item(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            path = self._write_state(
                root,
                "SCRUM-999",
                [{"key": "SCRUM-X", "status": "in_progress"}],
            )
            changed = state_mod.mark_done(
                path, "SCRUM-X", pr_number=42, merged_sha="aaa", final_gate="PASS"
            )
            self.assertTrue(changed)
            data = json.loads(path.read_text())
            item = data["work_list"][0]
            self.assertEqual(item["status"], "done")
            self.assertEqual(item["merged_sha"], "aaa")
            self.assertEqual(item["pr"], 42)
            self.assertEqual(item["final_gate"], "PASS")

    def test_mark_done_idempotent(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            path = self._write_state(
                root,
                "SCRUM-999",
                [{"key": "SCRUM-X", "status": "done", "merged_sha": "aaa", "pr": 42}],
            )
            changed = state_mod.mark_done(
                path, "SCRUM-X", pr_number=42, merged_sha="aaa", final_gate="PASS"
            )
            self.assertFalse(changed)  # no mutation needed


# ── template tests ───────────────────────────────────────────────────────────

class TemplateTest(unittest.TestCase):
    def _ctx(self):
        return ClosureContext(
            ticket="SCRUM-X",
            pr_number=42,
            merged_sha="abc1234",
            main_sha_after="def5678",
            final_gate_status="PASS",
            branch_name="feat/SCRUM-X",
        )

    def test_polling_template(self):
        s = templates_mod.render(POLLING, self._ctx())
        self.assertIn("FULL_AUTO complete — polling path", s)
        self.assertIn("PR #42", s)
        self.assertIn("abc1234", s)
        self.assertIn("def5678", s)
        self.assertIn("feat/SCRUM-X", s)

    def test_manual_override_template(self):
        s = templates_mod.render(MANUAL_OVERRIDE, self._ctx())
        self.assertIn("user-override squash-merge", s)
        self.assertIn("Did NOT call merge_pull_request", s)
        self.assertIn("abc1234", s)

    def test_webhook_template(self):
        s = templates_mod.render(WEBHOOK, self._ctx())
        self.assertIn("webhook path", s)
        self.assertIn("FULL_AUTO_WEBHOOK", s)
        self.assertIn("Local cleanup skipped", s)

    def test_unknown_path_raises(self):
        with self.assertRaises(ValueError):
            templates_mod.render("nope", self._ctx())


if __name__ == "__main__":
    unittest.main()
