#!/usr/bin/env python3
"""Unit tests for scripts/jira_ticket_lint.py (SCRUM-490)."""

from __future__ import annotations

import io
import json
import sys
import tempfile
import unittest
from contextlib import redirect_stdout
from pathlib import Path

_REPO_ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(_REPO_ROOT / "scripts"))

import jira_ticket_lint as lint  # noqa: E402


def _rule_ids(result) -> list[str]:
    return [g.rule_id for g in result.gaps]


# ---- Fixture descriptions ----

STORY_OK = """## Context

User wants to do a thing.

## Acceptance criteria

- [ ] Behavior A
- [ ] Behavior B
- [ ] Behavior C
"""

STORY_MISSING_AC = """## Context

User wants to do a thing.

## Out of scope

- Something
"""

STORY_TOO_FEW_AC = """## Acceptance criteria

- [ ] Only behavior A
- [ ] Only behavior B
"""

TASK_OK = """## Context

Refactor the foo.

## Acceptance criteria

- [ ] Foo is now bar
"""

BUG_OK = """## Observed

Crash on click.

## Expected

No crash.

## Reproduction

1. Click the thing
2. See the crash

## Impact

Some users affected.
"""

BUG_NO_REPRO = """## Observed

Crash on click.

## Expected

No crash.
"""

EPIC_OK = """## Goal

Ship the thing.

## Scope

- Do A
- Do B

## Non-goals

- Don't do C

## Success criteria

- [ ] A is done
- [ ] B is done
"""

EPIC_NO_GOAL = """## Scope

- Do A

## Success criteria

- [ ] X
- [ ] Y
"""

EPIC_NO_SCOPE = """## Goal

Ship it.

## Success criteria

- [ ] X
- [ ] Y
"""

EPIC_SC_ONE_CHECKBOX = """## Goal

Ship it.

## Scope

- Do A

## Success criteria

- [ ] Only one
"""

LOWERCASE_HEADER_STORY = """## Context

Thing.

## acceptance criteria

- [ ] alpha
- [ ] beta
- [ ] gamma
"""

LEVEL3_HEADER_STORY = """# Document title

### Acceptance criteria

- [ ] x
- [ ] y
- [ ] z
"""

BOLD_HEADER_STORY = """**Context**: thing

**Acceptance criteria**

- [ ] one
- [ ] two
- [ ] three
"""

EPIC_MULTI_GAP = """## Goal

Maybe?
"""

# SCRUM-504: PR-mode fixtures derived from the canonical PR body format used
# across SCRUM-485 → SCRUM-503 (the agent-authored uplift PRs all follow it).

PR_OK = """## Jira

SCRUM-499

## Summary

- New thing shipped.
- Old thing improved.

## Test plan

- [x] Tests pass.
- [ ] CI green.

## Risks / follow-ups

- Edge case noted.
"""

PR_MISSING_JIRA = """## Summary

- Bullet here.

## Test plan

- [ ] Tested.
"""

PR_MISSING_SUMMARY = """## Jira

SCRUM-100

## Test plan

- [ ] Tested.
"""

PR_MISSING_TEST_PLAN = """## Jira

SCRUM-100

## Summary

- Bullet.
"""

PR_EMPTY_SUMMARY = """## Jira

SCRUM-100

## Summary

## Test plan

- [ ] Tested.
"""

PR_EMPTY_TEST_PLAN = """## Jira

SCRUM-100

## Summary

- Bullet here.

## Test plan

(none yet)
"""

PR_JIRA_LINK_IN_BODY_TEXT = """## Summary

This change is required by SCRUM-42 (link shows up in prose, not in a Jira section).

- Bullet.

## Test plan

- [x] Done.
"""


class TestPassCases(unittest.TestCase):
    def test_story_ok(self):
        result = lint.lint(STORY_OK, "Story")
        self.assertEqual(result.exit_code, 0)
        self.assertEqual(result.gaps, [])

    def test_task_ok(self):
        result = lint.lint(TASK_OK, "Task")
        self.assertEqual(result.exit_code, 0)
        self.assertEqual(result.gaps, [])

    def test_bug_ok(self):
        result = lint.lint(BUG_OK, "Bug")
        self.assertEqual(result.exit_code, 0)
        self.assertEqual(result.gaps, [])

    def test_epic_ok(self):
        result = lint.lint(EPIC_OK, "Epic")
        self.assertEqual(result.exit_code, 0)
        self.assertEqual(result.gaps, [])


class TestFailCases(unittest.TestCase):
    def test_story_missing_ac(self):
        result = lint.lint(STORY_MISSING_AC, "Story")
        self.assertEqual(result.exit_code, 2)
        self.assertIn("AC.present", _rule_ids(result))

    def test_story_too_few_ac(self):
        result = lint.lint(STORY_TOO_FEW_AC, "Story")
        self.assertEqual(result.exit_code, 2)
        self.assertIn("AC.min_count", _rule_ids(result))
        self.assertNotIn(
            "AC.present",
            _rule_ids(result),
            "AC.present must not fire when AC has at least one checkbox",
        )

    def test_bug_no_repro(self):
        result = lint.lint(BUG_NO_REPRO, "Bug")
        self.assertEqual(result.exit_code, 2)
        self.assertIn("BUG.repro", _rule_ids(result))

    def test_epic_missing_goal(self):
        result = lint.lint(EPIC_NO_GOAL, "Epic")
        self.assertEqual(result.exit_code, 2)
        self.assertIn("EPIC.goal", _rule_ids(result))

    def test_epic_missing_scope(self):
        result = lint.lint(EPIC_NO_SCOPE, "Epic")
        self.assertEqual(result.exit_code, 2)
        self.assertIn("EPIC.scope_present", _rule_ids(result))

    def test_epic_sc_one_checkbox(self):
        result = lint.lint(EPIC_SC_ONE_CHECKBOX, "Epic")
        self.assertEqual(result.exit_code, 2)
        self.assertIn("EPIC.success_criteria", _rule_ids(result))


class TestPRMode(unittest.TestCase):
    """SCRUM-504: --issue-type PR routes through 3 new rules."""

    def test_pr_ok(self):
        result = lint.lint(PR_OK, "PR")
        self.assertEqual(result.exit_code, 0, result.gaps)

    def test_pr_missing_jira_link(self):
        result = lint.lint(PR_MISSING_JIRA, "PR")
        self.assertEqual(result.exit_code, 2)
        self.assertIn("PR.jira_link", _rule_ids(result))

    def test_pr_missing_summary(self):
        result = lint.lint(PR_MISSING_SUMMARY, "PR")
        self.assertEqual(result.exit_code, 2)
        self.assertIn("PR.summary", _rule_ids(result))

    def test_pr_missing_test_plan(self):
        result = lint.lint(PR_MISSING_TEST_PLAN, "PR")
        self.assertEqual(result.exit_code, 2)
        self.assertIn("PR.test_plan", _rule_ids(result))

    def test_pr_empty_summary_section(self):
        result = lint.lint(PR_EMPTY_SUMMARY, "PR")
        self.assertEqual(result.exit_code, 2)
        self.assertIn("PR.summary", _rule_ids(result))

    def test_pr_empty_test_plan_section(self):
        result = lint.lint(PR_EMPTY_TEST_PLAN, "PR")
        self.assertEqual(result.exit_code, 2)
        self.assertIn("PR.test_plan", _rule_ids(result))

    def test_pr_jira_link_in_body_text_counts(self):
        """A SCRUM-N reference anywhere in the body satisfies PR.jira_link.
        Per the rule's intent — Jira section is a convention, but the
        requirement is the *link* exists, not the section heading.
        """
        result = lint.lint(PR_JIRA_LINK_IN_BODY_TEXT, "PR")
        # PR.jira_link should NOT fire (link is in prose).
        self.assertNotIn("PR.jira_link", _rule_ids(result))

    def test_pr_jira_rules_dont_apply_to_other_types(self):
        # The 3 new PR rules should not affect Story / Task / Epic / Bug lint.
        # STORY_OK has no Jira link or Test plan section but should still pass.
        for t in ("Story", "Task", "Epic", "Bug"):
            result = lint.lint(STORY_OK if t == "Story" else BUG_OK if t == "Bug" else TASK_OK if t == "Task" else EPIC_OK, t)
            self.assertNotIn("PR.jira_link", _rule_ids(result))
            self.assertNotIn("PR.summary", _rule_ids(result))
            self.assertNotIn("PR.test_plan", _rule_ids(result))


class TestStructural(unittest.TestCase):
    def test_empty_description_is_unfixable(self):
        result = lint.lint("", "Story")
        self.assertEqual(result.exit_code, 1)
        self.assertFalse(result.fixable)
        self.assertIn("STRUCT.empty", _rule_ids(result))

    def test_whitespace_only_description_is_unfixable(self):
        result = lint.lint("\n\n   \n", "Story")
        self.assertEqual(result.exit_code, 1)
        self.assertIn("STRUCT.empty", _rule_ids(result))

    def test_invalid_issue_type_is_unfixable(self):
        result = lint.lint("anything", "Subtask")
        self.assertEqual(result.exit_code, 1)
        self.assertIn("STRUCT.bad_type", _rule_ids(result))


class TestHeaderTolerance(unittest.TestCase):
    def test_lowercase_section_header_matches(self):
        result = lint.lint(LOWERCASE_HEADER_STORY, "Story")
        self.assertEqual(result.exit_code, 0, result.gaps)

    def test_level3_section_header_matches(self):
        result = lint.lint(LEVEL3_HEADER_STORY, "Story")
        self.assertEqual(result.exit_code, 0, result.gaps)

    def test_bold_section_header_matches(self):
        result = lint.lint(BOLD_HEADER_STORY, "Story")
        self.assertEqual(result.exit_code, 0, result.gaps)


class TestMultipleGaps(unittest.TestCase):
    def test_epic_with_only_goal_reports_scope_and_sc(self):
        result = lint.lint(EPIC_MULTI_GAP, "Epic")
        self.assertEqual(result.exit_code, 2)
        rule_ids = set(_rule_ids(result))
        self.assertIn("EPIC.scope_present", rule_ids)
        self.assertIn("EPIC.success_criteria", rule_ids)


class TestLogWrite(unittest.TestCase):
    def test_log_writes_jsonl_row(self):
        with tempfile.TemporaryDirectory() as tmp:
            log = Path(tmp) / "lint-runs.log"
            result = lint.lint(STORY_OK, "Story")
            lint._log_run(log, "SCRUM-X", "Story", result)
            lines = log.read_text().splitlines()
            self.assertEqual(len(lines), 1)
            row = json.loads(lines[0])
            self.assertEqual(row["ticket"], "SCRUM-X")
            self.assertEqual(row["issue_type"], "Story")
            self.assertEqual(row["exit"], 0)
            self.assertEqual(row["gaps"], [])

    def test_log_appends_multiple_rows(self):
        with tempfile.TemporaryDirectory() as tmp:
            log = Path(tmp) / "lint-runs.log"
            for _ in range(3):
                result = lint.lint(STORY_OK, "Story")
                lint._log_run(log, "SCRUM-Y", "Story", result)
            self.assertEqual(len(log.read_text().splitlines()), 3)


class TestCLI(unittest.TestCase):
    def test_main_exits_0_on_pass(self):
        with tempfile.TemporaryDirectory() as tmp:
            desc = Path(tmp) / "desc.md"
            desc.write_text(STORY_OK)
            log = Path(tmp) / "lint-runs.log"
            buf = io.StringIO()
            with redirect_stdout(buf):
                rc = lint.main(
                    [
                        "--description-file",
                        str(desc),
                        "--issue-type",
                        "Story",
                        "--ticket",
                        "SCRUM-T",
                        "--log",
                        str(log),
                    ]
                )
            self.assertEqual(rc, 0)
            payload = json.loads(buf.getvalue())
            self.assertTrue(payload.get("pass"))
            self.assertTrue(log.is_file())

    def test_main_exits_2_with_structured_gaps(self):
        with tempfile.TemporaryDirectory() as tmp:
            desc = Path(tmp) / "desc.md"
            desc.write_text(STORY_TOO_FEW_AC)
            buf = io.StringIO()
            with redirect_stdout(buf):
                rc = lint.main(
                    [
                        "--description-file",
                        str(desc),
                        "--issue-type",
                        "Story",
                        "--log",
                        "",
                    ]
                )
            self.assertEqual(rc, 2)
            payload = json.loads(buf.getvalue())
            self.assertTrue(payload["fixable"])
            self.assertTrue(
                any(g["rule_id"] == "AC.min_count" for g in payload["gaps"])
            )


if __name__ == "__main__":
    unittest.main()
