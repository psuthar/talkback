# Skill: jira-ticket-authoring

Policy source: `docs/agent/workflow-jira.md`.
This skill covers Jira ticket writing templates only and is not execution policy.

## Purpose

Produce consistent, concise, reviewable Jira tickets. Applies to Epics, Stories, Tasks, and Bugs. Output is paste-ready for Jira and deterministic in structure.

## Invocation

```
write jira ticket for <description>
draft epic / story / task / bug for <description>
```

---

## Type Definitions

| Type | Use when… |
|------|-----------|
| **Epic** | A coherent body of work with a shared business goal, spanning multiple Stories/Tasks. Has scope and non-goals. |
| **Story** | A user-facing change with demonstrable value deliverable in one sprint. "A user can now ___." |
| **Task** | Execution work without a direct user-facing outcome: infrastructure, config, migrations, refactors, internal tooling. Peer of Story (not child). |
| **Bug** | Observed behavior deviates from intended behavior. Includes reproduction steps. |

**Stories and Tasks are peers** — do not nest Tasks under Stories unless there is an explicit parent-child dependency.

---

## Templates

### Epic

```
Title: [Product area] — [Outcome phrase]
Example: Session Intelligence — Surface structured decisions to participants

Type: Epic

Goal
<One sentence. What business outcome does this deliver? Who benefits?>

Scope (in)
- <deliverable 1>
- <deliverable 2>

Non-goals (explicitly out)
- <excluded item 1>
- <excluded item 2>

Success criteria
- [ ] <measurable outcome 1>
- [ ] <measurable outcome 2>

Child tickets
- <SCRUM-XX> <short title>
```

---

### Story

```
Title: [Actor] can [action] [context]
Example: Creator can export session decisions as CSV

Type: Story

Context
<1–2 sentences. Why does this matter? What gap does it close?>

Acceptance criteria
- [ ] <observable behavior 1>
- [ ] <observable behavior 2>
- [ ] <edge case or error state if relevant>
- [ ] Tests written and passing locally covering the new behavior

Out of scope
- <anything adjacent but not included>

Notes
<Optional: constraints, dependencies, open questions>
```

**Testing note for Stories:** Every Story that involves code changes must include a "Tests written and passing locally" acceptance criterion. Specify the behavior being tested, not just "tests pass". UI-only Stories that cannot be verified by automated tests should say so explicitly.

---

### Task

```
Title: [Verb] [object] [qualifier]
Example: Add database index on session_chunks.session_id

Type: Task

Why
<1 sentence. What breaks, degrades, or is blocked without this?>

What
- <concrete step or deliverable 1>
- <concrete step or deliverable 2>

Done when
- [ ] <verifiable completion criterion 1>
- [ ] <verifiable completion criterion 2>
- [ ] Tests written and passing locally: <name the test type — unit / DB integration / handler / MCP behavioral>

Notes
<Optional: constraints, dependencies, risks>
```

**Testing note for Tasks:** Every Task that changes product code (Go packages, DB queries, MCP handlers, migrations, frontend) must include at least one "Tests written and passing locally" item in "Done when". Name the specific test type. Docs-only and config-only Tasks are exempt.

---

### Bug

```
Title: [Component/area]: [observed behavior] when [condition]
Example: Session search: returns 500 when query contains special characters

Type: Bug

Observed behavior
<Exactly what happens. Be specific — include error messages, status codes, or UI states.>

Expected behavior
<What should happen instead.>

Impact
<Who is affected? How often? Is there a workaround?>

Reproduction steps
1. <step 1>
2. <step 2>
3. <step 3 — observe the bug>

Environment
<Version, browser, role, data conditions if relevant>

Notes
<Optional: suspected cause, related tickets>
```

---

## Title Conventions

| Type | Pattern | Example |
|------|---------|---------|
| Epic | `[Area] — [Outcome]` | `Cross-Session Intelligence — Query decisions across meetings` |
| Story | `[Actor] can [action]` | `Participant can filter Q&A by speaker` |
| Task | `[Verb] [object]` | `Migrate session_chunks to partitioned table` |
| Bug | `[Area]: [behavior] when [condition]` | `RAG answer: truncates at 500 chars when source has tables` |

**Avoid:** vague nouns ("improvements", "updates", "work"), passive voice, solution-first titles ("Use Redis for…"), version numbers in Epic titles.

---

## Acceptance Criteria Rules

- Write in present tense, observable form: "User sees…", "API returns…", "System stores…"
- Each criterion tests exactly one behavior
- Include at least one error/edge state for Stories that touch user input or external calls
- Do not describe implementation: "uses a JOIN" is not a criterion; "returns results within 500ms" is
- Maximum 6 criteria per Story; split the Story if you need more
- Criteria must be verifiable without reading the code

---

## Section Ordering (mandatory)

**Epic:** Goal → Scope → Non-goals → Success criteria → Child tickets  
**Story:** Context → Acceptance criteria → Out of scope → Notes  
**Task:** Why → What → Done when → Notes  
**Bug:** Observed → Expected → Impact → Reproduction → Environment → Notes

Do not reorder sections. Do not merge sections.

---

## Separation of Business Outcome and Execution

- **Epic / Story:** lead with the outcome for the user or business. Do not open with implementation detail.
- **Task:** lead with why (business/technical reason), then what (execution steps).
- **Bug:** lead with what is broken (observable), not with a hypothesis about the cause.
- If you find yourself writing "we will use X library / run Y query" in the Goal or Context section, move it to Notes.

---

## Anti-Patterns and Vague Language to Avoid

| Avoid | Replace with |
|-------|-------------|
| "Improve performance" | "API response time for search drops below 300ms at p95" |
| "Handle edge cases" | Enumerate the specific edge cases as criteria |
| "Refactor X" as a Story | Use a Task; Stories require user-visible value |
| "Look into…" | Break into a spike Task with a defined output |
| "As a user I want…" (Given/When/Then) | Plain prose context + observable criteria |
| Passive voice in criteria ("should be shown") | Active: "user sees…", "system returns…" |
| Acceptance criteria that describe internal state | Only observable outcomes |
| Multi-clause criteria ("and also…") | Split into two criteria |
| "etc.", "and more", "various" | Be exhaustive or call out explicitly what is excluded |

---

## Examples

### Epic — Strong

```
Title: Cross-Session Intelligence — Query decisions across meetings

Type: Epic

Goal
Enable users to search and retrieve structured decisions from all accessible sessions, so institutional knowledge is queryable rather than buried in individual recordings.

Scope (in)
- Semantic search across all session chunks (search_all_sessions MCP tool)
- Keyword/topic search over decision fields (get_decisions_by_topic MCP tool)
- Per-user session access scoping (creator + membership rules)
- Memory guard for large cross-session chunk loads

Non-goals
- Dedicated cross-session index tables (reuse session_chunks)
- Real-time indexing jobs
- Cross-session summarization or deduplication
- External search engine integration

Success criteria
- [ ] search_all_sessions returns semantically relevant chunks from any accessible session
- [ ] get_decisions_by_topic returns matching sessions scoped to the acting user
- [ ] No user can retrieve sessions they cannot access in the web app
- [ ] Chunk load is capped to prevent unbounded memory use

Child tickets
- SCRUM-63 Accessible session enumeration helper
- SCRUM-64 get_decisions_by_topic MCP tool
- SCRUM-65 search_all_sessions MCP tool
```

---

### Epic — Strong (simpler scope)

```
Title: Participant Invite Flow — Email-based session access

Type: Epic

Goal
Allow creators to invite participants by email so sessions are not limited to users who already have accounts.

Scope (in)
- Invitation creation and email delivery
- Token-based accept flow (no account required to accept)
- Invitation status tracking (pending / accepted / expired)
- Creator UI to manage invites

Non-goals
- Bulk CSV invite import
- SSO or OAuth-based invite acceptance
- Invitation analytics

Success criteria
- [ ] Creator can invite an email address not yet in the system
- [ ] Recipient receives an email with a working accept link
- [ ] Accepted invite creates a session_membership row
- [ ] Expired tokens return a clear error, not a 500

Child tickets
- SCRUM-10 Invitations DB schema and migration
- SCRUM-11 POST /sessions/:id/invitations endpoint
- SCRUM-12 Invitation email delivery
- SCRUM-13 Accept-invite flow (token validation + membership creation)
- SCRUM-14 Creator invite management UI
```

---

### Story — Strong

```
Title: Creator can view all pending invitations for a session

Type: Story

Context
Creators currently have no visibility into who has been invited but not yet accepted. They need to see pending invitations to follow up or resend.

Acceptance criteria
- [ ] Creator sees a list of pending invitations on the session management page
- [ ] Each row shows invitee email, invite date, and status (pending / accepted / expired)
- [ ] Creator can resend an invitation from the list
- [ ] Expired invitations are visually distinct from pending ones
- [ ] Non-creator participants cannot see the invitation list

Out of scope
- Revoking invitations
- Bulk resend

Notes
- Depends on SCRUM-11 (invitations endpoint) being merged first
```

---

### Story — Strong

```
Title: Participant can mark a material as read

Type: Story

Context
Participants have no way to track which session materials they have reviewed. A read marker lets them resume where they left off and gives creators visibility into engagement.

Acceptance criteria
- [ ] Participant can toggle a "mark as read" state on any material in a session
- [ ] Read state persists across page reloads and sessions
- [ ] Creator can see how many participants have marked each material as read
- [ ] Toggling off removes the read state

Out of scope
- Per-page read progress within a document
- Aggregated engagement analytics

Notes
- Read state stored in a new materials_read table (separate Task)
```

---

### Task — Strong

```
Title: Add materials_read table and migration

Type: Task

Why
The "mark material as read" Story (SCRUM-XX) requires a new table to persist per-user read state. No schema exists today.

What
- Create migration: materials_read(id, session_id, material_id, user_id, read_at)
- Add indexes on (session_id, user_id) and (material_id, user_id)
- Wire into golang-migrate on API startup

Done when
- [ ] Migration runs cleanly on a fresh database
- [ ] Migration runs cleanly against the current production schema via go test or manual check
- [ ] Index names follow repo conventions

Notes
- Migration must be backward-compatible (no NOT NULL columns without defaults)
```

---

### Task — Strong

```
Title: Cap cross-session chunk embedding load to prevent OOM

Type: Task

Why
search_all_sessions loads all chunk embeddings (~6 KB each) for up to 5000 sessions into memory. On large tenants this can exhaust process memory.

What
- Add MaxCrossSessionChunks constant (default 50 000) to internal/rag
- Apply LIMIT in ListChunksWithEmbeddingsBySessionIDs query
- Add TALKBACK_MCP_MAX_CROSS_SESSION_CHUNKS env var override
- Document in docs/mcp-server.md

Done when
- [ ] Query is capped at MaxCrossSessionChunks rows
- [ ] Env var override is respected at startup
- [ ] Unit test covers the cap boundary
- [ ] Docs updated

Notes
- Cap applies before cosine ranking; larger corpora are truncated, not sampled
```

---

### Bug — Strong

```
Title: Session search: returns 500 when query contains special characters

Type: Bug

Observed behavior
POST /sessions/:id/search returns HTTP 500 with no body when the query string contains characters like %, &, or <. Reproducible in production and locally.

Expected behavior
Search should return results normally, or return a 400 with a descriptive error if the query is invalid. A 500 with no body is never correct.

Impact
Any participant using special characters in a search query sees a blank error. Workaround: rephrase query without special characters. Estimated frequency: low but reported by 2 users this week.

Reproduction steps
1. Open any session with indexed content
2. Submit a search query containing %20 or &
3. Observe HTTP 500 response with empty body in network tab

Environment
Production and local dev. Affects all session roles. Introduced in or before v0.9.2.

Notes
Likely unescaped string passed directly into SQL or embedding call. Check internal/handlers/search.go.
```

---

### Bug — Strong

```
Title: Invite accept: token marked accepted before membership row is created

Type: Bug

Observed behavior
When two requests hit /invitations/accept/:token within ~200ms of each other, both return 200 but only one session_membership row is created. The second request returns success despite doing nothing.

Expected behavior
The second request should return 409 (already accepted) or the operation should be idempotent with a single membership row guaranteed.

Impact
Rare under normal use, but automated retries (e.g. mobile app tapping twice) can produce inconsistent state. Users report "joined but can't see session" in 3 support tickets.

Reproduction steps
1. Generate a valid invite token
2. Fire two concurrent POST /invitations/accept/:token requests using curl --parallel
3. Observe: both return 200; query session_memberships — only one row exists

Environment
Local dev with docker-compose Postgres. Likely present in production.

Notes
Fix should use a database-level unique constraint + upsert or a SELECT FOR UPDATE to serialize. See internal/invitations/accept.go.
```

---

## Pre-Output Checklist

Before finalizing any ticket, verify:

- [ ] Type is correct (Epic / Story / Task / Bug — not mixed)
- [ ] Title follows the convention for the type
- [ ] All required sections are present and in the correct order
- [ ] Context / Goal leads with outcome, not implementation
- [ ] Acceptance criteria are observable and implementation-free
- [ ] No vague language ("improve", "handle", "various", "etc.")
- [ ] Out of scope / Non-goals are explicitly stated
- [ ] Notes section used for constraints, dependencies, and open questions — not mixed into criteria
- [ ] Criteria count ≤ 6 for Stories (split if more needed)
- [ ] Bug has all four core sections: Observed / Expected / Impact / Reproduction
- [ ] **Code-change tickets (Task/Story/Bug) include an explicit testing criterion** — names the test type (unit, DB integration, handler, MCP behavioral) and confirms tests must pass locally before the ticket is done. Docs/config-only tickets are exempt; mark them as such in Notes.
