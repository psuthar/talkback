#!/usr/bin/env python3
"""SCRUM-442 — pin the behavior of the synthesized e2e_results.json that
release-readiness.yml writes when E2E is path-skipped (per SCRUM-369).

The workflow's heredoc must produce a shape that the upstream
release_readiness_core readiness evaluator treats as "no E2E ran, no
inference, no warning" — NOT as "E2E was supposed to run but didn't."

The pre-SCRUM-442 shape used status="skipped" which fired the
"E2E was skipped in CI" warning + 10-point penalty + e2e_skipped
failed_check — a procedural WARN on every pure-tooling PR.

The post-SCRUM-442 shape uses status="not_applicable" which is silently
accepted by the engine: no warning, no penalty, no over-inference of
validations. This test calls the upstream evaluator directly with the
exact JSON the workflow heredoc writes, so a future workflow edit that
reverts to status="skipped" (or any other status that re-introduces the
warning) fails the test before merge.
"""

from __future__ import annotations

import json
import re
import sys
import unittest
from pathlib import Path

_REPO_ROOT = Path(__file__).resolve().parents[1]
if str(_REPO_ROOT) not in sys.path:
    sys.path.insert(0, str(_REPO_ROOT))


# Heredoc payload extracted from .github/workflows/release-readiness.yml. The
# test reads the workflow file directly so the assertion stays anchored to
# the actual shipped string rather than a copy.
_WORKFLOW_PATH = _REPO_ROOT / ".github" / "workflows" / "release-readiness.yml"
_HEREDOC_RE = re.compile(
    r"cat > e2e_results\.json <<'JSON'\s*\n\s*(?P<json>\{[^\n]+\})\s*\n",
)


def _extract_synthesized_json() -> dict:
    text = _WORKFLOW_PATH.read_text(encoding="utf-8")
    m = _HEREDOC_RE.search(text)
    if not m:
        raise AssertionError(
            "Could not locate the synthesized e2e_results.json heredoc in "
            f"{_WORKFLOW_PATH}. Did the workflow refactor change the step "
            "name or heredoc format? Update the regex if so."
        )
    return json.loads(m.group("json"))


class TestSynthesizedE2eSkipBehavior(unittest.TestCase):
    """Behavioral contract: the heredoc payload must NOT trigger the
    upstream e2e_skipped warning/penalty/failed_check."""

    def setUp(self) -> None:
        # Skip the integration suite if release_readiness_core isn't
        # installed (local-dev convenience). CI has it.
        try:
            from release_readiness_core.readiness_evaluate import evaluate  # noqa: F401
        except ImportError:
            self.skipTest("release_readiness_core not installed locally")

    def _evaluate_with(self, e2e_payload: dict):
        import yaml
        from release_readiness_core.readiness_evaluate import evaluate

        with (_REPO_ROOT / "ops" / "release-readiness" / "config.yaml").open() as f:
            config = yaml.safe_load(f)

        return evaluate(
            repo_root=_REPO_ROOT,
            config=config,
            base_ref="origin/main",
            smoke={"status": "passed", "passed": True},
            e2e=e2e_payload,
            coverage=None,
            prod_health=None,
            migration_validated_cli=True,
            empty_diff=False,
            commit_validation_note=True,
            commit_validation_snippet="Validation: smoke synthesis",
        )

    def test_synthesized_payload_uses_not_applicable_status(self) -> None:
        payload = _extract_synthesized_json()
        self.assertEqual(
            payload.get("status"),
            "not_applicable",
            "SCRUM-442 requires status='not_applicable' on the synthesized "
            "e2e_results.json so the upstream engine doesn't fire the "
            "e2e_skipped warning. If you intentionally changed this, also "
            "update this test's expected value.",
        )
        # Preserve the diagnostic fields so the artifact is still readable.
        self.assertTrue(payload.get("skipped"))
        self.assertIn("skip_reason", payload)
        self.assertEqual(payload.get("failed_count"), 0)
        self.assertEqual(payload.get("retries"), 0)

    def test_synthesized_payload_does_not_fire_e2e_skipped_warning(self) -> None:
        payload = _extract_synthesized_json()
        result = self._evaluate_with(payload)
        warnings = [str(w) for w in result.warnings]
        self.assertFalse(
            any("E2E was skipped" in w for w in warnings),
            f"Expected NO 'E2E was skipped' warning, got: {warnings}",
        )
        self.assertNotIn(
            "e2e_skipped",
            result.failed_checks,
            f"Expected e2e_skipped NOT in failed_checks, got: {result.failed_checks}",
        )

    def test_legacy_skipped_shape_DOES_fire_warning(self) -> None:
        """Sanity check that the regression test isn't a no-op — assert the
        upstream engine still fires the e2e_skipped warning when status is
        'skipped'. If this ever fails, the upstream behavior changed and
        SCRUM-442's wrapper-side workaround may be redundant."""
        legacy = {
            "status": "skipped",
            "skipped": True,
            "skip_reason": "no E2E-relevant changes in this PR",
            "failed_count": 0,
            "total_count": 0,
            "retries": 0,
            "failures": [],
        }
        result = self._evaluate_with(legacy)
        warnings = [str(w) for w in result.warnings]
        self.assertTrue(
            any("E2E was skipped" in w for w in warnings),
            f"Expected 'E2E was skipped' warning for legacy 'skipped' "
            f"status (upstream behavior sanity check). Got: {warnings}",
        )
        self.assertIn("e2e_skipped", result.failed_checks)


if __name__ == "__main__":
    unittest.main()
