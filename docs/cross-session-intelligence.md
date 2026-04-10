# Phase 4 — Cross-session intelligence (MCP)

This document records the **MVP design** for cross-session MCP tools under epic **SCRUM-31** (implementation: SCRUM-63, SCRUM-64, SCRUM-70, and this task **SCRUM-65**). It answers: what is indexed, where it lives, how access is enforced, and what limits apply.

## Tenant and user scope (non-negotiable)

Every cross-session tool resolves an **acting TalkBack user** (see `docs/mcp-server.md`: `TALKBACK_MCP_ACTING_USER_ID` and optional `TALKBACK_MCP_KEY_USER_MAP_JSON` in strict mode — **SCRUM-70**). No tool returns data for sessions that user would not be allowed to open in the web app.

**Session enumeration** is centralized in [`internal/database/mcp_accessible_sessions.go`](../internal/database/mcp_accessible_sessions.go):

- **Global admin:** up to **`MaxMCPAccessibleSessions` (5000)** sessions by `updated_at DESC`.
- **Everyone else:** sessions where the user is **creator** (`sessions.created_by` email match) **or** has a **`session_memberships`** row, same ordering and cap.

All cross-session queries **restrict to this ID list first** (or equivalent `WHERE id = ANY($accessible::uuid[])`), then apply search semantics. There is **no** cross-tenant dump and **no** unscoped scan of `sessions` or `session_chunks`.

## What is “indexed” for cross-session search?

### Vector (semantic) search — `search_all_sessions`

**Storage:** Reuses the existing per-session **RAG index**: table **`session_chunks`** with embedding vectors (same rows used by `search_session` and HTTP Q&A). **No separate cross-session table** is required for MVP: chunks are keyed by `session_id`; the implementation loads candidate chunks for **accessible session IDs only**, embeds the query once, and ranks globally (see [`internal/rag/retrieval.go`](../internal/rag/retrieval.go) — `CrossSessionTopKByChunks`).

**Implications:**

- Sessions **without** indexed chunks contribute **no** vector hits until the normal indexing path has run (typically via `EnsureSessionIndex` on demand for session-scoped tools; `search_all_sessions` does **not** sweep all sessions to build indexes).
- Requires **`OPENAI_API_KEY`** (or configured embedder) for query embeddings.

### Decision fields by topic — `get_decisions_by_topic`

**Storage:** Reuses **`sessions`** rows: **`premise`**, **`primary_decision`**, **`decision_outcome`**. Matching is **case-insensitive substring** (`position(lower(topic) in lower(field))`) — see [`internal/database/decisions_by_topic.go`](../internal/database/decisions_by_topic.go). **No** new index table for MVP; optional future work includes `pg_trgm` or a dedicated search service if needed.

## Limits and operational behavior

| Concern | MVP behavior |
|--------|----------------|
| Accessible sessions | Capped at **5000** per user (see `MaxMCPAccessibleSessions`). |
| `search_all_sessions` `top_k` | Default **10**, max **50** (tool input). |
| `get_decisions_by_topic` `limit` | Default **40**, max **100**. |
| Embedding rate limit | Per-session and cross-session accounting per `TALKBACK_MCP_MAX_EMBEDDING_CALLS_PER_SESSION_PER_MINUTE` (cross-session uses a fixed internal quota id — see `internal/mcpserver/cross_session_search.go`). |
| Concurrency | Single-process MCP; limits are **in-process** only (not shared across replicas). |

## Failure modes (user-visible)

- **No acting user** (`TALKBACK_MCP_ACTING_USER_ID` / map not configured when required): **403** with `error_code` `acting_user_not_configured` where applicable.
- **No accessible sessions:** empty `results` with an explanatory `note` where the tool returns one.
- **Vector path, `OPENAI_API_KEY` unset:** **503** / embedder errors per existing MCP error contract.
- **No chunk rows for a session:** that session simply produces no vector hits (not an error).

## Out of scope for this MVP (explicit)

- Dedicated external search engine (Elasticsearch, OpenSearch, etc.).
- New first-class **cross-session** tables for embeddings (duplicate of `session_chunks`).
- Real-time cross-session indexing jobs unrelated to existing session index lifecycle.
- Cross-session **summarization** or deduplication of decisions (tracked as product follow-ups).

## References

- MCP tool catalog: [`docs/mcp-server.md`](mcp-server.md)
- Accessible session helper: [`internal/database/mcp_accessible_sessions.go`](../internal/database/mcp_accessible_sessions.go)
- Cross-session vector search: [`internal/mcpserver/cross_session_search.go`](../internal/mcpserver/cross_session_search.go)
- Decision-by-topic query: [`internal/database/decisions_by_topic.go`](../internal/database/decisions_by_topic.go)
