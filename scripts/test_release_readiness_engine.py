import unittest

from scripts.release_readiness_engine import build_remediation_items, compute_readiness


BASE_CONFIG = {
    "scoring": {
        "max_score": 100,
        "pass_threshold": 80,
        "warn_threshold": 60,
        "block_score": 0,
        "penalties": {
            "missing_smoke_artifact": 25,
            "missing_e2e_artifact": 15,
            "missing_coverage_artifact": 5,
            "missing_prod_health_artifact": 5,
            "non_critical_e2e_failure": 15,
            "e2e_retries_or_flaky": 10,
            "coverage_regression": 12,
            "risky_config_without_note": 10,
        },
    },
    "e2e_critical_name_patterns": ["login", "session", "upload", "material", "invite"],
    "risk_from_paths": [
        {
            "categories": ["upload_extraction"],
            "patterns": ["internal/handlers/session_materials.go"],
        },
        {
            "categories": ["migrations"],
            "patterns": ["db/migrations/**"],
        },
    ],
    "risky_config_patterns": ["Dockerfile"],
    "infer_validations_when_pass": {
        "smoke": ["upload_extraction"],
        "e2e": ["upload_extraction"],
    },
}


def smoke_passed(validations=None):
    return {
        "status": "passed",
        "failed_count": 0,
        "total_count": 1,
        "validations": validations or {},
    }


def e2e_passed(validations=None):
    return {
        "status": "passed",
        "failed_count": 0,
        "total_count": 1,
        "retries": 0,
        "failures": [],
        "validations": validations or {},
    }


def e2e_failed_with_failures(failures, failed_count=1, retries=0):
    return {
        "status": "failed",
        "failed_count": failed_count,
        "total_count": 1,
        "retries": retries,
        "failures": failures,
    }


class ReleaseReadinessEngineTests(unittest.TestCase):
    def test_smoke_failed_blocks(self):
        res = compute_readiness(
            config=BASE_CONFIG,
            changed_files=["internal/handlers/session_materials.go"],
            smoke={"status": "failed", "failed_count": 1, "total_count": 1},
            e2e=e2e_passed(),
            coverage={"line_percent": 50, "baseline_percent": 51},
            prod_health={"ok": True},
            migration_validated_cli=False,
        )
        self.assertEqual(res.outcome, "BLOCK")
        self.assertIn("smoke_failed", res.failed_checks)

    def test_critical_e2e_blocks(self):
        res = compute_readiness(
            config=BASE_CONFIG,
            changed_files=["internal/handlers/session_materials.go"],
            smoke=smoke_passed(),
            e2e=e2e_failed_with_failures([{"title": "login flow failed", "name": "login_flow"}]),
            coverage={"line_percent": 50, "baseline_percent": 51},
            prod_health={"ok": True},
            migration_validated_cli=False,
        )
        self.assertEqual(res.outcome, "BLOCK")
        self.assertIn("e2e_critical", res.failed_checks)

    def test_noncritical_e2e_warns(self):
        res = compute_readiness(
            config=BASE_CONFIG,
            changed_files=["internal/handlers/session_materials.go"],
            smoke=smoke_passed(),
            e2e=e2e_failed_with_failures([{"title": "qa-rag-noncritical", "name": "qa"}]),
            coverage={"line_percent": 50, "baseline_percent": 51},
            prod_health={"ok": True},
            migration_validated_cli=False,
        )
        self.assertEqual(res.outcome, "WARN")
        self.assertIn("e2e_non_critical", res.failed_checks)

    def test_migrations_without_validation_blocks(self):
        res = compute_readiness(
            config=BASE_CONFIG,
            changed_files=["db/migrations/000012_add_video_ingestion_fields.up.sql"],
            smoke=smoke_passed(),
            e2e=e2e_passed(),
            coverage={"line_percent": 50, "baseline_percent": 51},
            prod_health={"ok": True},
            migration_validated_cli=False,
        )
        self.assertEqual(res.outcome, "BLOCK")
        self.assertIn("risk_without_validation", res.failed_checks)

    def test_risky_config_warns_without_note(self):
        res = compute_readiness(
            config=BASE_CONFIG,
            changed_files=["Dockerfile", "internal/handlers/session_materials.go"],
            smoke=smoke_passed(),
            e2e=e2e_passed(),
            coverage={"line_percent": 51, "baseline_percent": 50},  # no regression
            prod_health={"ok": True},
            migration_validated_cli=False,
        )
        self.assertEqual(res.outcome, "WARN")
        self.assertIn("risky_config_without_note", res.failed_checks)

    def test_risky_config_suppressed_by_commit_note(self):
        res = compute_readiness(
            config=BASE_CONFIG,
            changed_files=["Dockerfile", "internal/handlers/session_materials.go"],
            smoke=smoke_passed(),
            e2e=e2e_passed(),
            coverage={"line_percent": 51, "baseline_percent": 50},  # no regression
            prod_health={"ok": True},
            migration_validated_cli=False,
            commit_validation_note=True,
            commit_validation_snippet="Validation: ran workflow_dispatch, verified artifacts",
        )
        self.assertEqual(res.outcome, "PASS")
        self.assertNotIn("risky_config_without_note", res.failed_checks)
        self.assertTrue(res.evidence["validation_note_present"])
        self.assertEqual(res.evidence["validation_note_source"], "commit_message")

    def test_coverage_regression_warns(self):
        res = compute_readiness(
            config=BASE_CONFIG,
            changed_files=["internal/handlers/session_materials.go"],
            smoke=smoke_passed(),
            e2e=e2e_passed(),
            coverage={"line_percent": 49, "baseline_percent": 50},
            prod_health={"ok": True},
            migration_validated_cli=False,
        )
        self.assertEqual(res.outcome, "WARN")
        self.assertIn("coverage_regression", res.failed_checks)

    def test_low_score_blocks_without_explicit_blocker(self):
        # No smoke, no e2e, coverage regression: score = 100 - 25 - 15 - 12 = 48 < 60 → BLOCK
        res = compute_readiness(
            config=BASE_CONFIG,
            changed_files=[],
            smoke=None,
            e2e=None,
            coverage={"line_percent": 40, "baseline_percent": 50},
            prod_health={"ok": True},
            migration_validated_cli=False,
        )
        self.assertEqual(res.outcome, "BLOCK")
        self.assertIn("smoke_artifact", res.failed_checks)
        self.assertIn("e2e_artifact", res.failed_checks)
        self.assertIn("coverage_regression", res.failed_checks)

    def test_remediation_items_populated_on_warn(self):
        config_with_remediation = {
            **BASE_CONFIG,
            "remediation": {
                "coverage_regression": {
                    "severity": "warn",
                    "likely_cause": "Coverage dropped",
                    "recommended_action": "Add tests",
                    "fix_type": "test",
                },
            },
        }
        res = compute_readiness(
            config=config_with_remediation,
            changed_files=[],
            smoke=smoke_passed(),
            e2e=e2e_passed(),
            coverage={"line_percent": 40, "baseline_percent": 50},
            prod_health={"ok": True},
            migration_validated_cli=False,
        )
        self.assertIn("coverage_regression", res.failed_checks)
        checks = [item["check"] for item in res.remediation_items]
        self.assertIn("coverage_regression", checks)
        item = next(i for i in res.remediation_items if i["check"] == "coverage_regression")
        self.assertEqual(item["severity"], "warn")
        self.assertEqual(item["fix_type"], "test")
        self.assertEqual(item["recommended_action"], "Add tests")

    def test_remediation_items_empty_on_pass(self):
        res = compute_readiness(
            config=BASE_CONFIG,
            changed_files=[],
            smoke=smoke_passed(),
            e2e=e2e_passed(),
            coverage={"line_percent": 50, "baseline_percent": 50},
            prod_health={"ok": True},
            migration_validated_cli=False,
        )
        self.assertEqual(res.outcome, "PASS")
        self.assertEqual(res.remediation_items, [])

    def test_build_remediation_items_fallback_for_unknown_key(self):
        items = build_remediation_items(["unknown_check"], {})
        self.assertEqual(len(items), 1)
        self.assertEqual(items[0]["check"], "unknown_check")
        self.assertIn("unknown_check", items[0]["recommended_action"])

    def test_happy_path_passes(self):
        res = compute_readiness(
            config=BASE_CONFIG,
            changed_files=["internal/handlers/session_materials.go"],
            smoke=smoke_passed(),
            e2e=e2e_passed(),
            coverage={"line_percent": 50, "baseline_percent": 50},  # no regression
            prod_health={"ok": True},
            migration_validated_cli=False,
        )
        self.assertEqual(res.outcome, "PASS")
        self.assertEqual(res.failed_checks, [])
        self.assertEqual(res.blockers, [])


if __name__ == "__main__":
    unittest.main()

