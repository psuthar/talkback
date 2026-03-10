# Feature Development — Orchestrated Multi-Agent Workflow

This document describes the **recommended orchestration workflow** when using Claude’s multi-agent setup for TalkBack feature work. Planning should come before implementation; implementation should be broken into small, approved phases; the reviewer should explicitly call out naming, migration, and testing concerns.

---

## Sequence

### 1. ARCHITECT analyzes and proposes design

- Use the **feature-plan skill** and/or the **talkback-architect** agent.
- Output: problem definition, relevant architecture, minimal schema/API/UI changes, implementation steps, risks, MVP vs future.
- No code edits; only plans and recommendations.
- **Guidance:** Planning should come before implementation. If the feature is large, break it into phases and get agreement on Phase 1 before coding.

### 2. BACKEND implements backend portion

- Use the **talkback-backend** agent (and the Architect’s plan).
- Implement in order: schema → migrations → models → handlers (or workers) → tests.
- Run targeted Go tests; fix failures; summarize changed files and migration notes.
- **Guidance:** Implementation should be broken into small approved phases. Do not expand scope beyond the agreed plan unless explicitly requested.

### 3. FRONTEND implements UI portion

- Use the **talkback-frontend** agent (and the Architect’s plan).
- Implement: state/props → API calls → UI; preserve existing patterns and API compatibility.
- Summarize changed files and assumptions.
- **Guidance:** Keep UX simple for MVP; avoid unnecessary styling churn.

### 4. REVIEWER audits result

- Use the **talkback-reviewer** agent.
- Review proposed or completed changes for: regressions, edge cases, naming issues, migration risks, testing gaps.
- **Guidance:** Reviewer should explicitly call out naming (e.g. decision topic vs outcome), migration (backfills, deployment order), and testing (focused verification steps). Do not make broad unsolicited changes; only recommend or apply minimal targeted fixes if asked.

### 5. Claude summarizes changed files and verification steps

- List all touched files and a one-line reason for each.
- List verification steps: e.g. `go test ./internal/handlers/...`, “Test invite flow in browser”, “Verify session load with participant_ref”.
- Call out any deployment or migration follow-up (e.g. run migrations, feature flag).

---

## Principles

- **Planning before implementation** — Architect (and feature-plan skill) first; then Backend and Frontend in sequence.
- **Small approved phases** — Avoid big-bang changes; get each phase reviewed before moving on.
- **Explicit reviewer focus** — Naming, migration, and testing concerns must be called out; reviewer does not refactor unrelated code.
- **No product code changes unless requested** — Agents and skills do not modify application behavior except to fulfill an explicit user request.
