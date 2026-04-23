#!/usr/bin/env python3
"""Load and validate Q&A eval dataset JSON (eval_cases + expected_scores) with JSON Schema."""

from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path
from typing import Any

from jsonschema import Draft202012Validator
from jsonschema.exceptions import SchemaError

REPO_ROOT = Path(__file__).resolve().parents[1]

DEFAULT_EVAL_CASES = REPO_ROOT / "eval" / "qa" / "eval_cases_v1.json"
DEFAULT_EXPECTED_SCORES = REPO_ROOT / "eval" / "qa" / "expected_scores_v1.json"
DEFAULT_INVENTORY = REPO_ROOT / "eval" / "qa" / "fixture_fact_inventory.json"
SCHEMA_EVAL_CASES = REPO_ROOT / "eval" / "qa" / "schemas" / "eval_cases_v1.schema.json"
SCHEMA_EXPECTED_SCORES = REPO_ROOT / "eval" / "qa" / "schemas" / "expected_scores_v1.schema.json"


def load_json(path: Path) -> dict[str, Any]:
    return json.loads(path.read_text(encoding="utf-8"))


def _schema_validator(schema_path: Path) -> Draft202012Validator:
    schema = load_json(schema_path)
    try:
        Draft202012Validator.check_schema(schema)
    except SchemaError as e:
        raise ValueError(f"invalid JSON Schema at {schema_path}: {e}") from e
    return Draft202012Validator(schema)


def validate_against_schema(instance: Any, schema_path: Path) -> list[str]:
    validator = _schema_validator(schema_path)
    return [f"{e.json_path}: {e.message}" for e in validator.iter_errors(instance)]


def cross_check_eval_cases_inventory(
    eval_data: dict[str, Any], inv_data: dict[str, Any]
) -> list[str]:
    errors: list[str] = []
    inv_cases = inv_data.get("cases")
    if not isinstance(inv_cases, list):
        return ["inventory.cases must be an array"]
    inv_by_id: dict[str, dict[str, Any]] = {}
    for c in inv_cases:
        if isinstance(c, dict) and isinstance(c.get("case_id"), str):
            inv_by_id[c["case_id"]] = c

    eval_cases = eval_data.get("cases")
    if not isinstance(eval_cases, list):
        return ["eval_cases.cases must be an array"]

    for idx, case in enumerate(eval_cases):
        prefix = f"eval_cases.cases[{idx}]"
        if not isinstance(case, dict):
            errors.append(f"{prefix} must be an object")
            continue
        cid = case.get("case_id")
        if not isinstance(cid, str):
            continue
        inv = inv_by_id.get(cid)
        if inv is None:
            errors.append(f"{prefix}: case_id {cid} not found in fixture_fact_inventory")
            continue
        for field in ("fixture_id", "question", "expected_status"):
            if case.get(field) != inv.get(field):
                errors.append(
                    f"{prefix}: {field} must match fixture_fact_inventory for {cid}"
                )
        inv_kw = inv.get("expected_keywords")
        case_kw = case.get("expected_keywords")
        if inv.get("expected_status") == "answered":
            if case_kw != inv_kw:
                errors.append(
                    f"{prefix}: expected_keywords must match fixture_fact_inventory for {cid}"
                )
        elif case_kw is not None:
            errors.append(
                f"{prefix}: expected_keywords must be omitted when expected_status is not_covered"
            )
    return errors


def cross_check_scores_vs_eval_cases(
    scores_data: dict[str, Any], eval_data: dict[str, Any]
) -> list[str]:
    errors: list[str] = []
    eval_ids: list[str] = []
    for c in eval_data.get("cases", []):
        if isinstance(c, dict) and isinstance(c.get("case_id"), str):
            eval_ids.append(c["case_id"])
    eval_set = set(eval_ids)
    if len(eval_ids) != len(eval_set):
        errors.append("eval_cases: duplicate case_id")
        return errors

    targets = scores_data.get("case_targets")
    if not isinstance(targets, list):
        return ["expected_scores.case_targets must be an array"]

    score_ids: list[str] = []
    for t in targets:
        if isinstance(t, dict) and isinstance(t.get("case_id"), str):
            score_ids.append(t["case_id"])
    score_set = set(score_ids)
    if len(score_ids) != len(score_set):
        errors.append("expected_scores: duplicate case_id in case_targets")

    if eval_set != score_set:
        missing = sorted(eval_set - score_set)
        extra = sorted(score_set - eval_set)
        if missing:
            errors.append(
                "expected_scores missing case_ids: " + ", ".join(missing)
            )
        if extra:
            errors.append(
                "expected_scores has unknown case_ids: " + ", ".join(extra)
            )

    if eval_ids != score_ids:
        errors.append(
            "expected_scores.case_targets order must match eval_cases.cases order "
            f"(eval: {eval_ids}, scores: {score_ids})"
        )
    return errors


def validate_eval_dataset_bundle(
    *,
    eval_cases_path: Path = DEFAULT_EVAL_CASES,
    expected_scores_path: Path = DEFAULT_EXPECTED_SCORES,
    inventory_path: Path = DEFAULT_INVENTORY,
    require_inventory_alignment: bool = True,
) -> list[str]:
    errors: list[str] = []
    try:
        eval_data = load_json(eval_cases_path)
    except (OSError, json.JSONDecodeError) as e:
        return [f"eval_cases file: {e}"]
    errors.extend(
        f"eval_cases schema: {m}" for m in validate_against_schema(eval_data, SCHEMA_EVAL_CASES)
    )

    try:
        scores_data = load_json(expected_scores_path)
    except (OSError, json.JSONDecodeError) as e:
        return errors + [f"expected_scores file: {e}"]
    errors.extend(
        f"expected_scores schema: {m}"
        for m in validate_against_schema(scores_data, SCHEMA_EXPECTED_SCORES)
    )

    errors.extend(
        f"cross_check scores/eval: {m}"
        for m in cross_check_scores_vs_eval_cases(scores_data, eval_data)
    )

    if require_inventory_alignment:
        try:
            inv_data = load_json(inventory_path)
        except (OSError, json.JSONDecodeError) as e:
            errors.append(f"inventory file: {e}")
            return errors
        errors.extend(
            f"inventory alignment: {m}"
            for m in cross_check_eval_cases_inventory(eval_data, inv_data)
        )
    return errors


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--eval-cases",
        type=Path,
        default=DEFAULT_EVAL_CASES,
        help="Path to eval_cases JSON",
    )
    parser.add_argument(
        "--expected-scores",
        type=Path,
        default=DEFAULT_EXPECTED_SCORES,
        help="Path to expected_scores JSON",
    )
    parser.add_argument(
        "--inventory",
        type=Path,
        default=DEFAULT_INVENTORY,
        help="Path to fixture_fact_inventory.json",
    )
    parser.add_argument(
        "--no-inventory-check",
        action="store_true",
        help="Skip alignment check against fixture inventory",
    )
    args = parser.parse_args(argv)

    errs = validate_eval_dataset_bundle(
        eval_cases_path=args.eval_cases.resolve(),
        expected_scores_path=args.expected_scores.resolve(),
        inventory_path=args.inventory.resolve(),
        require_inventory_alignment=not args.no_inventory_check,
    )
    if errs:
        print("Validation failed:", file=sys.stderr)
        for e in errs:
            print(f"  - {e}", file=sys.stderr)
        return 2
    print("OK: eval_cases + expected_scores validate")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
