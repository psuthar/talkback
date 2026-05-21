"""FULL_AUTO orchestration scripts (Epic SCRUM-529).

Modules in this package extract deterministic chunks of the
`implement SCRUM-XX FULL_AUTO` workflow into Python so they can be
called by either Claude Code (today) or a webhook listener (future).

Phase 0 (SCRUM-528): credential plumbing only — `lib/auth.py`.
Phase 1+: close.py + lib/jira.py + lib/github.py + lib/state.py + lib/templates.py.

See docs/agent/full-auto-scripts.md for the runbook.
"""
