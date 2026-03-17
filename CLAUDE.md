# TalkBack — Claude Project Memory

This file provides durable repo-level instructions and project memory for Claude. Do not modify product code without following the workflow rules below.

---

## 1. Project Overview

TalkBack is an **AI-powered system** for turning recorded presentations and sessions into interactive knowledge. Creators upload Zoom recordings, documents, and transcripts; participants join sessions to watch content and ask questions with AI-generated answers and citations.

- **Core flow:** Creator creates session → adds video (Zoom import or upload) + materials (PDF, docs, slides) → participants join → watch video, read materials, ask questions → RAG answers with citations.
- **Modes:** Creator (edit session, manage materials, answer flow) and Participant (view, ask questions, mark materials seen).
- **Auth:** Cookie-based login; invite flow for participants; optional bootstrap admin.
- **Product direction:** Sessions are evolving toward a **decision-centric model**. The long-term goal includes **decision intelligence** (premise, primary decision, outcomes), not just generic meeting summaries or Q&A.

---

## 2. Architecture Summary

| Layer | Technology | Purpose |
|-------|------------|--------|
| **Frontend** | React 18, Vite 5 | SPA: sessions, materials, Q&A, invite flow, creator/participant modes (`web/`) |
| **Backend** | Go (stdlib `net/http`) | REST API, WebSocket, cookie + Bearer auth, CORS; single binary `cmd/api` |
| **Database** | PostgreSQL 16 | Users, sessions, materials, invitations, questions/answers, jobs (golang-migrate) |
| **Object storage** | Cloudflare R2 (S3-compatible) or local disk | Video files, uploads, presigned URLs; env-driven |
| **Integrations** | Zoom, transcript ingestion, AI/RAG, observability worker(s) | Zoom OAuth + recording import; Whisper/OpenAI; optional New Relic; `cmd/obsworker` |

**Key packages:** `internal/handlers` (HTTP + WebSocket), `internal/database`, `internal/models`, `internal/rag`, `internal/invitations`, `internal/auth`, `internal/processing`, `internal/storage`, `internal/transcript`, `internal/utils`.

---

## 3. Product Direction

- Sessions may include **premise** and **primary decision** (schema and UX evolving).
- Long-term goal: **decision intelligence** — structured decisions and outcomes derived from or attached to sessions, not only generic meeting summaries or free-form Q&A.
- Keep naming semantically clear: e.g. **decision topic** vs **decision outcome**; avoid overloading terms.

---

## 4. Development Workflow Expectations

When working in this repository, Claude must:

1. **Analyze before editing** — Identify affected modules, data flow, and API boundaries before changing code.
2. **Propose a plan before implementing** — Outline steps and touchpoints (backend, frontend, migrations) so changes stay minimal and backward-compatible.
3. **Implement in small, incremental steps** — Prefer single-purpose edits; avoid large refactors in one go.
4. **Prefer backward-compatible changes** — Avoid breaking existing APIs or client behavior; extend rather than replace where possible.
5. **Explain touched files after edits** — Summarize what was changed and why; call out migration or deployment considerations.
6. **Run focused tests after backend changes** — Run `go test ./...` or affected packages and report failures.
7. **Avoid broad restyling when making frontend changes** — Preserve existing UI patterns and styling unless explicitly requested.

---

## 5. Repo-Specific Guidance

- **Follow existing patterns** before introducing new abstractions.
- **Keep naming semantically clean**, especially around “decision topic” vs “decision outcome” and session/premise/decision concepts.
- **Do not change product code unless explicitly requested** — only add or adjust behavior when the user asks for it.
- **API style:** REST; routes under `/sessions/{id}/...` and `/api/...`; CORS with credentials; WebSocket for session updates.

---

## 6. Repository Development Commands

- **DB:** `docker compose -f deploy/docker-compose.yml up -d`; migrations on API startup when `RUN_MIGRATIONS=true`.
- **API:** `go run ./cmd/api` (default port 8080; `PORT` override).
- **Web:** `cd web && npm install && npm run dev` (e.g. http://localhost:3000).
- **Tests:** `go test ./...` (no frontend test runner in `web/package.json`).
- **Manual API checks:** `requests.http`; `make auth-check` / `scripts/auth_check.sh`.

---

## Subagent routing

Use the `talkback-e2e-fixer` subagent for tasks that involve:
- running Playwright tests
- diagnosing E2E failures
- fixing selectors, waits, fixture/setup issues, or minor UI bugs revealed by browser tests

Use the `smoke-tests` skill for API/integration test creation.
Use the `e2e-tests` skill for browser-test authoring conventions.

---

## Test routing

Use the `smoke-tests` skill for creating or refining deterministic backend smoke/integration tests.

Use the `talkback-smoke-fixer` subagent for:
- running smoke tests
- diagnosing smoke test failures
- fixing smoke tests or small backend issues revealed by them
- expanding smoke coverage while following repo smoke-test conventions

Use the `e2e-tests` skill and `talkback-e2e-fixer` subagent for browser-based workflow validation.

---

*This file is part of the Claude Code configuration for TalkBack. See README.md and `docs/architecture.md` for full product and architecture details.*
