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
| **Backend** | Go (stdlib `net/http`) | REST API, WebSocket, cookie + Bearer auth; optional CORS for split-origin dev; single binary `cmd/api` |
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
- **API style:** REST; routes under `/sessions/{id}/...` and `/api/...`; credentialed CORS where needed for split-origin; WebSocket for session updates.

---

## 6. Repository Development Commands

- **DB:** `docker compose -f deploy/docker-compose.yml up -d`; migrations on API startup when `RUN_MIGRATIONS=true`.
- **API:** `go run ./cmd/api` (default port 8080; `PORT` override).
- **Web:** `cd web && npm install && npm run dev` (e.g. http://localhost:3000).
- **Tests:** `go test ./...` (no frontend test runner in `web/package.json`).
- **TalkBack MCP (agents):** `./scripts/setup-mcp-config.sh` (Cursor + Claude Code local MCP config) or `TALKBACK_MCP_API_KEY=<secret> go run ./cmd/talkback-mcp` — see `docs/mcp-server.md`.
- **Manual API checks:** `requests.http`; `make auth-check` / `scripts/auth_check.sh`.

---

## 7. Jira Ticket Execution Workflow

When the user requests implementation of a Jira ticket (e.g. "Implement SCRUM-12"), follow this workflow:

### Jira Status Management
- Before any code edits, test execution, or **implementation commits**, move the Jira ticket to:
  In Progress
- When all implementation, validation, and PR creation are complete, move the Jira ticket to:
  In Review

### Jira Status Enforcement (MANDATORY)
- Required implementation sequence:
  1) Transition ticket to **In Progress**
  2) **Create the feature branch from `main`** (`feat/<ticket-number>`) and do all implementation work on that branch—**before** writing product code or committing implementation changes, check out the branch so passing tests map cleanly to a PR from that branch.
  3) Implement + validate (commits on the feature branch only)
  4) Push branch and create PR
  5) Transition ticket to **In Review**
- Hard-stop rules:
  - Do not modify product code, run implementation tests, or open/finalize a PR until step (1) is complete.
  - Do not put implementation work directly on `main`; create `feat/<ticket>` first, then commit there.
  - Do not transition to **In Review** before PR creation is complete.
- Verification evidence required in completion output:
  - confirmation that **In Progress** transition was applied
  - confirmation that **In Review** transition was applied
  - PR URL
  - confirmation that a **structured Jira completion comment** was posted (see **Jira completion comment** below)
- If a transition is missed:
  - immediately correct status sequence in Jira
  - add a Jira comment noting correction and linking the implementation branch/PR

### Branching
- **Order:** After **In Progress**, create and check out the branch **before** implementation commits (e.g. `git fetch origin`, `git checkout main`, `git pull`, `git checkout -b feat/<ticket-number>`).
- Branch name:
  feat/<ticket-number>
- Example:
  feat/SCRUM-12
- Rationale: if tests pass, you push the same branch and open the PR without moving commits off `main` or cherry-picking.

### Scope & Approach
- Read and understand the Jira ticket before coding
- Implement only the requested scope unless additional changes are required for correctness
- Prefer minimal, clean, well-structured changes
- Do not introduce unrelated refactors

### Testing
- Add or update automated tests for all new or changed behavior
- Ensure meaningful coverage
- Do not skip tests

### Validation
- Run relevant validation before completion:
  - backend tests (`go test ./...`)
  - any affected integration or E2E flows
- Do not proceed if validation fails

### Jira completion comment (MANDATORY)
When implementation and the PR are ready (before or immediately after transitioning to **In Review**), add a **regular issue comment** on the Jira ticket—not only transition text—so the Comments tab has a durable record. Use this structure (same spirit as SCRUM-15):

1. **Opening line:** `{TICKET} complete.` (or `Implementation complete.`) plus the **full PR URL** (e.g. `https://github.com/psuthar/talkback/pull/N`).
2. **Delivered:** bullet list of concrete outcomes (what shipped: behavior, APIs, migrations, notable files or subsystems—enough for support/product to skim).
3. **Validation:** bullet list of **exact commands** run (e.g. `go test ./...`, targeted packages, smoke/E2E if applicable) and pass/fail outcome.
4. **Risks / deployment:** short bullets if relevant (migrations, ordering, env flags, backward compatibility).
5. **Follow-up:** optional bullets (tech debt, future tickets, monitoring).

If Jira MCP or API is available, use it to post this comment; otherwise note in completion output that the user should paste it. Do not rely on GitHub–Jira dev links alone to replace this narrative.

**MCP (@xuandev/atlassian-mcp):** To add the comment with **`jira_add_comment`**, pass the text as **`body`** (required). Using **`comment`** instead returns `INVALID_INPUT`. See `.cursor/rules/atlassian-mcp-jira.mdc`.

### Jira Updates
- Beyond the completion comment above, add mid-flight notes when useful (blockers, scope clarifications).
- Do not silently expand scope.

### Commits
- Use commit messages prefixed with the ticket number
- Example:
  SCRUM-12: add session state evaluator

### Pull Request
- Push branch to GitHub
- Create PR targeting `main`

### Git authentication for `git push` (HTTPS / Cursor)

Step 4 requires **`git push`**. With an `https://github.com/...` remote, push can fail in the **Cursor agent** (or any non-interactive subprocess) with:

`fatal: could not read Username for 'https://github.com': Device not configured`

Read-only checks like `git ls-remote origin HEAD` may still succeed; **push needs write credentials** and Git cannot prompt without a TTY.

**One-time setup on each machine (recommended):**

1. Install [GitHub CLI](https://cli.github.com/): `brew install gh` (macOS).
2. `gh auth login` — choose **HTTPS** and finish browser or token login.
3. `gh auth setup-git` — wires Git to use `gh auth git-credential` for `github.com`, so credentials are supplied **without** an interactive prompt (required for agent push).

**PATH:** If `gh` is missing in Cursor, Homebrew is often not on `PATH` for GUI apps. This repo includes `.vscode/settings.json` prepending `/opt/homebrew/bin` for the **integrated terminal** on macOS. If the **agent** still cannot find `gh`, either add the same `PATH` in **Cursor → Settings → search “env”** (user settings JSON), or **launch Cursor from a terminal** (`cursor .` in the repo) so it inherits your shell `PATH`.

**Alternative:** Use an SSH remote (`git@github.com:<owner>/<repo>.git`) with a key loaded in `ssh-agent` and `github.com` in `known_hosts`.

**If agent push still fails:** Run `git push -u origin feat/<ticket>` in **Cursor’s integrated terminal** after the one-time setup above. That still completes the workflow; create/update the PR on GitHub using **GitHub MCP** (not `gh`) as below.

**PR description format (match SCRUM-15 / PR-quality comments):** Use clear Markdown with these sections:

1. **Plan (executed)** — numbered list of what you did in order.
2. **Summary of changes** — bullets; nest file paths or subsystems under top-level bullets when helpful.
3. **Validation** — bullets with exact commands run (and pass/fail if relevant).
4. **Acceptance criteria coverage** — bullets mapping the work to the ticket’s acceptance criteria (or goals).
5. **Refs:** ticket key(s), e.g. `SCRUM-12`, parent epic if useful.

Also cover risks, follow-ups, and Jira reference where they fit (e.g. under Summary or a short **Risks / follow-up** subsection).

**Creating/updating PRs on GitHub.com:** Use **GitHub MCP** only (read the server’s tool schema, then call the appropriate create/update PR tools). Do **not** use `gh pr create`, `gh pr edit`, or `curl` for GitHub when MCP can do the job.

**PR body and Markdown:** Draft the full body in a scratch file or string so formatting stays correct (fenced code blocks, paths, **bold**). Pass that body through the MCP PR tool’s parameters—avoid cramming unescaped markdown through a shell where **backticks** or escapes can break (e.g. PowerShell).

Remove or `.gitignore` local `pr-body.md` if you do not want it committed; or commit it only when the team wants a permanent record.

### Completion Output
Return (mirror the Jira completion comment where applicable):
- branch name
- validations executed (commands + outcome)
- PR link
- Jira status transition confirmations (In Progress and In Review)
- confirmation that the structured Jira completion comment was posted (or paste the comment body if posting failed)
- summary of changes
- follow-up actions

---

## 8. Planning Mode Behavior

If the user explicitly requests planning (e.g. "Plan SCRUM-13"):

- Do NOT:
  - write code
  - create branches
  - update Jira status

- Instead:
  - analyze scope
  - identify impacted systems (backend, frontend, DB)
  - define test strategy
  - highlight risks and unknowns
  - propose implementation plan

Wait for confirmation before proceeding to implementation.

---

## Subagent routing

Route tasks to specialized TalkBack subagents as follows:

- `talkback-architect`
  - multi-file design work spanning backend + frontend + data model
  - migrations, API contract changes, and rollout/backward-compatibility decisions
  - decision/premise model evolution where naming and structure matter

- `talkback-backend`
  - Go API handlers, database access, auth/session, invitations, processing, RAG, storage
  - endpoint behavior fixes, business logic updates, and backend test updates

- `talkback-frontend`
  - React/Vite UI changes in `web/` (creator/participant modes, materials, Q&A, invite flows)
  - client-side state, API wiring, and UI behavior fixes that do not require broad UX redesign

- `talkback-ux`
  - interaction design and usability improvements
  - layout/content hierarchy updates that should preserve existing visual language unless requested otherwise

- `talkback-reviewer`
  - code review tasks focused on regressions, risk, and missing tests
  - use when the user asks for a review or wants a quality/risk pass

- `talkback-e2e-fixer`
  - running Playwright tests
  - diagnosing E2E failures
  - fixing selectors, waits, fixture/setup issues, or minor UI bugs revealed by browser tests

- `talkback-smoke-fixer`
  - running smoke/integration tests
  - diagnosing smoke failures
  - fixing smoke tests or small backend issues revealed by smoke runs
  - expanding smoke coverage while following repo smoke-test conventions

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
Use `talkback-reviewer` for post-change risk review and missing-test detection.

---

*This file is part of the Claude Code configuration for TalkBack. See README.md and `docs/architecture.md` for full product and architecture details.*
