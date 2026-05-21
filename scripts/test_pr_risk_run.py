#!/usr/bin/env python3
"""Unit tests for scripts/pr_risk_run.py (SCRUM-336 heuristic)."""

from __future__ import annotations

import sys
import unittest
from pathlib import Path

_REPO_ROOT = Path(__file__).resolve().parents[1]
if str(_REPO_ROOT) not in sys.path:
    sys.path.insert(0, str(_REPO_ROOT))

from release_readiness_core.pr_risk.types import (  # noqa: E402
    DOMAIN_ORCHESTRATION,
    DOMAIN_WEB,
    FileChange,
    Signals,
)

from scripts.pr_risk_run import (  # noqa: E402
    apply_style_only_fallback,
    detect_style_only_from_pr_head,
    diff_mentions_orchestration,
    discount_test_loc_from_large_diff,
    is_audit_log_path,
    is_test_path_extended,
    reclassify_creatormode,
)


def _sig(paths_with_loc: list[tuple[str, int]]) -> Signals:
    """Build a minimal Signals object with the orchestration domain hit
    pre-populated so the reclassifier has work to do."""
    files = [FileChange(path=p, added=loc, deleted=0) for p, loc in paths_with_loc]
    s = Signals(files=files, file_count=len(files))
    s.domain_hits[DOMAIN_ORCHESTRATION] = sum(
        1
        for p, _ in paths_with_loc
        if p.lower().startswith("web/src/modes/creatormode")
        or p.startswith("internal/orchestration/")
    )
    return s


class TestDiffMentionsOrchestration(unittest.TestCase):
    def test_pure_ui_diff_is_not_orchestration(self) -> None:
        diff = (
            "@@ -10,1 +10,1 @@\n"
            "-<input accept=\".pdf,.docx\" />\n"
            "+<input accept=\".pdf,.docx,.csv\" />\n"
        )
        self.assertFalse(diff_mentions_orchestration(diff))

    def test_orchestration_token_in_changed_line(self) -> None:
        diff = (
            "@@ -10,1 +10,3 @@\n"
            "+const onAccept = () => syncOrchestrationRecommendations();\n"
        )
        self.assertTrue(diff_mentions_orchestration(diff))

    def test_recommendation_token_in_changed_line(self) -> None:
        diff = (
            "@@ -10,1 +10,2 @@\n"
            "+ListOrchestrationRecommendations();\n"
        )
        self.assertTrue(diff_mentions_orchestration(diff))

    def test_token_only_in_diff_header_is_ignored(self) -> None:
        # Hunk headers and file headers must not trigger the heuristic — only
        # +/- body lines count.
        diff = (
            "--- a/web/src/modes/CreatorMode.jsx (orchestration ref)\n"
            "+++ b/web/src/modes/CreatorMode.jsx\n"
            "@@ -1,1 +1,1 @@ orchestration handler\n"
            "-foo\n"
            "+bar\n"
        )
        self.assertFalse(diff_mentions_orchestration(diff))

    def test_token_match_is_case_insensitive(self) -> None:
        diff = "@@ -1 +1 @@\n+OrchestrAtion\n"
        self.assertTrue(diff_mentions_orchestration(diff))


class TestReclassifyCreatorMode(unittest.TestCase):
    def test_pure_ui_creatormode_drops_orchestration(self) -> None:
        s = _sig([("web/src/modes/CreatorMode.jsx", 1)])
        self.assertEqual(s.domain_hits.get(DOMAIN_ORCHESTRATION), 1)

        moved = reclassify_creatormode(
            s,
            ".",
            "origin/main",
            diff_reader=lambda *_: "@@ -1 +1 @@\n-old\n+new\n",
        )

        self.assertEqual(moved, ["web/src/modes/CreatorMode.jsx"])
        self.assertNotIn(DOMAIN_ORCHESTRATION, s.domain_hits)
        self.assertEqual(s.domain_hits.get(DOMAIN_WEB, 0), 1)

    def test_orchestration_diff_keeps_signal(self) -> None:
        s = _sig([("web/src/modes/CreatorMode.jsx", 1)])
        moved = reclassify_creatormode(
            s,
            ".",
            "origin/main",
            diff_reader=lambda *_: (
                "@@ -1 +1 @@\n+SyncOrchestrationRecommendations()\n"
            ),
        )
        self.assertEqual(moved, [])
        self.assertEqual(s.domain_hits.get(DOMAIN_ORCHESTRATION), 1)

    def test_pure_ui_creatormode_with_real_orchestration_handler_keeps_signal(
        self,
    ) -> None:
        # CreatorMode.jsx is purely UI, but a Go handler in the same diff also
        # legitimately changes orchestration. Only the CreatorMode hit should
        # be dropped; the orchestration domain count should remain 1 (handler).
        s = _sig(
            [
                ("web/src/modes/CreatorMode.jsx", 1),
                ("internal/orchestration/scheduler.go", 5),
            ]
        )
        self.assertEqual(s.domain_hits.get(DOMAIN_ORCHESTRATION), 2)

        def reader(_root: str, _base: str, path: str) -> str:
            if "CreatorMode" in path:
                return "@@ -1 +1 @@\n-foo\n+bar\n"
            return "@@ -1 +1 @@\n+orchestration\n"

        moved = reclassify_creatormode(s, ".", "origin/main", diff_reader=reader)

        self.assertEqual(moved, ["web/src/modes/CreatorMode.jsx"])
        self.assertEqual(s.domain_hits.get(DOMAIN_ORCHESTRATION), 1)
        self.assertEqual(s.domain_hits.get(DOMAIN_WEB, 0), 1)

    def test_no_creatormode_in_diff_is_noop(self) -> None:
        s = Signals(files=[FileChange(path="internal/orchestration/scheduler.go")])
        s.domain_hits[DOMAIN_ORCHESTRATION] = 1

        called = []
        moved = reclassify_creatormode(
            s,
            ".",
            "origin/main",
            diff_reader=lambda *args: called.append(args) or "",
        )

        self.assertEqual(moved, [])
        self.assertEqual(s.domain_hits.get(DOMAIN_ORCHESTRATION), 1)
        # No CreatorMode files → diff_reader must not be called.
        self.assertEqual(called, [])


class TestStyleOnlyFallback(unittest.TestCase):
    """SCRUM-442: upstream detect_style_only_note misses the marker on CI's
    fetch-depth: 1 checkout (HEAD is a synthetic merge, HEAD^2 isn't local).
    The wrapper-side fallback resolves HEAD^2 directly and re-scans its body.
    """

    def _fake_git(self, scripted):
        """Return a fake git runner that pops responses off a scripted list.
        Each entry is ((expected_args_substring, ...), (stdout, returncode))."""
        calls = []

        def runner(repo_root, *args):
            calls.append(args)
            for matcher, response in scripted:
                if all(token in args for token in matcher):
                    return response
            return ("", 1)

        return runner, calls

    def test_finds_style_only_when_head2_is_local(self) -> None:
        runner, _ = self._fake_git(
            [
                # rev-parse HEAD^2 → SHA returned
                (("rev-parse", "--verify", "HEAD^2"),
                 ("deadbeef\n", 0)),
                # cat-file -e <sha> → object exists locally (rc=0)
                (("cat-file", "-e", "deadbeef"),
                 ("", 0)),
                # log -1 --format=%B <sha> → body has Style-only line
                (("log", "-1", "--format=%B", "deadbeef"),
                 ("SCRUM-441: button typography\n\n"
                  "Style-only: drop fontSize/fontWeight overrides\n", 0)),
            ]
        )
        found, snippet = detect_style_only_from_pr_head(".", git=runner)
        self.assertTrue(found)
        self.assertIn("Style-only", snippet)

    def test_fetches_head2_when_not_local(self) -> None:
        runner, calls = self._fake_git(
            [
                (("rev-parse", "--verify", "HEAD^2"),
                 ("cafe1234\n", 0)),
                # cat-file fails → not local
                (("cat-file", "-e", "cafe1234"),
                 ("", 128)),
                # fetch succeeds
                (("fetch", "--depth=50", "origin", "cafe1234"),
                 ("", 0)),
                (("log", "-1", "--format=%B", "cafe1234"),
                 ("Style-only: pure cosmetic\n", 0)),
            ]
        )
        found, snippet = detect_style_only_from_pr_head(".", git=runner)
        self.assertTrue(found)
        self.assertEqual(snippet, "Style-only: pure cosmetic")
        # Confirm fetch was actually invoked.
        self.assertTrue(
            any("fetch" in c and "cafe1234" in c for c in calls),
            f"expected a fetch call for cafe1234 in {calls}",
        )

    def test_returns_false_when_head_has_no_second_parent(self) -> None:
        runner, _ = self._fake_git(
            [
                # No second parent → rev-parse fails
                (("rev-parse", "--verify", "HEAD^2"),
                 ("fatal: ...\n", 128)),
            ]
        )
        found, snippet = detect_style_only_from_pr_head(".", git=runner)
        self.assertFalse(found)
        self.assertEqual(snippet, "")

    def test_returns_false_when_fetch_fails(self) -> None:
        runner, _ = self._fake_git(
            [
                (("rev-parse", "--verify", "HEAD^2"),
                 ("deadbeef\n", 0)),
                (("cat-file", "-e", "deadbeef"),
                 ("", 128)),
                (("fetch", "--depth=50", "origin", "deadbeef"),
                 ("network error\n", 1)),
            ]
        )
        found, _ = detect_style_only_from_pr_head(".", git=runner)
        self.assertFalse(found)

    def test_returns_false_when_body_has_no_style_only_line(self) -> None:
        runner, _ = self._fake_git(
            [
                (("rev-parse", "--verify", "HEAD^2"),
                 ("deadbeef\n", 0)),
                (("cat-file", "-e", "deadbeef"),
                 ("", 0)),
                (("log", "-1", "--format=%B", "deadbeef"),
                 ("SCRUM-XXX: real feature change\n\n"
                  "Some description that mentions style but is not the marker.\n", 0)),
            ]
        )
        found, snippet = detect_style_only_from_pr_head(".", git=runner)
        self.assertFalse(found)
        self.assertEqual(snippet, "")

    def test_apply_fallback_mutates_signals_when_marker_found(self) -> None:
        s = Signals()
        self.assertFalse(s.style_only_note_found)
        fired = apply_style_only_fallback(
            s, ".",
            detector=lambda _root: (True, "Style-only: cosmetic"),
        )
        self.assertTrue(fired)
        self.assertTrue(s.style_only_note_found)
        self.assertEqual(s.style_only_note_snippet, "Style-only: cosmetic")

    def test_apply_fallback_is_idempotent_when_upstream_already_detected(self) -> None:
        s = Signals(style_only_note_found=True, style_only_note_snippet="upstream")
        sentinel = []
        fired = apply_style_only_fallback(
            s, ".",
            detector=lambda _root: sentinel.append("called") or (True, "wrapper"),
        )
        self.assertFalse(fired)  # wrapper short-circuited
        self.assertEqual(s.style_only_note_snippet, "upstream")  # not overwritten
        self.assertEqual(sentinel, [])  # detector never invoked

    def test_apply_fallback_noop_when_detector_returns_false(self) -> None:
        s = Signals()
        fired = apply_style_only_fallback(
            s, ".",
            detector=lambda _root: (False, ""),
        )
        self.assertFalse(fired)
        self.assertFalse(s.style_only_note_found)


# ── SCRUM-545: test-LOC discount for Large diff signal ───────────────────────

def _sig_with_loc(file_loc: list[tuple[str, int]]) -> Signals:
    """Build Signals with the given file/LOC pairs (added=loc, deleted=0).
    Mirrors what ``extract_signals`` would compute for ``total_*``."""
    files = [FileChange(path=p, added=loc, deleted=0) for p, loc in file_loc]
    s = Signals(files=files, file_count=len(files))
    s.total_added = sum(loc for _, loc in file_loc)
    s.total_deleted = 0
    s.total_loc = s.total_added + s.total_deleted
    return s


class TestIsTestPathExtended(unittest.TestCase):
    def test_upstream_patterns_still_match(self):
        for p in (
            "internal/foo/bar_test.go",
            "web/src/test/Foo.test.jsx",
            "web/e2e/orchestration.spec.ts",
            "internal/handlers/testdata/fixture.json",
        ):
            self.assertTrue(is_test_path_extended(p), p)

    def test_python_test_paths_match(self):
        for p in (
            "scripts/test_full_auto_start.py",
            "scripts/test_pr_risk_run.py",
            "tests/test_something.py",
            "internal/something_test.py",
        ):
            self.assertTrue(is_test_path_extended(p), p)

    def test_non_test_paths_do_not_match(self):
        for p in (
            "scripts/full_auto/start.py",
            "internal/handlers/session.go",
            "web/src/modes/CreatorMode.jsx",
            "scripts/full_auto/lib/jira.py",
        ):
            self.assertFalse(is_test_path_extended(p), p)


class TestDiscountTestLocFromLargeDiff(unittest.TestCase):
    def test_pure_test_diff_collapses_to_zero(self):
        # 500 LOC, all tests — well above the 400 threshold but should
        # collapse to prod-only=0 after discount.
        s = _sig_with_loc([
            ("scripts/test_full_auto_start.py", 300),
            ("scripts/test_full_auto_review.py", 200),
        ])
        result = discount_test_loc_from_large_diff(s)
        self.assertIsNotNone(result)
        prod_loc, original = result
        self.assertEqual(prod_loc, 0)
        self.assertEqual(original, 500)
        self.assertEqual(s.total_loc, 0)

    def test_pure_prod_diff_is_unchanged(self):
        s = _sig_with_loc([
            ("scripts/full_auto/start.py", 350),
            ("scripts/full_auto/lib/jira.py", 200),
        ])
        self.assertIsNone(discount_test_loc_from_large_diff(s))
        self.assertEqual(s.total_loc, 550)
        self.assertEqual(s.total_added, 550)

    def test_mixed_prod_heavy_still_warns(self):
        # 800 LOC, of which 200 tests, 600 prod. Prod is above threshold,
        # so the discount must NOT fire — the WARN is legitimate.
        s = _sig_with_loc([
            ("scripts/full_auto/review.py", 600),
            ("scripts/test_full_auto_review.py", 200),
        ])
        self.assertIsNone(discount_test_loc_from_large_diff(s))
        self.assertEqual(s.total_loc, 800)

    def test_mixed_test_heavy_collapses_below_threshold(self):
        # 800 LOC, of which 500 tests, 300 prod. Prod is under 400, so the
        # discount should fire and total_loc drops to 300.
        s = _sig_with_loc([
            ("scripts/full_auto/review.py", 300),
            ("scripts/test_full_auto_review.py", 500),
        ])
        result = discount_test_loc_from_large_diff(s)
        self.assertIsNotNone(result)
        prod_loc, original = result
        self.assertEqual(prod_loc, 300)
        self.assertEqual(original, 800)
        self.assertEqual(s.total_loc, 300)

    def test_diff_below_threshold_is_a_noop(self):
        # 300 LOC total, doesn't reach the 400 threshold. No-op even if
        # half is tests — the original total_loc stays.
        s = _sig_with_loc([
            ("scripts/full_auto/start.py", 150),
            ("scripts/test_full_auto_start.py", 150),
        ])
        self.assertIsNone(discount_test_loc_from_large_diff(s))
        self.assertEqual(s.total_loc, 300)

    def test_threshold_exactly_at_400_still_fires(self):
        # 500 LOC, prod=399, tests=101 → prod < threshold (400), fires.
        s = _sig_with_loc([
            ("scripts/full_auto/start.py", 399),
            ("scripts/test_full_auto_start.py", 101),
        ])
        result = discount_test_loc_from_large_diff(s)
        self.assertIsNotNone(result)
        prod_loc, _ = result
        self.assertEqual(prod_loc, 399)

    def test_threshold_exactly_at_prod_400_does_not_fire(self):
        # prod=400 (exact threshold), tests=200 → prod >= threshold, no fire.
        s = _sig_with_loc([
            ("scripts/full_auto/start.py", 400),
            ("scripts/test_full_auto_start.py", 200),
        ])
        self.assertIsNone(discount_test_loc_from_large_diff(s))
        self.assertEqual(s.total_loc, 600)

    def test_historical_pr_482_would_pass(self):
        # SCRUM-544 PR #482: 508 net LOC, 264 of tests, 244 prod.
        # Prod is well below 400 → discount fires.
        s = _sig_with_loc([
            ("scripts/full_auto/poll.py", 214),
            ("scripts/full_auto/lib/github.py", 16),
            ("ops/define-kpis/lint-runs.log", 14),
            ("scripts/test_full_auto_poll.py", 264),
        ])
        result = discount_test_loc_from_large_diff(s)
        self.assertIsNotNone(result, "PR #482's test-heavy diff should fire the discount")
        prod_loc, original = result
        self.assertLess(prod_loc, 400)
        self.assertGreaterEqual(original, 400)

    def test_historical_pr_481_would_pass_with_audit_log_discount(self):
        # SCRUM-543 PR #481: 677 net LOC. Prod after test discount = 413,
        # which is just above the 400 threshold. Subtracting the 26-line
        # ops/define-kpis/lint-runs.log audit-log noise brings prod to 387,
        # below the threshold → discount fires.
        s = _sig_with_loc([
            ("scripts/full_auto/review.py", 295),
            ("scripts/test_full_auto_review.py", 264),
            ("scripts/full_auto/start.py", 58),
            ("scripts/full_auto/lib/github.py", 34),
            ("ops/define-kpis/lint-runs.log", 26),
        ])
        result = discount_test_loc_from_large_diff(s)
        self.assertIsNotNone(result, "PR #481 should fire the discount under the audit-log-extended rule")
        prod_loc, _ = result
        self.assertEqual(prod_loc, 387)
        self.assertLess(prod_loc, 400)

    def test_historical_pr_480_still_warns(self):
        # SCRUM-542 PR #480: 802 net LOC. Prod after test+audit-log
        # discount = 367 + 66 + 50 = 483, still above 400. The largest
        # PR of the three legitimately stays at WARN — the discount
        # doesn't paper over a real review surface.
        s = _sig_with_loc([
            ("scripts/full_auto/start.py", 367),
            ("scripts/test_full_auto_start.py", 305),
            ("scripts/full_auto/lib/adf.py", 66),
            ("scripts/full_auto/lib/jira.py", 50),
            ("ops/define-kpis/lint-runs.log", 14),
        ])
        self.assertIsNone(
            discount_test_loc_from_large_diff(s),
            "PR #480's prod portion (483) is still above threshold; WARN stays",
        )
        self.assertEqual(s.total_loc, 802)


class TestIsAuditLogPath(unittest.TestCase):
    def test_matches_lint_runs_log(self):
        self.assertTrue(is_audit_log_path("ops/define-kpis/lint-runs.log"))

    def test_does_not_match_other_log_extensions(self):
        # Conservative: only the named audit-log path matches. Future
        # additions go through the explicit _AUDIT_LOG_PATHS frozenset.
        for p in (
            "ops/some-other.log",
            "scripts/full_auto/start.py",
            "ops/define-kpis/snapshot.json",
            "ops/define-kpis/lint-runs.log.bak",
        ):
            self.assertFalse(is_audit_log_path(p), p)


if __name__ == "__main__":
    unittest.main()
