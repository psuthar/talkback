#!/usr/bin/env python3
"""SCRUM-542: unit + e2e tests for ``scripts/full_auto/start.py``."""

from __future__ import annotations

import json
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path
from unittest import mock

_REPO_ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(_REPO_ROOT / "scripts"))

from full_auto import start as start_mod  # noqa: E402
from full_auto.lib import adf as adf_mod  # noqa: E402


# ── ADF → MD converter ──────────────────────────────────────────────────────

class AdfToMdTest(unittest.TestCase):
    def test_handles_heading_nodes(self):
        adf = {
            "type": "doc",
            "content": [
                {"type": "heading", "attrs": {"level": 2}, "content": [{"type": "text", "text": "AC"}]},
                {"type": "paragraph", "content": [{"type": "text", "text": "body"}]},
            ],
        }
        md = adf_mod.adf_to_md(adf)
        self.assertIn("## AC", md)
        self.assertIn("body", md)

    def test_handles_task_list(self):
        adf = {
            "type": "doc",
            "content": [
                {
                    "type": "taskList",
                    "content": [
                        {"type": "taskItem", "attrs": {"state": "TODO"}, "content": [{"type": "text", "text": "a"}]},
                        {"type": "taskItem", "attrs": {"state": "DONE"}, "content": [{"type": "text", "text": "b"}]},
                    ],
                }
            ],
        }
        md = adf_mod.adf_to_md(adf)
        self.assertIn("- [ ] a", md)
        self.assertIn("- [x] b", md)

    def test_handles_paragraph_with_embedded_markdown(self):
        # Common shape produced when the agent updates the description with
        # raw Markdown — the entire body lands in a single text node.
        adf = {
            "type": "doc",
            "content": [
                {
                    "type": "paragraph",
                    "content": [{"type": "text", "text": "## Acceptance criteria\n\n- [ ] one\n"}],
                }
            ],
        }
        md = adf_mod.adf_to_md(adf)
        self.assertIn("## Acceptance criteria", md)
        self.assertIn("- [ ] one", md)

    def test_non_dict_input_returns_empty(self):
        self.assertEqual(adf_mod.adf_to_md(None), "")
        self.assertEqual(adf_mod.adf_to_md(""), "")


# ── description patch ───────────────────────────────────────────────────────

class PatchDescriptionTest(unittest.TestCase):
    def test_adds_missing_ac_section(self):
        original = "## Context\n\nSomething.\n"
        gaps = [{"rule_id": "AC.present", "section": "Acceptance criteria"}]
        out = start_mod._patch_description(original, gaps)
        self.assertIn("## Acceptance criteria", out)
        self.assertIn("- [ ] (fill in)", out)

    def test_pads_ac_to_min_count_for_story(self):
        original = "## Acceptance criteria\n\n- [ ] one\n"
        gaps = [{"rule_id": "AC.min_count", "section": "Acceptance criteria"}]
        out = start_mod._patch_description(original, gaps)
        # Should now have 3 total checkboxes (one existing + two added).
        self.assertEqual(out.count("- [ "), 3)

    def test_fills_empty_reproduction_section(self):
        original = "## Reproduction\n\n## Next\n\nbody\n"
        gaps = [{"rule_id": "BUG.repro", "section": "Reproduction"}]
        out = start_mod._patch_description(original, gaps)
        # Placeholder should land between the two headings.
        self.assertIn("(fill in)", out)
        repro_idx = out.index("## Reproduction")
        next_idx = out.index("## Next")
        self.assertIn("(fill in)", out[repro_idx:next_idx])


# ── start() flow ────────────────────────────────────────────────────────────

class _FakeJiraAPI:
    def __init__(self, issue: dict, transitions: list[dict] | None = None):
        self._issue = issue
        self._transitions = transitions or [
            {"id": "21", "name": "In Progress"},
            {"id": "51", "name": "Done"},
        ]
        self.updates: list[tuple[str, dict]] = []
        self.transitions_taken: list[tuple[str, str]] = []

    def get_issue(self, key):
        return dict(self._issue)

    def get_transitions(self, key):
        return self._transitions

    def transition(self, key, transition_id):
        self.transitions_taken.append((key, transition_id))

    def update_issue(self, key, fields):
        self.updates.append((key, fields))
        # Replicate the patched description on subsequent reads.
        if "description" in fields:
            self._issue = dict(self._issue)
            self._issue["description"] = {
                "type": "doc",
                "version": 1,
                "content": [
                    {"type": "paragraph", "content": [{"type": "text", "text": fields["description"]}]}
                ],
            }

    def add_comment(self, key, body):
        raise NotImplementedError("start.py does not post comments")


def _seed_repo(td: Path) -> Path:
    """Bare origin + work clone, both seeded with one commit on main."""
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
    return work


def _adf_with_markdown(md: str) -> dict:
    return {
        "type": "doc",
        "version": 1,
        "content": [{"type": "paragraph", "content": [{"type": "text", "text": md}]}],
    }


class StartHappyPathTest(unittest.TestCase):
    def test_lint_pass_transitions_and_branches(self):
        with tempfile.TemporaryDirectory() as td:
            work = _seed_repo(Path(td))
            jira = _FakeJiraAPI(
                {
                    "key": "SCRUM-999",
                    "summary": "test ticket",
                    "issuetype": "Task",
                    "labels": ["agent-authored"],
                    "description": _adf_with_markdown(
                        "## Acceptance criteria\n\n- [ ] something\n"
                    ),
                    "status": "To Do",
                }
            )
            result = start_mod.start("SCRUM-999", jira_api=jira, repo_root=work)
            self.assertIsNone(result.aborted_reason, result.actions_taken)
            self.assertEqual(result.lint_status, "pass")
            self.assertTrue(result.jira_transitioned)
            self.assertEqual(result.branch_name, "feat/SCRUM-999")
            self.assertEqual(jira.transitions_taken, [("SCRUM-999", "21")])
            self.assertTrue(result.actions_taken[-1].startswith("start.py succeeded:"))
            # Branch actually exists.
            branches = subprocess.run(
                ["git", "branch"], cwd=str(work), capture_output=True, text=True, check=True
            ).stdout
            self.assertIn("feat/SCRUM-999", branches)


class StartLintFailTest(unittest.TestCase):
    def test_human_authored_exit2_halts_without_patch(self):
        with tempfile.TemporaryDirectory() as td:
            work = _seed_repo(Path(td))
            jira = _FakeJiraAPI(
                {
                    "key": "SCRUM-998",
                    "summary": "no AC",
                    "issuetype": "Task",
                    "labels": [],  # NOT agent-authored
                    "description": _adf_with_markdown("## Context\n\nno AC section here.\n"),
                    "status": "To Do",
                }
            )
            result = start_mod.start("SCRUM-998", jira_api=jira, repo_root=work)
            self.assertIsNotNone(result.aborted_reason)
            self.assertEqual(result.lint_status, "halted_gaps")
            self.assertFalse(result.jira_transitioned)
            self.assertEqual(jira.updates, [])
            # No branch created.
            branches = subprocess.run(
                ["git", "branch"], cwd=str(work), capture_output=True, text=True, check=True
            ).stdout
            self.assertNotIn("feat/SCRUM-998", branches)
            self.assertTrue(result.actions_taken[-1].startswith("start.py aborted:"))

    def test_agent_authored_exit2_patches_then_passes(self):
        with tempfile.TemporaryDirectory() as td:
            work = _seed_repo(Path(td))
            jira = _FakeJiraAPI(
                {
                    "key": "SCRUM-997",
                    "summary": "missing AC",
                    "issuetype": "Task",
                    "labels": ["agent-authored"],
                    "description": _adf_with_markdown("## Context\n\nbody but no AC.\n"),
                    "status": "To Do",
                }
            )
            result = start_mod.start("SCRUM-997", jira_api=jira, repo_root=work)
            self.assertIsNone(result.aborted_reason, result.actions_taken)
            self.assertEqual(result.lint_status, "patched_then_pass")
            self.assertEqual(len(jira.updates), 1)
            self.assertIn("Acceptance criteria", jira.updates[0][1]["description"])
            self.assertTrue(result.jira_transitioned)


class StartIdempotentBranchTest(unittest.TestCase):
    def test_existing_branch_is_checked_out_not_recreated(self):
        with tempfile.TemporaryDirectory() as td:
            work = _seed_repo(Path(td))
            # Pre-create the branch.
            subprocess.run(
                ["git", "branch", "feat/SCRUM-996"], cwd=str(work), check=True
            )
            jira = _FakeJiraAPI(
                {
                    "key": "SCRUM-996",
                    "summary": "existing branch",
                    "issuetype": "Task",
                    "labels": ["agent-authored"],
                    "description": _adf_with_markdown(
                        "## Acceptance criteria\n\n- [ ] x\n"
                    ),
                    "status": "To Do",
                }
            )
            result = start_mod.start("SCRUM-996", jira_api=jira, repo_root=work)
            self.assertIsNone(result.aborted_reason)
            current = subprocess.run(
                ["git", "branch", "--show-current"], cwd=str(work), capture_output=True, text=True, check=True
            ).stdout.strip()
            self.assertEqual(current, "feat/SCRUM-996")
            # Look for the "checked out existing" line in actions_taken.
            self.assertTrue(any("checked out existing" in a for a in result.actions_taken))


class StartDryRunTest(unittest.TestCase):
    def test_dry_run_no_mutations(self):
        with tempfile.TemporaryDirectory() as td:
            work = _seed_repo(Path(td))
            jira = _FakeJiraAPI(
                {
                    "key": "SCRUM-995",
                    "summary": "dry",
                    "issuetype": "Task",
                    "labels": ["agent-authored"],
                    "description": _adf_with_markdown(
                        "## Acceptance criteria\n\n- [ ] x\n"
                    ),
                    "status": "To Do",
                }
            )
            result = start_mod.start("SCRUM-995", dry_run=True, jira_api=jira, repo_root=work)
            self.assertIsNone(result.aborted_reason)
            self.assertEqual(jira.transitions_taken, [])
            self.assertEqual(jira.updates, [])
            branches = subprocess.run(
                ["git", "branch"], cwd=str(work), capture_output=True, text=True, check=True
            ).stdout
            self.assertNotIn("feat/SCRUM-995", branches)
            self.assertTrue(result.actions_taken[-1].startswith("start.py dry-run:"))


if __name__ == "__main__":
    unittest.main()
