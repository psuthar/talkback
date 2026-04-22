# Feature-Plan Skill

Policy source: `docs/agent/overview.md` and `CLAUDE.md`.
This skill defines planning output structure and execution steps, not global policy ownership.

## Purpose

Provide a **consistent workflow** for designing new TalkBack features. When invoked, Claude produces a structured plan covering architecture, schema, API, UI, testing, rollout, and MVP vs future enhancements—**without implementing code**. Useful for decision intelligence, participant simulation, invitation workflow, transcript-derived analysis, observability/agent enhancements, and similar features.

---

## When Invoked

Claude should:

1. **Analyze current architecture**  
   Use `CLAUDE.md` and the codebase to identify which parts of TalkBack are relevant (sessions, artifacts, materials, participants, RAG, auth, Zoom, R2, premise/decision concepts, etc.).

2. **Identify relevant files/modules**  
   List specific backend packages (e.g. `internal/handlers`, `internal/database`), frontend areas (e.g. `CreatorMode`, `ParticipantMode`, `MaterialsTreePanel`), and any workers or external integrations.

3. **Define the problem clearly**  
   One or two sentences on what the feature should achieve and for whom (creator, participant, system).

4. **Propose minimal schema/data model changes**  
   If the feature needs new or changed tables/columns, describe them (table name, columns, indexes, FKs). Call out whether migrations are additive only or require backfills/downtime.

5. **Propose backend/API changes**  
   New or modified endpoints (method, path, request/response shape). Note backward compatibility: new fields vs breaking changes.

6. **Propose frontend/UI changes**  
   Which screens or components change, new flows (e.g. new form, new panel), and how they call the API (fetch, refetch, WebSocket).

7. **Identify testing needs**  
   Unit tests, handler tests, integration scenarios, or manual verification steps for the new or touched code.

8. **Identify rollout or migration risks**  
   Data migration, backward compatibility, auth/security, performance, deployment order (e.g. backend before frontend, feature-flagged rollout).

9. **Separate MVP from future enhancements**  
   Clearly mark what is in scope for the first deliverable vs what can follow later (e.g. “MVP: store premise on session; future: decision outcome and timeline”).

---

## Output Format

Structure the response as:

- **Goal** (1–2 sentences)
- **Problem definition** (who, what)
- **Relevant architecture** (bullets)
- **Schema changes** (if any)
- **API changes** (if any)
- **UI changes** (if any)
- **Implementation steps** (numbered; assignable to Backend/Frontend/Architect)
- **Testing needs**
- **Rollout / migration risks**
- **MVP vs future enhancements**

Do not edit product code in this skill; only produce the plan. Implementation is done by the Backend/Frontend agents or by following the plan manually.
