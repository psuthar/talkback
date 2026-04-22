# Repo-Map Skill

Policy source: `docs/agent/overview.md`.
This skill provides mapping methodology and output format, not policy ownership.

## Purpose

Enable Claude to **generate a repo architecture map** of TalkBack consistently. When invoked, Claude produces a concise overview of the system, backend, frontend, data flow, integrations, workers, technical debt/risk areas, and likely extension points—**without editing files** unless explicitly requested.

---

## When Invoked

Claude should produce:

1. **High-level system overview**  
   What TalkBack is (session-based Q&A, decision-centric evolution), main user flows (creator vs participant), and core value (video + materials + RAG + sessions).

2. **Backend architecture**  
   Entrypoints (`cmd/api`, `cmd/obsworker`, etc.), key packages (`internal/handlers`, `internal/database`, `internal/rag`, `internal/processing`, etc.), API style (REST, WebSocket), and how sessions, materials, and Q&A are implemented.

3. **Frontend structure**  
   `web/` layout, `App.jsx` role (mode routing, session open/refetch, auth), CreatorMode vs ParticipantMode, main components (QAPanel, MaterialsTreePanel, VideoPlayer, etc.), and API usage patterns.

4. **Data flow**  
   Session load → materials/video → transcript ingestion → RAG index → Q&A; participant_ref and unread_material_ids; WebSocket updates (session_updated, answer_created, etc.).

5. **Integrations**  
   Zoom (OAuth, recording import), R2 (or local disk) for video/uploads, OpenAI (RAG, Whisper), optional Resend (invites), optional New Relic; where they plug in (handlers, processing, storage).

6. **Background workers**  
   Job processor (transcript/Whisper), processing pipeline (Zoom ingest), reconciler; where they live and what they touch.

7. **Technical debt / risk areas**  
   Known limitations, areas that are brittle or hard to extend, migration or deployment considerations (e.g. upload root under `internal/handlers/sessions/`).

8. **Likely extension points for new features**  
   Where decision intelligence, participant simulation, new providers (Loom, Teams), or observability/agent work would attach (sessions schema, handlers, frontend modes).

---

## Constraints

- **Do not edit files** unless explicitly requested. This skill is for **generating a map**, not for making changes.
- Use `CLAUDE.md` and `docs/architecture.md` as primary references; supplement with codebase search only as needed to keep the map accurate and concise.
