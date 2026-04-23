#!/usr/bin/env python3
"""Validation helpers for eval/qa fixture-fact inventory."""

from __future__ import annotations

import json
from pathlib import Path
from typing import Any

VALID_STATUS = {"answered", "not_covered"}
MIN_CASE_COUNT = 25


def load_inventory(path: Path) -> dict[str, Any]:
    raw = path.read_text(encoding="utf-8")
    return json.loads(raw)


def validate_inventory(data: dict[str, Any]) -> list[str]:
    errors: list[str] = []
    cases = data.get("cases")

    if not isinstance(cases, list):
        return ["'cases' must be an array"]
    if len(cases) < MIN_CASE_COUNT:
        errors.append(f"'cases' must contain at least {MIN_CASE_COUNT} entries")

    seen_ids: set[str] = set()
    for idx, case in enumerate(cases):
        prefix = f"cases[{idx}]"
        if not isinstance(case, dict):
            errors.append(f"{prefix} must be an object")
            continue

        case_id = case.get("case_id")
        if not isinstance(case_id, str) or not case_id.startswith("FF-"):
            errors.append(f"{prefix}.case_id must be a string starting with 'FF-'")
        elif case_id in seen_ids:
            errors.append(f"duplicate case_id: {case_id}")
        else:
            seen_ids.add(case_id)

        question = case.get("question")
        if not isinstance(question, str) or not question.strip():
            errors.append(f"{prefix}.question must be a non-empty string")

        status = case.get("expected_status")
        if status not in VALID_STATUS:
            errors.append(
                f"{prefix}.expected_status must be one of {sorted(VALID_STATUS)}"
            )

        source_refs = case.get("source_refs")
        if not isinstance(source_refs, list) or len(source_refs) == 0:
            errors.append(f"{prefix}.source_refs must be a non-empty array")
        else:
            for sidx, ref in enumerate(source_refs):
                rprefix = f"{prefix}.source_refs[{sidx}]"
                if not isinstance(ref, dict):
                    errors.append(f"{rprefix} must be an object")
                    continue
                for key in ("source_file", "evidence"):
                    val = ref.get(key)
                    if not isinstance(val, str) or not val.strip():
                        errors.append(f"{rprefix}.{key} must be a non-empty string")

        if status == "answered":
            keywords = case.get("expected_keywords")
            if not isinstance(keywords, list) or len(keywords) == 0:
                errors.append(
                    f"{prefix}.expected_keywords must be a non-empty array when expected_status='answered'"
                )
            elif any(not isinstance(k, str) or not k.strip() for k in keywords):
                errors.append(f"{prefix}.expected_keywords must contain non-empty strings")

    return errors


def validate_inventory_file(path: Path) -> None:
    errors = validate_inventory(load_inventory(path))
    if errors:
        raise ValueError("fixture inventory validation failed:\n- " + "\n- ".join(errors))
