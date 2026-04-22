# Skill: jira-work-decomposition

Policy source: `docs/agent/workflow-jira.md`.
This skill covers decomposition strategy only and is not execution policy.

## Purpose

Decompose a rough initiative or feature request into a well-structured, sequenced set of Jira tickets (Epics, Stories, Tasks, Bugs) before any ticket is written. Output is a ticket list ready to hand to `jira-ticket-authoring`.

## Invocation

```
decompose <initiative or feature description>
break down <initiative> into tickets
what tickets do we need for <initiative>?
```

---

## Type Selection Rules

Apply these rules in order. First match wins.

| Condition | Type |
|-----------|------|
| Work spans multiple sprints, has a shared goal, and needs child tickets | **Epic** |
| The change is directly observable by a user and delivers standalone value | **Story** |
| The work is execution-only (migration, infra, config, internal tooling, refactor) with no direct user-facing outcome | **Task** |
| Observed system behavior deviates from intended behavior | **Bug** |
| Scope is unknown and requires investigation before a ticket can be written | **Spike Task** (time-boxed, output = decision or doc) |

**Stories and Tasks are peers.** Do not make Tasks children of Stories unless there is a hard implementation dependency. A Story does not "contain" its supporting Tasks — they are separate, sequenced items.

---

## Splitting Oversized Work

A ticket is too large if any of these are true:

- Acceptance criteria exceed 6 items (Story)
- Implementation touches more than 2 independent subsystems (backend + frontend is fine; backend + frontend + migration + new service is not)
- It cannot be reviewed and merged in a single PR by one engineer in one session
- "Done" requires more than one sprint
- It mixes user-facing value with infrastructure setup

**How to split:**

1. **Separate schema/infra from behavior.** New table or migration → Task. Endpoint that uses it → Story or Task. UI that calls the endpoint → Story.
2. **Separate read from write.** List/view endpoint → one ticket. Create/update/delete → separate ticket(s).
3. **Separate happy path from error handling** only when error handling is complex enough to justify its own review.
4. **Separate backend from frontend** when they can be implemented and reviewed independently (which is almost always).
5. **Spike before Story** when the approach is unknown. The Spike's output defines the Story.

**Do not split** solely to hit a point estimate target. Split only when the resulting tickets are genuinely independent.

---

## Story vs Task Distinction

| Story | Task |
|-------|------|
| A user (any role) can do something new or differently | The system changes internally but no user workflow changes |
| "Creator can export decisions" | "Add decision_exports table and migration" |
| "Participant can filter Q&A by speaker" | "Backfill speaker_id on existing answer rows" |
| Acceptance criteria are observable in the UI or API response | Done-when criteria are verifiable by inspection, test, or query |
| Delivers product value if shipped alone | Enables a Story but has no standalone user value |

**Rule of thumb:** If you have to explain to a non-engineer why it matters, it's a Story. If the reason is "the Story can't be built without it," it's a Task.

---

## Bug vs Feature Distinction

| Bug | Feature (Story or Task) |
|-----|------------------------|
| System behavior was defined and is now incorrect | System never had this behavior |
| A regression — worked before, broken now | New capability |
| Crashes, incorrect data, security bypass, broken contract | Improved UX, new workflow, new endpoint |
| Fix is "restore the intended behavior" | Implementation requires product decisions |

**Gray area:** If the "bug" requires a product decision to fix (e.g., what should happen when X?), write it as a Story or spike first — the bug is a symptom, the feature is the fix.

---

## Sequencing and Dependencies

**Default order:**
1. Schema / migration Tasks first (no runtime dependencies)
2. Backend API Tasks / Stories (depends on schema)
3. Frontend Stories (depends on API)
4. Integration or E2E validation Tasks last

**Dependency rules:**
- Only enforce an ordering dependency when the work literally cannot be done without the prior ticket merged
- Prefer designing tickets to be independent (stub APIs, feature flags) rather than chaining everything
- Call out dependencies explicitly in each ticket's Notes section — do not leave them implicit
- Circular dependencies indicate the decomposition is wrong — re-split

**Parallel-ok criteria (for epic-run automation):**
A ticket may be marked `parallel-ok` only if:
- It shares no schema, endpoint, or file with the concurrent ticket, **and**
- Merge conflicts between the two branches are extremely unlikely, **and**
- The user explicitly approves parallel execution

Default: sequential. When in doubt, keep it sequential.

---

## Risk Guidance

Flag these when present; they require explicit decisions before the ticket list is finalized:

| Risk | What to do |
|------|-----------|
| Migration on a large or production table | Add a separate migration Task; note rollback plan |
| Breaking API contract change | Add backward-compatibility Task or split into deprecate + remove |
| Auth or access-control change | Mark as security-sensitive; criteria must include ACL test coverage |
| External service dependency (Zoom, OpenAI, etc.) | Note fallback behavior in acceptance criteria |
| Ambiguous scope | Write a Spike Task; do not write a Story until output is known |
| Cross-team dependency | Note the dependency and the blocking team in the Epic or Story |

---

## Decomposition Examples

### Example 1: "Let creators export session decisions as a CSV"

**Step 1 — Is this an Epic or a Story?**
Single user-facing feature, likely one sprint. No sub-system complexity beyond backend + frontend. → **Story**, not an Epic.

**Step 2 — What execution work is required?**
- A new endpoint is needed
- No new table required (decisions already exist in `sessions`)
- Frontend needs a download trigger

**Step 3 — Split?**
Backend endpoint and frontend button are independently reviewable. Split.

**Resulting tickets:**

| # | Type | Title | Depends on |
|---|------|-------|------------|
| 1 | Task | `GET /sessions/:id/decisions/export` — return CSV of decision fields | — |
| 2 | Story | Creator can download session decisions as CSV | Task 1 |

**Notes:** No Epic needed. If export formats expand (PDF, JSON), revisit as an Epic then.

---

### Example 2: "Build cross-session decision search for MCP agents"

**Step 1 — Is this an Epic or a Story?**
Multiple tools, new DB access patterns, new scoping logic, env var config, docs. → **Epic**.

**Step 2 — What are the independent units?**
- Scoping helper (who can see which sessions) — pure DB, no MCP dependency
- `get_decisions_by_topic` — MCP tool, depends on scoping helper
- `search_all_sessions` — MCP tool, depends on scoping helper, requires embeddings
- Memory guard for chunk load — internal cap, depends on search tool

**Step 3 — Sequence:**

| # | Type | Title | Depends on |
|---|------|-------|------------|
| 1 | Task | `ListAccessibleSessionIDsForUser` — DB scoping helper | — |
| 2 | Task | `get_decisions_by_topic` MCP tool | Task 1 |
| 3 | Task | `search_all_sessions` MCP tool | Task 1 |
| 4 | Task | Cap cross-session chunk embedding load (OOM guard) | Task 3 |
| 5 | Task | DB integration tests for cross-session scoping | Task 1 |
| 6 | Task | Behavioral handler tests for cross-session MCP tools | Tasks 2, 3 |

**Epic:** "Cross-Session Intelligence — Query decisions across meetings"  
**Non-goals (call out now):** dedicated cross-session table, real-time indexing, external search engine

---

## Decomposition Checklist

Before handing the ticket list to `jira-ticket-authoring`, verify:

- [ ] Every item has a type (Epic / Story / Task / Bug / Spike)
- [ ] No ticket mixes user-facing value with pure execution work
- [ ] Stories and Tasks are peers — Tasks are not nested under Stories
- [ ] Each ticket can be implemented in a single PR by one engineer
- [ ] All schema/migration work is a separate Task that precedes dependent behavior
- [ ] Dependencies are explicit, not implied
- [ ] No circular dependencies
- [ ] Parallel-ok tickets are explicitly identified (default is sequential)
- [ ] Risks are flagged with a decision or mitigation noted
- [ ] Spikes exist for any area of unknown scope
- [ ] Epic Non-goals are stated
- [ ] The list, if implemented in order, delivers value incrementally (no "big bang" at the end)
