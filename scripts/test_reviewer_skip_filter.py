#!/usr/bin/env python3
"""Unit tests for scripts/reviewer/skip_filter.py (SCRUM-510)."""

from __future__ import annotations

import os
import sys
import unittest
from pathlib import Path

_REPO_ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(_REPO_ROOT / "scripts"))

from reviewer.skip_filter import (  # noqa: E402
    DEFAULT_MIN_SOURCE_LOC,
    PRMetadata,
    should_skip,
)


def _pr(**overrides) -> PRMetadata:
    """Build a baseline PRMetadata that does NOT skip, then override."""
    defaults = dict(
        number=1,
        title="Add new feature",
        author_login="humandev",
        draft=False,
        labels=[],
        changed_files=[
            "internal/handlers/foo.go",
            "internal/handlers/bar.go",
            "internal/database/baz.go",
        ],
        additions=200,
        deletions=50,
    )
    defaults.update(overrides)
    return PRMetadata(**defaults)


class SkipFilterTest(unittest.TestCase):
    def test_baseline_pr_is_reviewed(self):
        skip, reason = should_skip(_pr())
        self.assertFalse(skip)
        self.assertEqual(reason, "")

    def test_draft_pr_is_skipped(self):
        skip, reason = should_skip(_pr(draft=True))
        self.assertTrue(skip)
        self.assertEqual(reason, "draft")

    def test_skip_reviewer_label_skipped(self):
        skip, reason = should_skip(_pr(labels=["skip-reviewer"]))
        self.assertTrue(skip)
        self.assertEqual(reason, "skip_label")

    def test_dependabot_author_skipped(self):
        skip, reason = should_skip(_pr(author_login="dependabot[bot]"))
        self.assertTrue(skip)
        self.assertEqual(reason, "bot_author")

    def test_renovate_author_skipped(self):
        skip, reason = should_skip(_pr(author_login="renovate[bot]"))
        self.assertTrue(skip)
        self.assertEqual(reason, "bot_author")

    def test_github_actions_bot_skipped(self):
        skip, reason = should_skip(_pr(author_login="github-actions[bot]"))
        self.assertTrue(skip)
        self.assertEqual(reason, "bot_author")

    def test_bot_lookalike_not_skipped(self):
        """A user with 'bot' in the name but not in the bot list is reviewed."""
        skip, reason = should_skip(_pr(author_login="robotuser"))
        self.assertFalse(skip)
        self.assertEqual(reason, "")

    def test_docs_only_md_files_skipped(self):
        skip, reason = should_skip(
            _pr(changed_files=["README.md", "docs/intro.md", "CHANGELOG.md"])
        )
        self.assertTrue(skip)
        self.assertEqual(reason, "docs_only")

    def test_docs_only_docs_subdir_skipped(self):
        skip, reason = should_skip(
            _pr(changed_files=["docs/agent/overview.md", "docs/specs/template.md"])
        )
        self.assertTrue(skip)
        self.assertEqual(reason, "docs_only")

    def test_docs_only_license_skipped(self):
        skip, reason = should_skip(_pr(changed_files=["LICENSE", "LICENSE.txt"]))
        self.assertTrue(skip)
        self.assertEqual(reason, "docs_only")

    def test_one_source_file_breaks_docs_only(self):
        # additions+deletions = 400, source_files=1, total_files=2 →
        # source_loc estimate = 200, above default 100 threshold.
        skip, reason = should_skip(
            _pr(
                changed_files=["README.md", "internal/handlers/foo.go"],
                additions=350,
                deletions=50,
            )
        )
        # Mixed PR with substantial source change — not docs_only, not under threshold.
        self.assertFalse(skip)
        self.assertEqual(reason, "")

    def test_under_loc_threshold_skipped(self):
        skip, reason = should_skip(
            _pr(
                changed_files=["internal/handlers/foo.go"],
                additions=20,
                deletions=5,
            )
        )
        self.assertTrue(skip)
        self.assertEqual(reason, "under_loc_threshold")

    def test_over_loc_threshold_reviewed(self):
        skip, reason = should_skip(
            _pr(
                changed_files=["internal/handlers/foo.go"],
                additions=200,
                deletions=20,
            )
        )
        self.assertFalse(skip)

    def test_explicit_min_source_loc_override(self):
        # Same diff, lower threshold means we no longer skip.
        skip_default, _ = should_skip(
            _pr(
                changed_files=["internal/handlers/foo.go"],
                additions=50,
                deletions=10,
            )
        )
        self.assertTrue(skip_default)  # default 100, total 60 → skip
        skip_low, reason_low = should_skip(
            _pr(
                changed_files=["internal/handlers/foo.go"],
                additions=50,
                deletions=10,
            ),
            min_source_loc=20,
        )
        self.assertFalse(skip_low)
        self.assertEqual(reason_low, "")

    def test_env_var_min_source_loc(self):
        os.environ["REVIEWER_MIN_SOURCE_LOC"] = "10"
        try:
            skip, _ = should_skip(
                _pr(
                    changed_files=["internal/handlers/foo.go"],
                    additions=50,
                    deletions=10,
                )
            )
            self.assertFalse(skip)
        finally:
            os.environ.pop("REVIEWER_MIN_SOURCE_LOC", None)

    def test_first_matching_rule_wins(self):
        """A draft PR with skip-reviewer label and bot author reports 'draft'."""
        skip, reason = should_skip(
            _pr(
                draft=True,
                labels=["skip-reviewer"],
                author_login="dependabot[bot]",
            )
        )
        self.assertTrue(skip)
        self.assertEqual(reason, "draft")

    def test_test_files_excluded_from_source_loc(self):
        """Tests and lockfiles do not count as source, so a test-only PR skips."""
        skip, reason = should_skip(
            _pr(
                changed_files=[
                    "web/src/test/foo.test.jsx",
                    "scripts/test_bar.py",
                    "package-lock.json",
                ],
                additions=500,
                deletions=100,
            )
        )
        # All files are non-source → source_loc = 0 → under threshold
        self.assertTrue(skip)
        self.assertEqual(reason, "under_loc_threshold")

    def test_custom_bot_authors_override(self):
        """A caller can extend the bot list at runtime."""
        skip, reason = should_skip(
            _pr(author_login="custombot[bot]"),
            bot_authors={"custombot[bot]"},
        )
        self.assertTrue(skip)
        self.assertEqual(reason, "bot_author")
        # Default bots are NOT included when caller provides explicit list.
        skip2, _ = should_skip(
            _pr(author_login="dependabot[bot]"),
            bot_authors={"custombot[bot]"},
        )
        self.assertFalse(skip2)

    def test_empty_changed_files_not_docs_only(self):
        """A PR with no files is not docs-only (no files → no docs)."""
        skip, reason = should_skip(
            _pr(changed_files=[], additions=0, deletions=0)
        )
        # Falls through docs check, hits LOC threshold (0 < default)
        self.assertTrue(skip)
        self.assertEqual(reason, "under_loc_threshold")


if __name__ == "__main__":
    unittest.main()
