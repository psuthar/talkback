#!/usr/bin/env python3
"""SCRUM-530: epic-run state-file helpers for close.py.

State files live at ``.epic-run/<EPIC>.json`` and track the work-list for
each Epic. close.py updates the entry for a single ticket when that
ticket's PR merges. If the ticket isn't part of any state file (a
standalone FULL_AUTO not under an epic), this module no-ops.
"""

from __future__ import annotations

import json
from pathlib import Path

STATE_DIR = Path(".epic-run")


def find_state_file(ticket: str, repo_root: Path | None = None) -> Path | None:
    """Search ``.epic-run/*.json`` for the file whose work_list contains ``ticket``.

    Returns the path, or ``None`` if no state file references the ticket.
    Cheap O(n) over a handful of files; intentional for simplicity.
    """
    root = repo_root or Path.cwd()
    state_dir = root / STATE_DIR
    if not state_dir.is_dir():
        return None
    for path in sorted(state_dir.glob("*.json")):
        try:
            data = json.loads(path.read_text())
        except (OSError, json.JSONDecodeError):
            continue
        for item in data.get("work_list", []) or []:
            if item.get("key") == ticket:
                return path
    return None


def mark_done(
    state_path: Path,
    ticket: str,
    *,
    pr_number: int,
    merged_sha: str,
    final_gate: str,
) -> bool:
    """Mark ``ticket`` as done in the state file. Returns True if the file
    was actually mutated (False if the entry was already at status=done
    with matching merge SHA).
    """
    data = json.loads(state_path.read_text())
    changed = False
    for item in data.get("work_list", []) or []:
        if item.get("key") != ticket:
            continue
        if (
            item.get("status") == "done"
            and item.get("merged_sha") == merged_sha
            and item.get("pr") == pr_number
        ):
            return False
        item["status"] = "done"
        item["pr"] = pr_number
        item["merged_sha"] = merged_sha
        item["final_gate"] = final_gate
        changed = True
        break
    if changed:
        state_path.write_text(json.dumps(data, indent=2, sort_keys=False) + "\n")
    return changed


__all__ = ["STATE_DIR", "find_state_file", "mark_done"]
