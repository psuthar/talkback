# TalkBack Agent Policy Overview

Source of truth: This file owns repository context and high-level execution principles for all agents. Workflow-specific rules live in the other `docs/agent/*` files.

## Project Overview

TalkBack is an AI-powered system for turning recorded presentations and sessions into interactive knowledge. Creators upload Zoom recordings, documents, and transcripts; participants join sessions to watch content and ask questions with AI-generated answers and citations.

- Core flow: Creator creates session, adds video/materials, participants join, consume content, and ask RAG-backed questions.
- Modes: Creator (manage/edit session) and Participant (consume/ask/track seen materials).
- Auth: Cookie-based login, invite flow for participants, optional bootstrap admin.

## Architecture Summary

| Layer | Technology | Purpose |
|---|---|---|
| Frontend | React 18, Vite 5 | SPA for sessions, materials, Q&A, creator/participant modes (`web/`) |
| Backend | Go (`net/http`) | REST API, WebSocket, cookie + Bearer auth, single binary `cmd/api` |
| Database | PostgreSQL 16 | Users, sessions, materials, invitations, questions/answers, jobs |
| Object storage | Cloudflare R2 or local disk | Video files, uploads, presigned URLs |
| Integrations | Zoom, transcript ingestion, AI/RAG, observability workers | Recording import, transcription, generation, telemetry |

Key packages: `internal/handlers`, `internal/database`, `internal/models`, `internal/rag`, `internal/invitations`, `internal/auth`, `internal/processing`, `internal/storage`, `internal/transcript`, `internal/utils`.

## Product Direction

- Sessions are evolving toward a decision-centric model with premise + primary decision.
- Long-term goal is decision intelligence (structured decisions/outcomes), not generic summaries only.
- Keep naming semantically clean (for example, decision topic vs decision outcome).

## Development Workflow Expectations

When working in this repository, agents must:

1. Analyze before editing.
2. Propose a plan before implementing.
3. Implement in small, incremental steps.
4. Prefer backward-compatible changes.
5. Explain touched files after edits.
6. Run focused tests after backend changes.
7. Avoid broad frontend restyling unless requested.

## Karpathy-style Execution Guardrails

These apply to all implementation work:

1. Think before coding: surface assumptions and ambiguity.
2. Prefer minimal solutions: avoid speculative abstractions.
3. Make surgical edits: touch only required files/lines.
4. Tie implementation to verification before completion.
5. Prefer and explain simpler approaches when available.

Tradeoff: these rules bias toward correctness/caution over speed; for trivial tasks, keep execution lightweight while still verifying outcomes.

## Repo-specific Guidance

- Follow existing patterns before introducing abstractions.
- Keep terminology semantically clear around decision model concepts.
- Do not change product behavior unless explicitly requested.
- API style is REST with routes under `/sessions/{id}/...` and `/api/...`; use credentialed CORS where needed and WebSocket for session updates.

## Observability → Jira loop

The observability agent (`.github/workflows/observability-agent.yml` + `cmd/obsworker/`) files daily GitHub issues with labels `[observability, agent]` on YELLOW or RED status. The `discovery-digest` skill (`.claude/skills/discovery-digest/SKILL.md`) and its weekly workflow (`.github/workflows/discovery-digest.yml`, SCRUM-497) bridge those signals into proposed Jira tickets — dedup against existing `source:obs-agent`-labelled Jira tickets, cluster by endpoint, render a Markdown proposal, and on operator approval call `jira_create_issue` with a remote link back to the obs issue. The skill never auto-creates; the cron workflow only surfaces candidates as a tracking issue and stops at the approval gate.

## TalkBack reviewer (PR-time AI review)

The talkback-reviewer agent (Epic SCRUM-508) comments on PRs at open with high-signal observations — risk-surface framing, missing test coverage on the changed surface, cross-file behavior deltas, regression sniffing on consolidated branches. Scope is bounded by `.github/talkback-reviewer/SCOPE.md` (the policy contract — any prompt change must update the contract first). The prompt itself lives at `.github/talkback-reviewer/PROMPT.md` and pins to a specific SCOPE.md commit SHA. The reviewer skips draft PRs, docs-only diffs, `skip-reviewer`-labelled PRs, bot authors, and source-LOC under threshold; explicit summon via `/talkback-review` bypasses the filter. A daily token-budget cap stops cost runaway. Reviewer is silent when it has nothing specific to say — a blank review is a successful outcome. Phase 1 calibration scoring rubric: `docs/agent/reviewer-calibration.md`; raw log at `ops/define-kpis/reviewer-calibration.csv`; weekly summary via `python3 scripts/reviewer/calibration_summary.py`.

## FULL_AUTO orchestration scripts (close.py extraction)

Epic SCRUM-529 extracts the deterministic post-merge close-out of `implement SCRUM-XX FULL_AUTO` into `scripts/full_auto/` — a Python package callable from Claude today and from a webhook listener later. Phase 0 (SCRUM-528) lands the credential plumbing only (`scripts/full_auto/lib/auth.py` + `.env.local.example` + this runbook). Phases 1-3 ship `close.py` itself, validate it via dry-run, and cut CLAUDE.md §8 over to call it. Runbook + setup instructions: `docs/agent/full-auto-scripts.md`. Token rotation guidance + deployment-target matrix included.

