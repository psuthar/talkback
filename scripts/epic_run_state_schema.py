#!/usr/bin/env python3
"""SCRUM-489: schema validator for .epic-run/<EPIC>.json state files.

Provides a small, dependency-free validator over the ``halt_category`` enum
documented in ``.claude/skills/epic-run/SKILL.md``. Consumers (KPI snapshot,
future tooling, manual sanity checks) can confirm enum conformance and the
``other``-requires-``halt_reason`` invariant without pulling in jsonschema.

The validator is intentionally narrow: it does NOT enforce the full state
shape (status enum, tickets list shape, etc.). Its scope is the new fields
introduced by SCRUM-489 plus their pairing rule.

Backward compatibility: legacy state files written before this enum landed
have no ``halt_category`` field. ``validate_state`` treats a missing field
as ``None`` and reports no error — none of the 17 pre-existing files need
migration.
"""

from __future__ import annotations

from typing import Iterable


HALT_CATEGORY_VALUES = frozenset(
    {
        "spec_missing",
        "gate_warn",
        "gate_block",
        "mergeable_blocked",
        "timeout",
        "human_requested_halt",
        "other",
    }
)

# SCRUM-493: extended status enum for the authoring phase.
VALID_STATUSES = frozenset(
    {
        "authoring",
        "awaiting_approval",
        "running",
        "halted",
        "complete",
    }
)

# Legacy / historical status values that exist in pre-SCRUM-493 state files
# but should not be produced by new code. validate_status tolerates these so
# the test suite can confirm every existing file still passes; new state files
# must use values from VALID_STATUSES.
LEGACY_STATUSES = frozenset({"epic_deferred_for_sunset"})

ALL_KNOWN_STATUSES = VALID_STATUSES | LEGACY_STATUSES

# SCRUM-493: per-Epic LOC threshold override range.
MAX_ESTIMATED_LOC_MIN = 100
MAX_ESTIMATED_LOC_MAX = 800
MAX_ESTIMATED_LOC_DEFAULT = 400


def validate_halt_category(value) -> str | None:
    """Return ``None`` if ``value`` is ``None`` or a known enum member; else an error."""
    if value is None:
        return None
    if value in HALT_CATEGORY_VALUES:
        return None
    return (
        f"halt_category {value!r} not in enum: "
        f"{sorted(HALT_CATEGORY_VALUES)}"
    )


def validate_status(value) -> str | None:
    """SCRUM-493: validate status field against the extended enum.

    Accepts ``VALID_STATUSES`` (the canonical enum for new state files) plus
    ``LEGACY_STATUSES`` (historical values that exist in pre-SCRUM-493 files).
    """
    if value is None:
        return None
    if value in ALL_KNOWN_STATUSES:
        return None
    return f"status {value!r} not in enum: {sorted(VALID_STATUSES)}"


def validate_max_estimated_loc(value) -> str | None:
    """SCRUM-493: max_estimated_loc must be int in [100, 800] (or None)."""
    if value is None:
        return None
    # bool is a subclass of int; reject explicitly.
    if isinstance(value, bool) or not isinstance(value, int):
        return f"max_estimated_loc must be int, got {type(value).__name__}"
    if not (MAX_ESTIMATED_LOC_MIN <= value <= MAX_ESTIMATED_LOC_MAX):
        return (
            f"max_estimated_loc {value} out of valid range "
            f"[{MAX_ESTIMATED_LOC_MIN}, {MAX_ESTIMATED_LOC_MAX}]"
        )
    return None


def _validate_other_requires_reason(
    halt_category, halt_reason, label: str
) -> Iterable[str]:
    if halt_category == "other" and not halt_reason:
        yield (
            f"{label}: halt_category 'other' requires non-empty halt_reason"
        )


def validate_state(state) -> list[str]:
    """Validate a state-file dict; return a list of error strings (empty = valid)."""
    if not isinstance(state, dict):
        return [f"state must be a dict, got {type(state).__name__}"]

    errors: list[str] = []

    err = validate_status(state.get("status"))
    if err:
        errors.append(f"root: {err}")

    err = validate_max_estimated_loc(state.get("max_estimated_loc"))
    if err:
        errors.append(f"root: {err}")

    root_category = state.get("halt_category")
    err = validate_halt_category(root_category)
    if err:
        errors.append(f"root: {err}")
    errors.extend(
        _validate_other_requires_reason(
            root_category, state.get("halt_reason"), "root"
        )
    )

    tickets = state.get("tickets") or []
    if not isinstance(tickets, list):
        errors.append(
            f"tickets must be a list, got {type(tickets).__name__}"
        )
        return errors

    for i, ticket in enumerate(tickets):
        if not isinstance(ticket, dict):
            errors.append(
                f"tickets[{i}]: must be a dict, got {type(ticket).__name__}"
            )
            continue
        prefix = f"tickets[{i}] (key={ticket.get('key')!r})"
        cat = ticket.get("halt_category")
        err = validate_halt_category(cat)
        if err:
            errors.append(f"{prefix}: {err}")
        errors.extend(
            _validate_other_requires_reason(
                cat, ticket.get("halt_reason"), prefix
            )
        )

    return errors
