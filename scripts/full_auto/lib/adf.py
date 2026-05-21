#!/usr/bin/env python3
"""SCRUM-542: minimal ADF (Atlassian Document Format) → Markdown converter.

Used by ``start.py`` to feed Jira ticket descriptions into
``scripts/jira_ticket_lint.py``. The lint script parses ATX headings
(``#``/``##``) and task-list checkboxes (``- [ ]``), so the converter only
needs to render those node shapes faithfully. Other node types degrade to
plain text — the lint won't care, and the agent doesn't read the
converted body anyway (it goes straight to the lint subprocess).

Reused by ``review.py`` (SCRUM-543) for any future ADF-handling needs.
"""

from __future__ import annotations

from typing import Any


def _text(node: dict) -> str:
    """Recursively extract plain text from an ADF node's content array."""
    if node.get("type") == "text":
        return node.get("text", "")
    parts: list[str] = []
    for child in node.get("content", []) or []:
        parts.append(_text(child))
    return "".join(parts)


def adf_to_md(adf: Any) -> str:
    """Convert an ADF document to Markdown for the lint script.

    The converter handles the four shapes the lint cares about:
    ``heading`` (→ ATX), ``paragraph`` (→ plain line), ``bulletList`` (→
    ``- ``), and ``taskList`` (→ ``- [ ]`` / ``- [x]``). Other nodes
    degrade to their text content on a paragraph.

    For ticket descriptions stored as a single text node with embedded
    Markdown syntax (the common shape produced by ``jira_update_issue``
    with a Markdown body), the text content already contains the
    headings + checkboxes the lint needs; the converter just unwraps
    the ADF envelope.
    """
    if not isinstance(adf, dict):
        return ""
    parts: list[str] = []
    for node in adf.get("content", []) or []:
        t = node.get("type")
        if t == "heading":
            level = int(node.get("attrs", {}).get("level", 1) or 1)
            parts.append("#" * level + " " + _text(node))
        elif t == "bulletList":
            for li in node.get("content", []) or []:
                parts.append("- " + _text(li).strip())
        elif t == "taskList":
            for item in node.get("content", []) or []:
                state = item.get("attrs", {}).get("state", "TODO")
                marker = "[x]" if state == "DONE" else "[ ]"
                parts.append(f"- {marker} " + _text(item).strip())
        else:
            # paragraph / blockquote / fallback — render the inner text.
            parts.append(_text(node))
        parts.append("")
    return "\n".join(parts).rstrip() + "\n"


__all__ = ["adf_to_md"]
