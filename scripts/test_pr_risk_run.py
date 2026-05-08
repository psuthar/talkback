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
    diff_mentions_orchestration,
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


if __name__ == "__main__":
    unittest.main()
