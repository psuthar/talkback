"""
e2e_to_readiness.py — Convert Playwright JSON reporter output to e2e_results.json
in the TalkBack release-readiness schema.

Usage:
    python scripts/e2e_to_readiness.py \
        --input playwright-results.json \
        --output e2e_results.json

Schema produced (all extra fields in the input are ignored by the engine):
    {
      "status": "passed|failed|skipped",
      "failed_count": <int>,
      "total_count": <int>,
      "retries": <int>,
      "failures": [{"title": "...", "name": "..."}],
      "validations": {
        "auth_session": true|false,   (only key present if tests from group ran)
        "upload_extraction": true|false,
        "nav_assets": true|false,
        "viewer_materials": true|false,
        "qa_rag": true|false
      }
    }

Validation → test file mapping:
    auth_session      creator-access, participant-acceptance, invite-invalid-token,
                      participant-happy-path, session-availability, session-routing
    upload_extraction material-processing-state
    nav_assets        material-viewers
    viewer_materials  material-viewers, pptx-polling-stop
    qa_rag            qa-history

A validation key is:
  - true  if all tests in the group ran and all passed
  - false if any test in the group failed
  - absent if no tests from that group ran
"""

from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path

# ---------------------------------------------------------------------------
# Validation → source file stem mapping
# A test belongs to a validation group when its file path contains the stem.
# ---------------------------------------------------------------------------

VALIDATION_FILE_STEMS: dict[str, list[str]] = {
    "auth_session": [
        "creator-access",
        "participant-acceptance",
        "invite-invalid-token",
        "participant-happy-path",
        "session-availability",
        "session-routing",
    ],
    "upload_extraction": [
        "material-processing-state",
    ],
    "nav_assets": [
        "material-viewers",
    ],
    "viewer_materials": [
        "material-viewers",
        "pptx-polling-stop",
    ],
    "qa_rag": [
        "qa-history",
    ],
}


def _stem_for_spec(file_path: str) -> str:
    """Return the bare file stem (no extension) from an absolute or relative path."""
    p = Path(file_path.replace("\\", "/"))
    # Drop known double-extension like .e2e.ts → remove both suffixes
    name = p.name
    for ext in (".ts", ".js", ".mjs", ".e2e"):
        if name.endswith(ext):
            name = name[: -len(ext)]
    return name


def _file_belongs_to_group(file_path: str, stems: list[str]) -> bool:
    stem = _stem_for_spec(file_path)
    return stem in stems


def _collect_specs(suite_node: dict) -> list[dict]:
    """
    Recursively walk the Playwright JSON suite tree and collect all leaf spec nodes.
    Each spec node has 'file' (path) and 'specs' list.
    A spec entry looks like: {"title": "...", "ok": true, "tests": [...]}
    """
    specs: list[dict] = []
    # A suite node may have "suites" (nested) and/or "specs" (leaf tests).
    for spec in suite_node.get("specs", []):
        # Attach the file path from the enclosing suite if not directly on the spec.
        if "file" not in spec:
            spec = dict(spec)
            spec["file"] = suite_node.get("file", "")
        specs.append(spec)

    for child_suite in suite_node.get("suites", []):
        # Propagate file path down from parent when missing on child.
        if "file" not in child_suite and "file" in suite_node:
            child_suite = dict(child_suite)
            child_suite["file"] = suite_node["file"]
        specs.extend(_collect_specs(child_suite))

    return specs


def convert(playwright_data: dict) -> dict:
    """Convert Playwright JSON reporter payload to readiness schema."""

    # ------------------------------------------------------------------
    # Playwright JSON reporter top-level shape:
    #   { "suites": [...], "stats": { "expected": N, "unexpected": N,
    #     "skipped": N, "flaky": N, "duration": N } }
    # Each suite entry has: { "title": "file.e2e.ts", "file": "...", "suites": [...], "specs": [...] }
    # Each spec has: { "title": "test name", "ok": true/false,
    #                  "tests": [{"results": [{"status": "passed|failed|skipped|timedOut"}]}] }
    # ------------------------------------------------------------------

    stats: dict = playwright_data.get("stats", {})
    top_suites: list[dict] = playwright_data.get("suites", [])

    total_expected = int(stats.get("expected", 0))
    total_unexpected = int(stats.get("unexpected", 0))
    total_skipped = int(stats.get("skipped", 0))
    total_flaky = int(stats.get("flaky", 0))
    total_count = total_expected + total_unexpected + total_skipped + total_flaky

    # Collect all leaf spec entries with their enclosing file path.
    all_specs: list[dict] = []
    for suite in top_suites:
        all_specs.extend(_collect_specs(suite))

    # Deduplicate retried specs: a spec that appears multiple times (retried) counts once.
    # We identify uniqueness by (file, title) and take the worst outcome.
    seen: dict[tuple[str, str], dict] = {}
    for spec in all_specs:
        file_path = spec.get("file", "")
        title = spec.get("title", "")
        key = (file_path, title)
        if key not in seen:
            seen[key] = spec
        else:
            # Keep the failing one if either is failing.
            if not spec.get("ok", True):
                seen[key] = spec

    unique_specs = list(seen.values())

    # Count retries: number of specs that have more than one result entry.
    retry_count = 0
    for spec in unique_specs:
        tests = spec.get("tests", [])
        for t in tests:
            results = t.get("results", [])
            if len(results) > 1:
                retry_count += 1

    # Recalculate totals from unique specs when stats are absent/zero.
    if total_count == 0 and unique_specs:
        total_count = len(unique_specs)

    # Gather failures.
    failures: list[dict] = []
    failed_titles: set[str] = set()
    for spec in unique_specs:
        if not spec.get("ok", True):
            title = spec.get("title", "")
            failures.append({"title": title, "name": title})
            failed_titles.add(title)

    failed_count = len(failures)

    # If Playwright reported unexpected > 0 but we found no failures from specs,
    # fall back to the stats count so the engine sees a non-zero failed_count.
    if failed_count == 0 and total_unexpected > 0:
        failed_count = total_unexpected

    # Overall status.
    if total_count == 0 and not unique_specs:
        status = "skipped"
    elif failed_count > 0:
        status = "failed"
    else:
        status = "passed"

    # ------------------------------------------------------------------
    # Validation mapping
    # ------------------------------------------------------------------
    # For each group: collect specs whose file matches, derive pass/fail.
    # Key is only emitted if at least one test from the group was observed.
    validations: dict[str, bool] = {}

    for val_key, stems in VALIDATION_FILE_STEMS.items():
        group_specs = [s for s in unique_specs if _file_belongs_to_group(s.get("file", ""), stems)]
        if not group_specs:
            # No tests from this group ran — omit the key.
            continue
        group_failed = any(not s.get("ok", True) for s in group_specs)
        validations[val_key] = not group_failed

    return {
        "status": status,
        "failed_count": failed_count,
        "total_count": total_count,
        "retries": retry_count,
        "failures": failures,
        "validations": validations,
    }


def main() -> None:
    parser = argparse.ArgumentParser(
        description="Convert Playwright JSON reporter output to e2e_results.json (readiness schema)."
    )
    parser.add_argument(
        "--input",
        required=True,
        help="Path to playwright-results.json (Playwright --reporter=json output).",
    )
    parser.add_argument(
        "--output",
        required=True,
        help="Path to write e2e_results.json.",
    )
    args = parser.parse_args()

    input_path = Path(args.input)
    output_path = Path(args.output)

    if not input_path.exists():
        print(f"ERROR: input file not found: {input_path}", file=sys.stderr)
        # Write a skipped placeholder so downstream steps don't fail on missing file.
        output_path.write_text(
            json.dumps(
                {
                    "status": "skipped",
                    "failed_count": 0,
                    "total_count": 0,
                    "retries": 0,
                    "failures": [],
                    "validations": {},
                    "note": f"playwright-results.json not found at {input_path}",
                },
                indent=2,
            ),
            encoding="utf-8",
        )
        sys.exit(0)

    try:
        playwright_data = json.loads(input_path.read_text(encoding="utf-8"))
    except json.JSONDecodeError as exc:
        print(f"ERROR: failed to parse {input_path}: {exc}", file=sys.stderr)
        output_path.write_text(
            json.dumps(
                {
                    "status": "failed",
                    "failed_count": 1,
                    "total_count": 0,
                    "retries": 0,
                    "failures": [{"title": "playwright_json_parse_error", "name": "playwright_json_parse_error"}],
                    "validations": {},
                    "note": f"JSON parse error: {exc}",
                },
                indent=2,
            ),
            encoding="utf-8",
        )
        sys.exit(1)

    result = convert(playwright_data)

    output_path.parent.mkdir(parents=True, exist_ok=True)
    output_path.write_text(json.dumps(result, indent=2), encoding="utf-8")

    print(
        f"e2e_results.json written: status={result['status']}, "
        f"passed={result['total_count'] - result['failed_count']}/{result['total_count']}, "
        f"retries={result['retries']}, "
        f"validations={result['validations']}"
    )


if __name__ == "__main__":
    main()
