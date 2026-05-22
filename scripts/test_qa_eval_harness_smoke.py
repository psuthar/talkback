#!/usr/bin/env python3
"""Smoke tests: end-to-end invocation for Q&A eval harness (dry-run + dataset bundle)."""

from __future__ import annotations

import json
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

_REPO_ROOT = Path(__file__).resolve().parents[1]
if str(_REPO_ROOT) not in sys.path:
    sys.path.insert(0, str(_REPO_ROOT))


class TestHarnessSmoke(unittest.TestCase):
    def test_run_qa_eval_dry_run_writes_manifest(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            run_id = "harness-smoke-dry-run"
            out = Path(tmp)
            cmd = [
                sys.executable,
                str(_REPO_ROOT / "scripts" / "run_qa_eval.py"),
                "--dry-run",
                "--run-id",
                run_id,
                "--out-dir",
                str(out),
            ]
            proc = subprocess.run(
                cmd,
                cwd=_REPO_ROOT,
                capture_output=True,
                text=True,
                timeout=120,
            )
            self.assertEqual(
                proc.returncode,
                0,
                proc.stdout + "\n" + proc.stderr,
            )
            manifest_path = out / run_id / "run_manifest.json"
            self.assertTrue(manifest_path.is_file(), f"missing {manifest_path}")
            data = json.loads(manifest_path.read_text(encoding="utf-8"))
            self.assertTrue(data.get("dry_run"))
            self.assertGreaterEqual(data.get("case_count", 0), 20)

    def test_eval_dataset_bundle_validates(self) -> None:
        try:
            from scripts.qa_eval_datasets import validate_eval_dataset_bundle
        except ImportError as e:
            self.skipTest(f"qa_eval_datasets import failed: {e}")
        errs = validate_eval_dataset_bundle()
        self.assertEqual(errs, [], "\n".join(errs))


if __name__ == "__main__":
    unittest.main()
