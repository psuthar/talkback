"""Reviewer agent helpers (Epic SCRUM-508).

Modules in this package are runtime building blocks for the talkback-reviewer
agent. They are pure logic — no Jira/GitHub network calls. Callers (CI
workflows, slash command handlers, dry-run scripts) pre-fetch data and pass
it in as dataclasses.
"""
