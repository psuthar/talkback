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
- **TalkBack MCP (agents):** `./scripts/setup-mcp-config.sh` (Cursor + Claude Code local MCP config) or `TALKBACK_MCP_API_KEY=<secret> go run ./cmd/talkback-mcp` — see `docs/mcp-server.md`. **Remote mode:** set `TALKBACK_MCP_URL=<deployed-url>` before running `setup-mcp-config.sh` to generate a `url`-based config (StreamableHTTP, no local Go process needed); see `docs/mcp-server.md` "Remote deployment" section. **`get_session_decisions` JSON contract (SCRUM-57):** `docs/mcp-session-decisions-schema.md`, `docs/schemas/mcp-session-decisions-v1.schema.json`. **`get_session_action_items` JSON contract (SCRUM-58):** `docs/mcp-session-action-items-schema.md`, `docs/schemas/mcp-session-action-items-v1.schema.json`. **Structured intelligence fallbacks (SCRUM-62):** `docs/mcp-structured-intelligence-fallbacks.md` (empty/partial success vs tool errors for decisions and action-item tools).
- **Manual API checks:** `requests.http`; `make auth-check` / `scripts/auth_check.sh`.

---

## 7. MCP Servers

Three MCP servers are configured for this project. Both `.cursor/mcp.json` (Cursor) and `.mcp.json` (Claude Code) wire these up; run `./scripts/setup-mcp-config.sh` to regenerate both files.

### `talkback` — TalkBack internal tools
- **Command:** `go run /Users/psuthar/code/talkback/cmd/talkback-mcp -version=dev`
- **Tools:** `health_check`; with `DATABASE_URL`: `get_session_metadata`, `get_session_decisions` / `get_decisions` (same handler — SCRUM-60), `get_session_action_items` / `get_action_items` (same handler — SCRUM-61), `search_session` (same handler as `search_session_content`), `get_session_raw_chunks` (same handler as `get_session_retrieval_context`), `get_session_source_chunks`, `ask_session` (same handler as `ask_session_question`) (session tools need DB; search, raw retrieval, action items, ask, and source-chunk listing when indexing need `OPENAI_API_KEY` — see env vars)
- **Env vars** (all are process-level env vars; `./scripts/setup-mcp-config.sh` copies `DATABASE_URL`, `TALKBACK_MCP_ACTING_USER_ID`, and `TALKBACK_MCP_KEY_USER_MAP_JSON` into `.cursor/mcp.json` when set in the shell you run it from):
  - `TALKBACK_MCP_URL` — **remote mode only (SCRUM-85/89):** set to the talkback-api URL + `/mcp` path (e.g. `https://talkback-api.onrender.com/mcp`) before running `setup-mcp-config.sh`. When set, the script generates a `url`-based config entry (StreamableHTTP) instead of `command`/`args`; no local Go process is needed. The `Authorization: Bearer <TALKBACK_MCP_API_KEY>` header is included automatically. Unset = local stdio mode (default, unchanged). **Note (SCRUM-89):** MCP is mounted at `/mcp/` on `talkback-api` — not a separate service; the URL must include the `/mcp` suffix.
  - `TALKBACK_MCP_HTTP_ADDR` — listen address for HTTP/StreamableHTTP transport (e.g. `:8080`); falls back to `PORT` env var (Render.com injects this automatically). When neither is set, the binary runs in stdio mode.
  - `TALKBACK_MCP_API_KEY` — shared secret for the MCP server
  - `TALKBACK_MCP_REQUIRE_CLIENT_KEY` — set `false` in dev
  - `DATABASE_URL` — Postgres connection string; enables session DB tools
  - `TALKBACK_MCP_ACTING_USER_ID` — acting user UUID for session tools (fallback when `TALKBACK_MCP_KEY_USER_MAP_JSON` does not list the client key)
  - `TALKBACK_MCP_KEY_USER_MAP_JSON` — optional JSON map from API key string to `users.id` UUID (strict key mode only; SCRUM-70); see `docs/mcp-server.md`
  - `TALKBACK_MCP_MAX_EMBEDDING_CALLS_PER_SESSION_PER_MINUTE` — optional per-session query-embedding cap (default unlimited); see `docs/mcp-server.md` (SCRUM-54)
  - `OPENAI_API_KEY` — required for `search_session` / `search_session_content` and `get_session_raw_chunks` / `get_session_retrieval_context` (embeddings only), `get_session_action_items` (embeddings + one LLM call per invocation), `get_session_source_chunks` when the session index must be built (`EnsureSessionIndex`), and `ask_session` / `ask_session_question` (embeddings + LLM answer generation)
  - **`STORAGE_DRIVER=r2`** plus the same R2 env vars as `cmd/api` — optional; enables MCP RAG indexing parity with HTTP for R2-stored PDFs (`EnsureSessionIndex` / `IndexSession`; SCRUM-49)
- **Codespace users:** Set `TALKBACK_MCP_URL` and `TALKBACK_MCP_API_KEY` as GitHub Codespace secrets, then run `setup-mcp-config.sh` — no `.bashrc` workaround or local Go process needed. See `docs/mcp-server.md` "Remote deployment → GitHub Codespace setup".

### `github` — GitHub operations
- **Command:** `docker run -i --rm -e GITHUB_PERSONAL_ACCESS_TOKEN ghcr.io/github/github-mcp-server`
- **Tools:** PR creation/review, issue management, file ops, code search, etc.
- **Env vars:**
  - `GITHUB_PERSONAL_ACCESS_TOKEN` — classic PAT with `repo` scope (Docker must be running)
- **Note:** Uses `github/github-mcp-server` (Go, official GitHub). The tool is **`pull_request_read` with `method: get`** — this is what agents must call for FULL_AUTO. The previously used `@modelcontextprotocol/server-github` (TypeScript, archived) exposed `get_pull_request` and dropped `mergeable_state` from responses; the current server returns it correctly. If the MCP payload still omits `mergeable_state`, treat it as "field absent" per §8 Post-PR automation — FULL_AUTO cannot run.

### `atlassian` — Jira & Confluence
- **Package:** `@xuandev/atlassian-mcp` (via `npx -y`)
- **Tools:** `jira_*` and `confluence_*` — issue lifecycle, sprints, boards, pages
- **Env vars:**
  - `ATLASSIAN_DOMAIN` — e.g. `suthar-team.atlassian.net`
  - `ATLASSIAN_EMAIL` — Atlassian account email
  - `ATLASSIAN_API_TOKEN` — Atlassian API token
- **Note:** `jira_add_comment` requires `body` param (not `comment`); see `.cursor/rules/atlassian-mcp-jira.mdc`.

---

## 8. Jira Ticket Execution Workflow

When the user requests implementation of a Jira ticket, two invocation modes are supported:

- **`implement SCRUM-XX`** — **Standard mode (default).** Run the workflow below through PR creation and Jira **In Review**. Stop there. No auto-merge, no branch cleanup, no Jira Done transition.
- **`implement SCRUM-XX FULL_AUTO`** — **Full automation mode.** Run the standard workflow, then follow **FULL_AUTO: Post-PR automation** below: call `pull_request_read (method: get)` via GitHub MCP; if `mergeable_state` is absent from the response, end FULL_AUTO immediately (hard stop, no polling); if present, apply the merge-gate table — **including when the first read is `blocked`:** poll every **30 seconds** on a single **40-minute** budget (shared with `null` / `unknown` / `unstable` / `behind`) until **`clean`** (then squash-merge, delete the remote branch, clean up local git, Jira **Done**), or until **`dirty`** (stop), the field becomes absent, or the budget expires without **`clean`**. If the gate never becomes **`clean`** within that budget (or ends in terminal **`dirty`**), same end state as standard mode: PR stays open, Jira stays **In Review**, user handles it from there.

### Jira Status Management
- Before any code edits, test execution, or **implementation commits**, move the Jira ticket to:
  In Progress
- When all implementation, validation, and PR creation are complete, move the Jira ticket to:
  In Review
- **FULL_AUTO only — on merge (PASS):** move the Jira ticket to:
  Done

### Jira Status Enforcement (MANDATORY)

**Standard mode sequence:**
  1) Transition ticket to **In Progress**
  2) **Create the feature branch from `main`** (`feat/<ticket-number>`) and do all implementation work on that branch—**before** writing product code or committing implementation changes, check out the branch so passing tests map cleanly to a PR from that branch.
  3) Implement + validate (commits on the feature branch only)
  4) Push branch and create PR
  5) Transition ticket to **In Review**

**FULL_AUTO mode sequence (extends standard):**
  1) Transition ticket to **In Progress**
  2) Create and checkout `feat/<ticket-number>` branch
  3) Implement + validate (commits on the feature branch only)
  4) Push branch and create PR
  5) Transition ticket to **In Review**
  6) Post Jira completion comment
  7) Follow **FULL_AUTO: Post-PR automation** (below) — if `mergeable_state` is present, poll until resolved, merge on `clean`, clean up branch, transition to Done. If absent, end FULL_AUTO immediately (no polling).

- Hard-stop rules:
  - Do not modify product code, run implementation tests, or open/finalize a PR until step (1) is complete.
  - Do not put implementation work directly on `main`; create `feat/<ticket>` first, then commit there.
  - Do not transition to **In Review** before PR creation is complete.
- Verification evidence required in completion output:
  - confirmation that **In Progress** transition was applied
  - confirmation that **In Review** transition was applied
  - PR URL
  - confirmation that a **structured Jira completion comment** was posted (see **Jira completion comment** below)
  - **FULL_AUTO only:** `mergeable_state` value observed (PASS = `clean`; BLOCK = terminal `dirty` or **`blocked`/`null`/etc. after 40-minute 30s polling**; do **not** stop on first `blocked`); if PASS — merge SHA, branch deletion confirmation, local cleanup confirmation, Jira **Done** transition confirmation
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

**Before writing any implementation code**, perform a test analysis:

1. Determine whether the ticket touches product code (Go packages, DB queries, MCP handlers, migrations, frontend, etc.) or is strictly documentation/config with no executable behavior. If strictly docs/config, skip to implementation.
2. For product-code tickets, identify the test gap:
   - What new behavior needs a test? (new function, new API, new DB query, new MCP tool)
   - What existing tests need updating? (changed signatures, changed behavior)
   - What paths are security- or correctness-sensitive? (ACL checks, session scoping, data boundaries — always test these even if surrounding code already has tests)
   - What test type fits? Unit test, DB integration test (real Postgres via `internal/test/testdb`), handler test (`setupTestHandlersParallel`), MCP behavioral test — match what the repo uses for similar code.
3. Record the test plan as a brief list before implementation. This is the acceptance bar: the ticket is not done until those tests are written, locally passing, and included in the PR.

**After implementation:**
- Add or update the tests identified above in the same commit/PR as the implementation.
- Run the affected packages locally before pushing (e.g. `go test ./internal/mcpserver/...` for MCP changes). Fix failures before pushing — CI is not a substitute for running tests locally first.
- Do not skip tests; do not defer them to a follow-up ticket.

### Validation
- Run relevant validation before completion:
  - backend tests (`go test ./...`)
  - any affected integration or E2E flows
- Do not proceed if validation fails
- **`go test ./...` partial failures:** If DB-backed packages fail solely due to a missing `DATABASE_URL`/`TEST_DATABASE_URL` and **none of those packages were touched** by the ticket, that is an acceptable known skip — document the exact failing packages and reason explicitly in the Jira completion comment. If any package **touched by the ticket** fails for any reason, that is a hard blocker.

### Jira completion comment (MANDATORY)
When implementation and the PR are ready (before or immediately after transitioning to **In Review**), add a **regular issue comment** on the Jira ticket—not only transition text—so the Comments tab has a durable record. **Transition-only comments** (`jira_transition_issue` `comment` parameter) **do not** reliably appear as top-level **Comments** tab entries in team-managed Jira; you must still post the narrative via **`jira_add_comment`** with **`body`**. Use this structure (same spirit as SCRUM-15):

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

### FULL_AUTO: Post-PR automation (FULL_AUTO mode only)

After the PR is created, use a single 40-minute polling budget for all non-terminal states. **`mergeable_state` from `pull_request_read` (method: `get`) is the sole authoritative merge gate for FULL_AUTO** — do not use the **legacy combined status API** as a parallel source of truth (it does not reflect Actions the same way). **Do not** merge based on a hunch from the Actions UI alone without a fresh `mergeable_state` read. When **branch protection** lists required status checks (e.g. `go-test`, `release-readiness`, TalkBack PR Gate), GitHub incorporates them into merge eligibility; **`mergeable_state: clean`** means the PR is mergeable under those rules—including required checks—not merely that some jobs finished.

**Hard stop — do not continue FULL_AUTO merge / Jira Done unless both are true:**

- **`TalkBack PR Gate` overall outcome is PASS** — in GitHub Checks terms, the check run named **`TalkBack PR Gate`** must have **`conclusion: success`**. Do **not** treat **`neutral`** (e.g. PR Gate **WARN**, often shown as `action_required` on the check run) or **`failure`** (**BLOCK**) as permission to merge. Release Readiness can score 100/100 while the **unified** gate is still **WARN**; only the **TalkBack PR Gate** check conclusion counts for this rule.
- **`mergeable_state` is `clean`** — per the table and pre-merge guard below (including the **immediate** pre-merge `pull_request_read`).

If **either** condition fails after the polling budget, **stop**: no `merge_pull_request`, no branch cleanup, no Jira **Done** — same end state as standard mode (**In Review**, PR open). **Do not** call `merge_pull_request` just because the API might accept it (e.g. admin bypass); that violates this workflow.

**Host “looping” / anti-repetition reminders (e.g. Cursor):** Merge-gate polling **must** repeat **`sleep 30`** + **`pull_request_read`** many times in a row while CI runs; **`blocked`** for **5+ minutes** is common. That repetition is **mandatory**, not an error. **Do not** stop early solely because the environment flags “looping” — continue until **`clean`**, **`dirty`**, field absent, or the **40-minute** budget expires. If the session cannot continue, tell the user to invoke **continue epic** / resume merge polling for that PR. See `.cursor/rules/full-auto-github-polling.mdc`.

1. **Call `pull_request_read` (method: `get`)** via GitHub MCP and inspect `mergeable_state`. Use this field — not the legacy status API, which does not see GitHub Actions check runs.

   | `mergeable_state` value | Action |
   |---|---|
   | **field absent** | **FULL_AUTO IS NOT AVAILABLE** — hard stop. No wait, no CI inference, no merge. PR stays open, Jira stays In Review. Report this limitation clearly. |
   | `null` | GitHub still computing — poll every 30s. |
   | `unknown` / `unstable` / `behind` | Not yet final — poll every 30s. |
   | `clean` | Mergeability OK → proceed to step 2 **only after** confirming **`TalkBack PR Gate` → `success`** (see Hard stop above). |
   | `blocked` | **Mandatory polling — not an immediate stop.** GitHub often returns `blocked` while required checks are still running or not yet reported. **Poll every 30s** until **`clean`**, **`dirty`**, or the **40-minute** budget from first post-PR poll expires (same rolling budget as `null` / `unknown` / `unstable` / `behind`). If it becomes `clean`, continue only if **`TalkBack PR Gate` → `success`** (Hard stop); then step 2. If the budget expires while still `blocked` (or any non-`clean` state except an earlier definitive `dirty`), treat as terminal BLOCK — same end state as standard mode. Report the final value. |
   | `dirty` | Merge conflict — stop. Same end state as standard mode. Report the value. |

   If `mergeable_state` has not reached a terminal outcome after **40 minutes** of polling — i.e. still stuck in `null`, `unknown`, `unstable`, `behind`, or **`blocked` that never resolves to `clean`** — end FULL_AUTO — same end state as standard mode.

   **Pre-merge guard (mandatory):** Call **`merge_pull_request` only** when **both** the **Hard stop** bullets above are satisfied ( **`TalkBack PR Gate` → `success`** and **`mergeable_state: clean`** ). Use `pull_request_read` **`get_check_runs`** (or equivalent) to confirm the **TalkBack PR Gate** conclusion on the PR head SHA. **Immediately before** calling `merge_pull_request`, perform **one more** `pull_request_read (get)`; merge **only if** `mergeable_state` is still **`clean`**. **Never** call `merge_pull_request` if the latest read was **`blocked`**, **`null`**, **`unknown`**, **`unstable`**, **`behind`**, or **field absent** — continue polling per the table until **`clean`** or a terminal failure/timeout. **Do not** merge because an **earlier** poll showed `clean` minutes ago without a fresh read.

2. **On confirmed `mergeable_state: clean` *and* `TalkBack PR Gate` `conclusion: success` (including the immediate pre-merge reads above):** Call `merge_pull_request` via GitHub MCP with `merge_method: squash`. Then:
   - **Remote branch:** GitHub auto-deletes it if "Automatically delete head branches" is enabled in repo settings. If not, delete it manually in the GitHub UI — there is no confirmed MCP tool for branch deletion.
   - **Local cleanup:**
     ```
     git checkout main
     git fetch --prune origin
     git pull --ff-only origin main
     git branch -D feat/<ticket-number>
     ```
   - **Transition Jira ticket to Done.**

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

**If agent push still fails:** Run `git push -u origin feat/<ticket>` in **Cursor’s integrated terminal** after the one-time setup above. That still completes the workflow; create the PR via **GitHub MCP** `create_pull_request` (not `gh`) as below — post-create edits via the GitHub UI if needed.

**PR description format (match SCRUM-15 / PR-quality comments):** Use clear Markdown with these sections:

1. **Plan (executed)** — numbered list of what you did in order.
2. **Summary of changes** — bullets; nest file paths or subsystems under top-level bullets when helpful.
3. **Validation** — bullets with exact commands run (and pass/fail if relevant).
4. **Acceptance criteria coverage** — bullets mapping the work to the ticket’s acceptance criteria (or goals).
5. **Refs:** ticket key(s), e.g. `SCRUM-12`, parent epic if useful.

Also cover risks, follow-ups, and Jira reference where they fit (e.g. under Summary or a short **Risks / follow-up** subsection).

**Creating PRs on GitHub.com:** Use `create_pull_request` via GitHub MCP. Put the full PR body in the create call — post-create edits may require the GitHub UI if no update tool is available in the active MCP session. Do **not** use `gh pr create`, `gh pr edit`, or `curl` when MCP can do the job.

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
- **FULL_AUTO only:** **`TalkBack PR Gate` `conclusion: success`** *and* **`mergeable_state: clean`** (after polling); if either fails — no merge, final `mergeable_state` and gate conclusion reported; if both pass — merge SHA, remote branch deletion confirmation, local cleanup confirmation, Jira Done transition confirmation

---

## 9. Planning Mode Behavior

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

## 10. Epic Execution Agent

Use the **`epic-run` skill** (`.claude/skills/epic-run/SKILL.md`) to execute all child tickets of a Jira Epic in automation. Skill index: `.claude/skills/README.md`.

```
run epic SCRUM-XX                    # start a fresh run (fails if a non-complete state file already exists)
continue epic SCRUM-XX               # resume: same automation as above for *all* remaining work
continue epic run for SCRUM-XX      # equivalent to continue epic
```

### What “epic run” is supposed to do (contract)

- **Goal:** Every child issue under the epic is **fully implemented**, opened as a PR, brought through **merge gates + Final Gate**, **squash-merged to `main`**, and moved to **Done** in Jira—**in order**, with **no manual steps** except when automation **HALT**s.
- **“Deployed” in this repo:** Automation completes when code is **on `main`** and the **TalkBack PR Gate / release-readiness** expectations for that PR are satisfied (Final Gate **PASS** for epic). Separate production deploy or release tagging is **out of scope** unless the user explicitly adds it to the epic.
- **One message should drain work:** A single **`continue epic SCRUM-XX`** must run the **full execution loop** for **every** remaining child that is not **Done** (see skill: **Drain remaining work**). Stopping after **one** merged ticket when more children remain is a **failure to follow the skill**, not an acceptable shortcut.
- **Epic vs standalone FULL_AUTO:** Outside an epic, **`implement SCRUM-XX FULL_AUTO`** (§8) may merge when **`mergeable_state: clean`**. **Inside an epic**, the agent must **not** merge until **`mergeable_state: clean`** **and** **Final Gate `PASS`** (see skill). Treat epic as **stricter**. **You** may still squash-merge manually after a WARN; **`continue epic`** then performs **reconciliation** (Jira, branches, **`main`**) without a second merge—see **Halt and resume** below.

### Parallel marker convention

By default all tickets in an epic run sequentially. A ticket opts into parallel execution by:
- carrying the Jira label `parallel-ok`, **or**
- including the line `Parallel: yes` anywhere in its Jira description

Consecutive parallel-ok tickets are batched and run concurrently. The batch must fully resolve before the next ticket starts. Do not infer parallelism — if the ticket doesn't say it, it's sequential.

### Halt and resume (WARN / BLOCK / timeouts)

The agent **HALT**s (and **never** self-resumes) when:
- FULL_AUTO does not reach `mergeable_state: clean` within its polling budget (§8, `.cursor/rules/full-auto-github-polling.mdc`)
- The unified PR gate **Final Gate** is not **`PASS`** — i.e. **`WARN`**, **`BLOCK`**, missing, or unreadable (see `pr-gate-summary.json` / **TalkBack PR Gate** check — epic runs **must not** merge in these cases)

**Manual squash merge after WARN (user override):** You may **squash-merge the PR yourself** in GitHub when you accept the WARN and want to move on. The agent will **not** perform that merge while epic policy is HALT. After you merge, invoke **`continue epic SCRUM-XX`**: the agent must **reconcile**—**Jira → Done** for that child, **update local `main`**, **delete the feature branch** (remote if still present, local `feat/<key>`), update epic state, then **continue the rest of the epic** (next tickets, same automation). See **`.claude/skills/epic-run/SKILL.md`** (**User override: manual squash merge**).

**After the user fixes** CI, branch protection, conflicts, or gate config (or after a **manual merge** as above):

1. They invoke **`continue epic SCRUM-XX`** (or equivalent)—**no** need to re-type **`FULL_AUTO`**; resume is **fully automated** for whatever is left.
2. The agent **re-queries Jira** for children with **`statusCategory != Done`** as the **source of truth** (handles manual merges or manual **Done** transitions).
3. For each remaining child **in order**: if already **Done**, skip; if **merged on GitHub** but Jira lags, **transition Jira** and **git cleanup** per skill (**do not** merge again); if a PR is **still open** and **Final Gate is still WARN/BLOCK** (gates not fixed), **HALT again immediately**—`continue epic` does not skip a WARN/BLOCK ticket to start later ones; if a PR is still open and **Final Gate is now PASS**, resume polling and merge per skill; if not started, run **`implement <KEY> FULL_AUTO`** with epic **Final Gate** rules.

**Git hygiene on resume:** Before starting the **next** `feat/<ticket>` branch, **`git fetch`** / **`git checkout main`** / **`git pull --ff-only`** so the new branch is based on current **`main`** (see skill **Sequential close-out**).

**Stale state file:** If `.epic-run/SCRUM-XX.json` exists and is not `complete`, use **`continue epic`**, not **`run epic`** again. To abandon a run, delete the state file manually (see skill).

On halt, the agent posts a Jira comment on the epic with completed tickets, the halted ticket and reason, and remaining work. The user then fixes the blocker (or merges manually) and invokes **`continue epic SCRUM-XX`**.

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
