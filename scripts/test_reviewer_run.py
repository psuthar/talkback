#!/usr/bin/env python3
"""Unit tests for scripts/reviewer/run.py (SCRUM-514)."""

from __future__ import annotations

import sys
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

_REPO_ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(_REPO_ROOT / "scripts"))

from reviewer.budget import (  # noqa: E402
    BUDGET_EXHAUSTED_COMMENT,
    InMemoryBudgetStore,
)
from reviewer.run import (  # noqa: E402
    REFUSAL_TOKEN,
    PRContent,
    StalePromptPinError,
    estimate_tokens,
    load_prompt,
    run_review,
)


class FakeGitHubClient:
    def __init__(self, pr: PRContent):
        self._pr = pr
        self.posted: list[tuple[int, str]] = []

    def fetch_pr(self, repo: str, pr_number: int) -> PRContent:
        return self._pr

    def post_pr_comment(self, repo: str, pr_number: int, body: str) -> int:
        self.posted.append((pr_number, body))
        return 1000 + len(self.posted)


def _fake_pr(**overrides) -> PRContent:
    defaults = dict(
        number=42,
        title="Add cool feature",
        description="Implements the cool feature.",
        diff=(
            "diff --git a/internal/foo.go b/internal/foo.go\n"
            "@@ -1,3 +1,5 @@\n"
            "+func Bar() int { return 1 }\n"
        ),
        changed_files=["internal/foo.go"],
        head_sha="abc123",
    )
    defaults.update(overrides)
    return PRContent(**defaults)


def _model_returns(text: str):
    def call(model, system, user):
        return text
    return call


class LoadPromptTest(unittest.TestCase):
    def test_load_real_prompt_passes_pin(self):
        # Real PROMPT.md in the repo; pin must match the current SCOPE.md HEAD
        # (the pin is bumped in the same PR as any SCOPE.md change).
        content, sha = load_prompt(enforce_pin=True)
        self.assertIn("SCOPE.md@", content)
        self.assertTrue(len(sha) >= 7)

    def test_stale_pin_raises(self):
        with tempfile.TemporaryDirectory() as tmp:
            bad = Path(tmp) / "PROMPT.md"
            bad.write_text("Authored against: SCOPE.md@deadbeefdead — and a body.")
            with self.assertRaises(StalePromptPinError):
                load_prompt(bad, enforce_pin=True)

    def test_missing_pin_raises(self):
        with tempfile.TemporaryDirectory() as tmp:
            bad = Path(tmp) / "PROMPT.md"
            bad.write_text("No pin here, just instructions.")
            with self.assertRaises(StalePromptPinError):
                load_prompt(bad, enforce_pin=False)

    def test_enforce_pin_false_skips_comparison(self):
        with tempfile.TemporaryDirectory() as tmp:
            bad = Path(tmp) / "PROMPT.md"
            bad.write_text("SCOPE.md@aaaaaaa — stale but enforce off.")
            content, sha = load_prompt(bad, enforce_pin=False)
            self.assertEqual(sha, "aaaaaaa")


class EstimateTokensTest(unittest.TestCase):
    def test_grows_with_diff_size(self):
        a = estimate_tokens("a" * 30, "prompt")
        b = estimate_tokens("a" * 300, "prompt")
        self.assertLess(a, b)

    def test_includes_prompt_overhead(self):
        # Even an empty diff costs the prompt + fixed overhead.
        self.assertGreater(estimate_tokens("", "system prompt"), 500)


class RunReviewTest(unittest.TestCase):
    def test_happy_path_posts_findings(self):
        store = InMemoryBudgetStore()
        client = FakeGitHubClient(_fake_pr())
        model_output = "## Findings\n\n- `internal/foo.go:1` — new public func without a test."
        r = run_review(
            42,
            "owner/repo",
            github_client=client,
            model_client=_model_returns(model_output),
            store=store,
            enforce_pin=True,
        )
        self.assertTrue(r.posted)
        self.assertFalse(r.skipped)
        self.assertEqual(len(client.posted), 1)
        self.assertIn("## Findings", client.posted[0][1])
        self.assertIn("Reviewed by talkback-reviewer @ PROMPT.md@", client.posted[0][1])

    def test_refusal_token_posts_nothing(self):
        store = InMemoryBudgetStore()
        client = FakeGitHubClient(_fake_pr())
        r = run_review(
            42,
            "owner/repo",
            github_client=client,
            model_client=_model_returns(REFUSAL_TOKEN),
            store=store,
        )
        self.assertFalse(r.posted)
        self.assertTrue(r.skipped)
        self.assertEqual(r.reason, "reviewer_refused")
        self.assertEqual(client.posted, [])

    def test_budget_denied_posts_exhausted_comment(self):
        store = InMemoryBudgetStore()
        # Burn the cap immediately so the next call is denied.
        from reviewer.budget import check_and_increment
        check_and_increment(store, 499_900, audit_log=None)
        client = FakeGitHubClient(_fake_pr(diff="x" * 5_000))
        r = run_review(
            42,
            "owner/repo",
            github_client=client,
            model_client=_model_returns("should not be called"),
            store=store,
        )
        self.assertTrue(r.posted)
        self.assertTrue(r.skipped)
        self.assertEqual(r.reason, "budget_exhausted")
        self.assertEqual(client.posted[0][1], BUDGET_EXHAUSTED_COMMENT)

    def test_empty_diff_skips_with_no_call(self):
        store = InMemoryBudgetStore()
        client = FakeGitHubClient(_fake_pr(diff=""))
        called = {"yes": False}

        def model(m, s, u):
            called["yes"] = True
            return "## Findings\n- `x:1` — should not be called"

        r = run_review(
            42,
            "owner/repo",
            github_client=client,
            model_client=model,
            store=store,
        )
        self.assertFalse(called["yes"])
        self.assertTrue(r.skipped)
        self.assertEqual(r.reason, "empty_diff")
        self.assertEqual(client.posted, [])

    def test_large_diff_is_truncated(self):
        store = InMemoryBudgetStore()
        # 1MB diff would blow the default 100K token budget.
        big_diff = "+" + ("a" * 1_000_000)
        client = FakeGitHubClient(_fake_pr(diff=big_diff))
        captured = {}

        def model(m, system, user):
            captured["user"] = user
            return "## Findings\n- `x:1` — truncated path."

        r = run_review(
            42,
            "owner/repo",
            github_client=client,
            model_client=model,
            store=store,
            max_input_tokens=10_000,
        )
        self.assertTrue(r.posted)
        # User message must be shorter than the raw diff (truncation actually
        # happened) and contain the truncation note.
        self.assertLess(len(captured["user"]), len(big_diff))
        self.assertIn("truncated by talkback-reviewer", captured["user"])

    def test_footer_pins_scope_sha(self):
        store = InMemoryBudgetStore()
        client = FakeGitHubClient(_fake_pr())
        r = run_review(
            42,
            "owner/repo",
            github_client=client,
            model_client=_model_returns("## Findings\n- `x:1` — note."),
            store=store,
        )
        body = client.posted[0][1]
        # Footer must include a SCOPE.md@<sha> reference, the same one
        # load_prompt resolved.
        _, sha = load_prompt(enforce_pin=False)
        self.assertIn(f"PROMPT.md@{sha}", body)


if __name__ == "__main__":
    unittest.main()
