# TalkBack MCP server (`talkback-mcp`)

Stdio [Model Context Protocol](https://modelcontextprotocol.io) server for agents (Cursor, Claude Code, Claude Desktop). **Stdout** carries the MCP JSON-RPC stream; **stderr** is used for operational logs (structured lines; see [Structured logging](#structured-logging-scrum-40) below).

**Protocol wiring:** The binary uses `internal/mcpserver.NewTalkbackMCPServer`, which constructs the official Go SDK server, attaches receiving middleware so **only** `tools/call` is API-key gated (`initialize` / `tools/list` stay open), registers tools, then runs [`mcp.StdioTransport`](https://pkg.go.dev/github.com/modelcontextprotocol/go-sdk/mcp#StdioTransport) (newline-delimited JSON-RPC).

**Phase 4 — cross-session tools (`search_all_sessions`, `get_decisions_by_topic`, …):** scope, reuse of `session_chunks` vs `sessions` fields, caps, and failure modes are documented in **[`cross-session-intelligence.md`](cross-session-intelligence.md)** (SCRUM-65). Identity for scoped queries is **SCRUM-70** (`TALKBACK_MCP_ACTING_USER_ID` / optional per-key map).

## Quick start from a clone (Cursor + Claude Code)

Goal: run `talkback-mcp` from a **local clone** with minimal friction—`health_check` first; optional Postgres-backed `get_session_metadata` when you need it.

1. **Install Go** using a toolchain that matches the **`go` version** in the repo root [`go.mod`](../go.mod) (the project tracks a current Go release).
2. **Build** (from the repository root):

   ```bash
   go build -o talkback-mcp ./cmd/talkback-mcp
   ```

   Or use `go run ./cmd/talkback-mcp` via the generated config (below); no separate build step is required.
3. **Generate IDE config** (writes **gitignored** `.cursor/mcp.json` and `.mcp.json`):

   ```bash
   ./scripts/setup-mcp-config.sh
   ```

   The script uses **`go run`** with an **absolute** path to `cmd/talkback-mcp`, so the MCP subprocess does not depend on shell `cwd`. It sets `TALKBACK_MCP_REQUIRE_CLIENT_KEY=false` and a random `TALKBACK_MCP_API_KEY` unless you preset **`TALKBACK_MCP_API_KEY`** in the environment before running the script.
4. **Optional — session metadata tool:** If you already have a TalkBack database (e.g. local Docker Postgres from the main app), export **`DATABASE_URL`** and **`TALKBACK_MCP_ACTING_USER_ID`** (a TalkBack `users.id` UUID) in your shell and **run the setup script again**—those variables are copied into the MCP `env` block. For **per-API-key user binding** (SCRUM-70), export **`TALKBACK_MCP_KEY_USER_MAP_JSON`** as well; the setup script copies it into `env` when set. Otherwise omit them; only `health_check` is registered when `DATABASE_URL` is unset at process start.
5. **Restart** Cursor and/or Claude Code fully so MCP reloads (config is not hot-reloaded).

**PATH and platforms**

- **macOS + GUI IDEs:** If `go` is installed via Homebrew (`/opt/homebrew/bin/go`) but the IDE cannot find `go`, ensure that directory is on `PATH` for GUI-launched apps (e.g. Cursor/VS Code `terminal.integrated.env.osx` or launch Cursor from a terminal). The repo includes `.vscode/settings.json` that prepends `/opt/homebrew/bin` for the **integrated terminal** only.
- **Linux:** Install Go from your distro or [go.dev](https://go.dev/dl/); confirm `go version` in the environment that starts the IDE.
- **Windows:** Prefer **WSL** or **Git Bash** with Go installed; use **forward slashes or escaped backslashes** in JSON `args` paths if you hand-edit configs.

**Committed examples (placeholders only, no real secrets)**

- Minimal: [`mcp-config.example.json`](mcp-config.example.json)
- With optional DB + acting user (placeholders): [`mcp-config.example-with-db.json`](mcp-config.example-with-db.json)

## Build

```bash
go build -o talkback-mcp ./cmd/talkback-mcp
```

## Authentication

The server always requires **`TALKBACK_MCP_API_KEY`** (non-empty) at startup.

| Variable | Required | Purpose |
|----------|----------|---------|
| `TALKBACK_MCP_API_KEY` | Yes | One or more comma-separated shared secrets the server knows (rotation). |
| `TALKBACK_MCP_REQUIRE_CLIENT_KEY` | No | Default **true** (strict): each `tools/call` must include a key matching `TALKBACK_MCP_API_KEY` (see below). Set to **`false`** so Cursor / Claude Code can use tools **without** per-call metadata (typical local dev). |
| `TALKBACK_MCP_ACTING_USER_ID` | No | TalkBack **users.id** UUID for the acting user. Session tools use this identity for ACL (same rules as the web app) when no per-key mapping applies. If it is **unset** while **`DATABASE_URL`** is set **and** you do not use **`TALKBACK_MCP_KEY_USER_MAP_JSON`** in strict key mode, tools return **403** with **`error_code`** `acting_user_not_configured`. The server logs a **stderr warning** at startup when **`DATABASE_URL`** is set but neither a global acting user nor a usable per-key map is configured. |
| `TALKBACK_MCP_KEY_USER_MAP_JSON` | No | **SCRUM-70 / Phase 4:** Optional JSON object mapping each **exact** API key string (must appear in **`TALKBACK_MCP_API_KEY`**) to a TalkBack **`users.id`** UUID, e.g. `{"sk-alice":"550e8400-e29b-41d4-a716-446655440000"}`. Used only when **`TALKBACK_MCP_REQUIRE_CLIENT_KEY=true`**: each `tools/call` resolves the acting user from the client key. Keys not listed in the map still authenticate but fall back to **`TALKBACK_MCP_ACTING_USER_ID`** when set. Ignored in IDE mode (`REQUIRE_CLIENT_KEY=false`). |
| `DATABASE_URL` | No | When set at process start, the server registers **`get_session_metadata`**, **`get_session_decisions`** and **`get_decisions`** (same behavior — SCRUM-55 / SCRUM-60), **`get_decisions_by_topic`** (SCRUM-64 — substring match on structured decision fields across accessible sessions), **`get_session_action_items`** and **`get_action_items`** (same behavior — SCRUM-56 / SCRUM-61), **`search_session`**, **`search_session_content`** (same behavior as `search_session`), **`search_all_sessions`** (cross-session — SCRUM-63), **`get_session_raw_chunks`**, **`get_session_retrieval_context`** (same behavior as `get_session_raw_chunks`), **`get_session_source_chunks`**, **`ask_session`**, and **`ask_session_question`** (same behavior as `ask_session`) (Postgres). When unset, only `health_check` is available. |
| `TALKBACK_MCP_MAX_EMBEDDING_CALLS_PER_SESSION_PER_MINUTE` | No | **SCRUM-54:** Max **query-embedding** calls per TalkBack session per sliding minute (in-process only). Applies to **`search_session`**, **`search_all_sessions`** (accounted against a fixed internal id for cross-session calls), **`get_session_raw_chunks`**, **`get_session_action_items`** / **`get_action_items`**, and **`ask_session`** query embeddings — **not** to bulk index builds inside `EnsureSessionIndex`. Default **`0`** = unlimited (backward compatible). Set e.g. **`60`** or **`120`** to cap misbehaving agents. |

**Local dev presets**

| Goal | Set these (minimum) |
|------|---------------------|
| `health_check` only | `TALKBACK_MCP_API_KEY` (non-empty), `TALKBACK_MCP_REQUIRE_CLIENT_KEY=false` for typical Cursor/Claude Code |
| `get_session_metadata` / `get_decisions` / `get_decisions_by_topic` / `get_session_action_items` / `search_session` / `search_session_content` / `search_all_sessions` / `get_session_raw_chunks` / … | Above plus `DATABASE_URL` and identity for session ACL: typically **`TALKBACK_MCP_ACTING_USER_ID`**, or **`TALKBACK_MCP_KEY_USER_MAP_JSON`** with **`TALKBACK_MCP_REQUIRE_CLIENT_KEY=true`** (per listed key), with **`TALKBACK_MCP_ACTING_USER_ID`** as fallback for API keys not in the map. **`get_session_decisions`**, **`get_decisions`**, and **`get_decisions_by_topic`** need only DB (no OpenAI). **`get_session_action_items`** / **`get_action_items`** need **`OPENAI_API_KEY`** (one embedding + one LLM call per invocation; not persisted). **`search_session`** (or **`search_session_content`**), **`search_all_sessions`**, **`get_session_raw_chunks`** (or **`get_session_retrieval_context`**), **`get_session_source_chunks`** (when indexing), and **`ask_session`** (or **`ask_session_question`**) need **`OPENAI_API_KEY`** (embeddings; Q&A also uses the LLM) — same embedding stack as web session Q&A. |

### Embedding rate limits and upstream errors (SCRUM-54)

**Per-session query embedding cap:** When **`TALKBACK_MCP_MAX_EMBEDDING_CALLS_PER_SESSION_PER_MINUTE`** is **&gt; 0**, each session UUID gets a sliding **one-minute** window. Exceeding the cap returns **`http_status` 429** and **`error_code`** `session_embedding_rate_limit`. Limits are enforced **in the MCP process only** (not shared across replicas).

**Tool error JSON:** On failure, tools may return a JSON string with **`error`**, **`http_status`**, and optionally **`error_code`** (stable machine identifier). Examples:

| `error_code` | Typical `http_status` | Meaning |
|----------------|------------------------|---------|
| `session_embedding_rate_limit` | 429 | Too many query embeddings for this session in one minute. |
| `openai_not_configured` | 503 | **`OPENAI_API_KEY`** unset. |
| `openai_auth_failed` | 503 | API key rejected by the embedding provider. |
| `openai_rate_limited` | 429 | Provider rate limit (upstream). |
| `openai_quota_exceeded` | 429 | Account quota / billing (upstream). |
| `embedding_failed` | 503 | Other embedding failure (details logged server-side, not echoed). |
| `embedding_empty_result` | 503 | No vector returned. |
| `index_unavailable` / `index_timeout` | 503 | **`EnsureSessionIndex`** failed or timed out. |
| `retrieval_failed` | 500 | DB / vector retrieval error after a successful embedding. |
| `acting_user_not_configured` | 403 | **`TALKBACK_MCP_ACTING_USER_ID`** unset or invalid at runtime; distinct from real ACL denial. |
| `session_access_denied` | 403 | Acting user is valid but may not read this session (no membership / not creator / not admin). |

**Logging:** Rate limits and embedding failures log **`mcp event=embedding_rate_limit`** or **`mcp event=embedding_error`** / **`mcp event=index_error`** on stderr with **`error_code`** and **`session_id`** (canonical UUID) where applicable — **no** query text or API key material.

### Structured intelligence fallbacks (SCRUM-62)

**`get_session_decisions`**, **`get_decisions`**, **`get_session_action_items`**, and **`get_action_items`** share documented behavior for **empty or partial success payloads** (e.g. omitted premise fields, **`low_signal`** action-item responses) versus **structured tool errors** (**`http_status`**, **`error_code`**). See [`mcp-structured-intelligence-fallbacks.md`](mcp-structured-intelligence-fallbacks.md).

### API key middleware

Implementation: `internal/mcpserver/middleware.go` — `Auth.RequireToolAuthMiddleware`. Only the JSON-RPC method **`tools/call`** is gated. **`initialize`**, **`tools/list`**, **`ping`**, and other non-tool methods pass through without a client key so the MCP handshake and tool discovery work. Keys are compared in constant time (per equal-length candidate); auth failures emit a structured stderr line (`event=auth_failed`, sanitized tool name, `reason=missing_or_invalid_key`) and **never** log key material or `_meta`.

### Strict mode (`TALKBACK_MCP_REQUIRE_CLIENT_KEY` unset or true)

Pass the key on every **`tools/call`** via `_meta` (or HTTP headers when the transport fills `RequestExtra`):

```json
{
  "name": "health_check",
  "arguments": {},
  "_meta": {
    "talkback": { "apiKey": "same-secret-as-TALKBACK_MCP_API_KEY" }
  }
}
```

Alternatives: `_meta.talkbackApiKey`, `_meta.authorization` as `Bearer <token>`, or `Authorization` / `X-API-Key` headers on HTTP-based transports.

### IDE mode (`TALKBACK_MCP_REQUIRE_CLIENT_KEY=false`)

Many IDE hosts do not attach `_meta` on tool calls. Set **`TALKBACK_MCP_REQUIRE_CLIENT_KEY=false`** in the MCP server `env` block (see [One-shot setup](#one-shot-setup-cursor--claude-code)). The process still loads `TALKBACK_MCP_API_KEY` so only your configured subprocess can run; treat this as **local-trust** (weaker than per-call keys).

Invalid keys in strict mode produce an **unauthorized** error for `tools/call` (no secrets in logs).

## Run (local)

```bash
export TALKBACK_MCP_API_KEY=dev-shared-secret
export TALKBACK_MCP_REQUIRE_CLIENT_KEY=false
./talkback-mcp
# optional:
./talkback-mcp -version=1.0.0
```

## One-shot setup (Cursor + Claude Code)

From the **repository root**:

```bash
./scripts/setup-mcp-config.sh
```

This writes **both** (gitignored, not committed):

- **`.cursor/mcp.json`** — Cursor (project or picked up when you open this repo)
- **`.mcp.json`** — Claude Code **project**-scoped MCP

It sets `TALKBACK_MCP_REQUIRE_CLIENT_KEY=false` and generates a random API key unless you preset **`TALKBACK_MCP_API_KEY`**.

If **`DATABASE_URL`** and/or **`TALKBACK_MCP_ACTING_USER_ID`** are set in the shell when you run the script, they are copied into the generated `env` object so `get_session_metadata` can run against your local DB.

Then **fully quit and reopen** Cursor and/or Claude Code so MCP reloads.

Committed references (edit paths if you copy manually): [`docs/mcp-config.example.json`](mcp-config.example.json), [`docs/mcp-config.example-with-db.json`](mcp-config.example-with-db.json).

## Cursor

**Option A — script:** run `./scripts/setup-mcp-config.sh` (see above).

**Option B — manual:** merge this shape into **`~/.cursor/mcp.json`** (global) or **`.cursor/mcp.json`** (project). Same JSON as Claude Code’s `mcpServers` block:

```json
{
  "mcpServers": {
    "talkback": {
      "command": "go",
      "args": ["run", "/absolute/path/to/talkback/cmd/talkback-mcp", "-version=dev"],
      "env": {
        "TALKBACK_MCP_API_KEY": "your-shared-secret",
        "TALKBACK_MCP_REQUIRE_CLIENT_KEY": "false"
      }
    }
  }
}
```

Use a **built binary** by setting `"command"` to the absolute path of `talkback-mcp` and `"args": ["-version=dev"]`, or keep the `go run` form above so you do not need `cwd`.

## Claude Code

**Option A — script:** `./scripts/setup-mcp-config.sh` creates **`.mcp.json`** at the repo root (project scope). Approve the server when prompted (`claude mcp reset-project-choices` if you need to re-approve).

**Option B — user scope:** add the same `talkback` entry under `mcpServers` in **`~/.claude.json`** (see [Claude Code MCP docs](https://code.claude.com/docs/en/mcp)).

**Secrets in git:** prefer `${TALKBACK_MCP_API_KEY}` style expansion in committed templates if your Claude Code version supports it, or keep `.mcp.json` gitignored and generate it with the script.

## Claude Desktop

Merge the same `mcpServers.talkback` object into:

- **macOS:** `~/Library/Application Support/Claude/claude_desktop_config.json`
- **Windows:** `%APPDATA%\Claude\claude_desktop_config.json`

Restart Claude Desktop after saving.

## Structured logging (SCRUM-40)

The server writes **single-line** messages to stderr via Go’s standard `log` package. Each line starts with `mcp` and uses space-separated `key=value` fields for grep-friendly parsing:

| Field | Meaning |
|-------|--------|
| `event` | `tool_complete` after a tool handler returns, or `auth_failed` when strict API-key validation fails on `tools/call`. |
| `tool` | Sanitized tool name (`a-z`, `0-9`, `_` only; max 64 chars); invalid names appear as `unknown_tool`. |
| `duration_ms` | Wall time for the tool handler (always present; `0` for `auth_failed`). |
| `session_id` | Present on `get_session_metadata` **only** after the input UUID parses successfully (canonical string). Invalid `session_id` arguments are **not** echoed. |
| `reason` | On `auth_failed`, fixed `missing_or_invalid_key`. |

**Not logged:** API keys, bearer tokens, full `_meta`, request bodies, or arbitrary client-supplied fields (only the allowlisted keys above are emitted).

Implementation: `internal/mcpserver/mcp_log.go` (`logMCPToolComplete`, `logMCPAuthFailed`).

## Tools (SCRUM-32+)

| Tool | Description |
|------|-------------|
| `health_check` | Returns JSON object: `status` (`ok`; `degraded` reserved), `service` (always `talkback-mcp`), `version` (process version, default `dev`). No secrets or session data. Implemented in `internal/mcpserver/health.go`. |
| `get_session_metadata` | Input: `session_id` (UUID). Output: `title`, `created_at`, `owner` (`created_by`, optional `display_name`). Requires `DATABASE_URL` and `TALKBACK_MCP_ACTING_USER_ID`. Errors mirror HTTP semantics in JSON (`http_status` 400/403/404). Implemented in `internal/mcpserver/session_metadata.go` (SCRUM-39). |
| `get_session_decisions` | Input: `session_id` (UUID). Output: **`schema_version`** (`1`), **`premise`**, **`primary_decision`**, **`decision_outcome`** (nullable strings from DB), **`decision_stances`** (array of persisted stances: `id`, `session_id`, `user_id`, `stance`, optional `rationale`, `user_email`, `created_at`, `updated_at`). **v1: persisted fields only** — no LLM extraction (SCRUM-55). **Contract:** [`mcp-session-decisions-schema.md`](mcp-session-decisions-schema.md), JSON Schema [`schemas/mcp-session-decisions-v1.schema.json`](schemas/mcp-session-decisions-v1.schema.json) (SCRUM-57). **Fallbacks:** [`mcp-structured-intelligence-fallbacks.md`](mcp-structured-intelligence-fallbacks.md) (SCRUM-62). Same session ACL as `get_session_metadata`. Implemented in `internal/mcpserver/session_decisions.go`. |
| `get_decisions` | **Alias** for **`get_session_decisions`** (SCRUM-60). Same input, output, and ACL. |
| `get_decisions_by_topic` | Input: **`topic`** (required), optional **`limit`** (default 40, max 100). Output: **`schema_version`**, **`topic`**, **`results`** — each hit has **`session_id`**, **`session_title`**, **`session_updated_at`**, nullable **`premise`** / **`primary_decision`** / **`decision_outcome`**, **`decision_stance_count`**. Matches the topic as a **case-insensitive substring** against those three fields. Scoped to sessions the acting user may read (same cap/rules as **`search_all_sessions`** — SCRUM-63/70). **SCRUM-64** — no deduplication across sessions. Implemented in `internal/mcpserver/decisions_by_topic.go`. |
| `get_session_action_items` | Input: `session_id` (UUID). Output: **`schema_version`** (`1`), **`action_items`** (`description`, optional **`owner`**), **`low_signal`**, optional **`note`**, optional **`llm_model`**. Ephemeral: one retrieval embedding + one LLM call per invocation; **not** stored in Postgres (SCRUM-56). Requires **`OPENAI_API_KEY`**. Same session ACL as `get_session_metadata`. **Contract:** [`mcp-session-action-items-schema.md`](mcp-session-action-items-schema.md), JSON Schema [`schemas/mcp-session-action-items-v1.schema.json`](schemas/mcp-session-action-items-v1.schema.json) (SCRUM-58). **Fallbacks:** [`mcp-structured-intelligence-fallbacks.md`](mcp-structured-intelligence-fallbacks.md) (SCRUM-62). Implemented in `internal/mcpserver/session_action_items.go`. |
| `get_action_items` | **Alias** for **`get_session_action_items`** (SCRUM-61). Same input, output, and ACL. |
| `search_session` | **Preferred** name for deterministic session search (SCRUM-48). Same inputs/outputs as `search_session_content` below — use either tool; behavior is identical. Implemented in `internal/mcpserver/session_search.go`. |
| `search_session_content` | **Legacy alias** for `search_session`. Input: `session_id`, `query`, optional `top_k` (default 10, max 50). Output: ranked chunks with **deterministic** cosine similarity (and primary-transcript boost per [`internal/rag`](../internal/rag)), **snippet**, **source_type**, **source_id**, **anchor**, **start_ms** / **end_ms** when present in anchors. No LLM ranking or summarization (SCRUM-43). Requires `DATABASE_URL`, `TALKBACK_MCP_ACTING_USER_ID`, and **`OPENAI_API_KEY`** for query embedding. Same session ACL as `get_session_metadata`. |
| `search_all_sessions` | Input: `query`, optional `top_k` (default 10, max 50). Output: **`schema_version`**, globally ranked hits across **all sessions the acting user may read** (creator, membership, or global admin within a cap — see [`internal/database/mcp_accessible_sessions.go`](../internal/database/mcp_accessible_sessions.go)), each with **`session_id`**, **`session_title`**, **`session_updated_at`**, **`similarity`**, **`snippet`**, source fields. One query embedding per call; uses indexed chunks only (no `EnsureSessionIndex` sweep). **SCRUM-63** — auth-scoped, not a global dump. Implemented in `internal/mcpserver/cross_session_search.go`. Requires **`OPENAI_API_KEY`**. |
| `get_session_raw_chunks` | **Preferred** name for raw ranked chunks (SCRUM-52). Same inputs/outputs as `get_session_retrieval_context` — use either tool; behavior is identical. Implemented in [`internal/mcpserver/session_retrieval_context.go`](../internal/mcpserver/session_retrieval_context.go). |
| `get_session_retrieval_context` | **Legacy alias** for `get_session_raw_chunks`. Input: `session_id`, `query`, optional `top_k` (default 10, max 50). Output: **`retrieval_context`** with **metadata** (embedding model, primary video id, score-boost flag) and **chunks** ranked by score with **chunk_id**, **chunk_idx**, **text** (truncated window), **content_hash**, **anchor**, timestamps. Same [`rag.RetrieveTopKWithScores`](../internal/rag/retrieval.go) stack as `search_session` / `search_session_content`; **different payload** for agent-side reasoning. **No LLM synthesis** (SCRUM-45). Requires `DATABASE_URL`, `TALKBACK_MCP_ACTING_USER_ID`, and **`OPENAI_API_KEY`**. Chunk row **UUIDs** may change if chunks are rebuilt during re-index; treat **`content_hash`** as the stable content fingerprint when deduplicating. |
| `get_session_source_chunks` | Input: `session_id`, **`source_type`** (`transcript` \| `material` \| `link`), optional **`source_id`** (UUID), optional **`limit`** (default 500, max 2000). Calls [`rag.EnsureSessionIndex`](../internal/rag/index.go) then lists rows from **`session_chunks`** (same index as [`rag.RetrieveTopK`](../internal/rag/retrieval.go)). **No query embedding for ranking** — read by source. **`OPENAI_API_KEY`** needed when the index must be built. (SCRUM-46). |
| `ask_session` | **Preferred** name for guarded session Q&A (SCRUM-50). Same inputs/outputs as `ask_session_question` — use either tool; behavior is identical. Implemented in `internal/mcpserver/session_ask_question.go`. |
| `ask_session_question` | **Legacy alias** for `ask_session`. Input: `session_id`, `question`. Output: **answer_text**, **answer_status** (`answered` \| `not_covered` \| `error`), **confidence**, **automation_recommended** (SCRUM-51), **citations** (see [Citations](#citations-scrum-53) — same fields and **navigation** resolution as HTTP SessionAsk), **question_id** / **answer_id** after persistence. Uses the same RAG pipeline and [`internal/utils`](../internal/utils/qa.go) guardrails as `POST /api/sessions/:id/ask` (SCRUM-44). Requires `DATABASE_URL`, `TALKBACK_MCP_ACTING_USER_ID`, and **`OPENAI_API_KEY`**. Enforces per-session question limits; returns **429** when the limit is reached. |

### Session metadata / DB (SCRUM-39)

When **`DATABASE_URL`** is set at process start, the server opens Postgres through [`internal/database`](../internal/database) and registers **`get_session_metadata`**, **`get_session_decisions`**, **`get_decisions`**, **`get_decisions_by_topic`**, **`get_session_action_items`**, and **`get_action_items`**. The acting user is the TalkBack user UUID from **`TALKBACK_MCP_ACTING_USER_ID`** (wired into the request context after API-key middleware on `tools/call`). Access is allowed for **global admins** or users who pass **`UserCanAccessSession`** for that session — same rules as the web app. Metadata tools do **not** return full transcript or material bodies; **`get_session_action_items`** uses indexed chunks internally for extraction only.

### Session search (SCRUM-43, SCRUM-48)

**`search_session`** and **`search_session_content`** are the same handler: [`rag.RetrieveTopKWithScores`](../internal/rag/retrieval.go) after embedding the query with the same embedder as web Q&A. If the session has no indexed chunks yet, [`rag.EnsureSessionIndex`](../internal/rag/index.go) runs (storage may be nil; R2-backed PDFs rely on extracted text when available). Results are ordered by score only — deterministic, no LLM.

**Example** (`tools/call` arguments — use `search_session` or `search_session_content` interchangeably):

```json
{
  "session_id": "00000000-0000-4000-8000-000000000001",
  "query": "quarterly revenue",
  "top_k": 10
}
```

**Example result shape** (abridged): `{ "session_id": "...", "query": "...", "results": [ { "rank": 1, "similarity": 0.82, "snippet": "...", "source_type": "transcript", "source_id": "...", "anchor": { }, "start_ms": 12000, "end_ms": 15000 } ] }`.

### Deterministic ranking and top-k (SCRUM-47)

Session-scoped **vector** tools (**`search_session`** / **`search_session_content`**, **`get_session_raw_chunks`** / **`get_session_retrieval_context`**) and HTTP/MCP **ask** paths share the same retrieval core: [`rag.RetrieveTopKWithScores`](../internal/rag/retrieval.go) (or [`rag.RetrieveTopK`](../internal/rag/retrieval.go), which delegates to it). This section is the **integrator-facing contract**; the Go implementation is authoritative for edge cases called out below.

| Topic | Behavior |
|--------|----------|
| **Corpus** | All rows from **`session_chunks`** for the session that have a stored embedding (see [`ListChunksWithEmbeddingsBySessionID`](../internal/database/session_chunks.go)). No cross-session data. |
| **Query embedding** | Same model as indexing: [`rag.OpenAIEmbedder`](../internal/rag/embedder.go) → **`text-embedding-ada-002`**, **1536** dimensions. MCP search/raw tools use this embedder in [`mcpRunVectorRetrieval`](../internal/mcpserver/session_retrieval_shared.go) after [`rag.EnsureSessionIndex`](../internal/rag/index.go). |
| **Stored embeddings** | Written during indexing with the embedder’s `ModelName()` per chunk; retrieval loads vectors from the DB. |
| **Similarity** | **Cosine similarity** (see `cosineSimilarity` in [`retrieval.go`](../internal/rag/retrieval.go)): dot product divided by the product of L2 norms; mathematically in **`[-1, 1]`**; zero vectors yield **0**. Typical embedding pairs are often positive, but clients must not assume scores are non-negative. |
| **Primary transcript boost** | When the session has a **primary video**, chunks with `source_type=transcript` and `source_id` equal to that video’s UUID get the raw cosine score multiplied by [`rag.PrimaryVideoScoreBoost`](../internal/rag/retrieval.go) (**`1.2`**). Other chunks use the unmodified cosine. If there is no primary video, **no** boost is applied. |
| **Ranking order** | Sort **descending** by adjusted score; return the **first k** after sort. |
| **`k` / top-k** | Package default [`rag.DefaultTopK`](../internal/rag/retrieval.go) is **10** when callers pass `k <= 0`. MCP **`search_session`**, **`search_session_content`**, **`get_session_raw_chunks`**, and **`get_session_retrieval_context`** accept optional **`top_k`**: default **10**, maximum **50** ([`session_retrieval_shared.go`](../internal/mcpserver/session_retrieval_shared.go)). If fewer than `k` chunks exist, fewer are returned. |
| **Dimension mismatch** | Chunks whose stored embedding length **≠** query embedding length are **skipped** (silent), e.g. stale rows if the embedding model ever changed without a full reindex. |
| **Empty index** | No chunks or no embeddings → **empty** result list (not an error). |

**Stable contract (for MCP clients and audits)**

- Session boundary, cosine + optional primary-transcript boost, descending score order, and top-k truncation are **intentional product behavior**.
- Exact **floating-point** scores may vary with library/hardware in principle; clients should treat scores as **ordinal** (rank), not as stable decimals across releases unless tested.

**Implementation details (may change without a semver promise on JSON fields)**

- **Ties:** When two chunks have **equal** adjusted scores, order is **not** guaranteed stable across processes or Go versions (`sort.Slice` is not stable). Rare in practice; rely on rank bands, not tie order.
- **Reindexing** replaces embeddings for the session; chunk **row identity** and ordering in the DB are not part of the public MCP contract.

### Raw retrieval context (SCRUM-45)

**`get_session_raw_chunks`** and **`get_session_retrieval_context`** use the same embedding + [`rag.RetrieveTopKWithScores`](../internal/rag/retrieval.go) stack as **`search_session`** / **`search_session_content`** (shared wiring in the MCP server). The response wraps ranked chunks under **`retrieval_context`** with chunk identifiers and hashes for downstream agents — no answer generation.

### Source-scoped chunk listing (SCRUM-46)

**`get_session_source_chunks`** ensures the session index exists, then reads **`session_chunks`** via [`Database.ListSessionChunksBySessionIDAndSource`](../internal/database/session_chunks.go) (filtered by `source_type` and optional `source_id`). This is the same persisted chunk set used by [`rag.RetrieveTopK`](../internal/rag/retrieval.go) after embedding.

### Session RAG Q&A (SCRUM-44, SCRUM-50)

**`ask_session`** and **`ask_session_question`** are the same handler: retrieval ([`rag.RetrieveTopK`](../internal/rag/retrieval.go)), then [`utils.GenerateAnswer`](../internal/utils/qa.go) with the same prompts, confidence threshold, and “not covered” behavior as the web app. It creates **question** and **answer** rows (like the HTTP ask endpoint) so the session history stays consistent. Duplicate questions in the same thread return **`cached_repeat: true`** without calling the LLM again.

### Confidence and automation hints (SCRUM-51)

Integrators should treat MCP **`ask_session`** / **`ask_session_question`** responses as **advisory** for downstream automation. The server exposes the same **`confidence`** (0–1) and **`answer_status`** strings as persisted answers. It also sets **`automation_recommended`**: **`true`** only when **`answer_status`** is **`answered`** *and* **`confidence` ≥ 0.55**, matching the guardrail in [`internal/utils/qa.go`](../internal/utils/qa.go) that forces **`not_covered`** and **clears citations** when confidence is below that threshold or the model marks the question as not covered.

| Situation | Typical outcome | Safe to automate? |
|-----------|-----------------|-------------------|
| **`answer_status: answered`** and **`confidence` ≥ 0.55** | Grounded answer; citations present when the model supplied them and normalization succeeded | **`automation_recommended: true`** — still subject to your org’s policy; never invent sources. |
| **`answer_status: not_covered`** or **`confidence` &lt; 0.55** | May include an explanatory **`answer_text`**; citations are **cleared** by QA rules when the guardrail applies | **`automation_recommended: false`** — **human review** or a non-automated path. |
| **`answer_status: error`** | LLM or pipeline failure message | **`automation_recommended: false`**. |

**No invented sources:** If evidence is thin, the shared QA path clears or downgrades citations per existing rules; MCP does not add citations that were not grounded in retrieval.

### Citations (SCRUM-53)

**`ask_session`** / **`ask_session_question`** return **structured citations** aligned with **`POST /api/sessions/:id/ask`**: each item uses the same pipeline ([`citation.NormalizeCitations`](../internal/citation/normalize.go) on the LLM output, then persistence on the answer). Field names match the HTTP **`SessionAskCitation`** shape where applicable:

| Field | Meaning |
|-------|--------|
| **`citation_id`** | Stable within the answer: **`C1`**, **`C2`**, … (assigned in order during normalization). |
| **`chunk_id`** | Session chunk UUID string (indexed row in **`session_chunks`**). |
| **`source_type`** | **`transcript`** \| **`material`** \| **`link`**. |
| **`source_id`** | Video UUID, material UUID, or link UUID as string. |
| **`label`** | Human-readable label (e.g. transcript time range, document title + page/block). |
| **`excerpt`** | Short excerpt (~200 chars) for preview. |
| **`anchor`** | Map with **`type`** (`time_range` \| `page` \| `block` \| `section` \| `link` \| `none`) and type-specific fields: **`start_ms`** / **`end_ms`** (transcript), **`page`** / **`block`** (materials), **`section`**, **`url`** (link chunks when present). |
| **`navigation`** | Resolved target for deep-linking — same [`citation.ResolveCitationTarget`](../internal/citation/resolver.go) as HTTP: **`type`** (`video` \| `pdf` \| `doc` \| `text` \| `url`), optional **`seek_ms`**, **`page`**, **`block`**, **`url`**, **`fragment`**. |

**Ordering:** Citations appear in **array order** matching the persisted answer (after normalization). That order is **`C1` first, then `C2`, …** — not re-sorted by retrieval score.

**Search / raw tools:** **`search_session`**, **`get_session_raw_chunks`**, and related tools return **chunk-level** **`anchor`** JSON from the index (no synthesized citations). They do **not** emit `citation_id` / `navigation` objects; use **`ask_session`** when you need the full citation contract.

**Examples (abridged JSON)**

*Transcript (time range):*

```json
{
  "citation_id": "C1",
  "chunk_id": "uuid-of-chunk",
  "source_type": "transcript",
  "source_id": "uuid-of-video",
  "label": "Transcript 1:12–4:38",
  "excerpt": "…",
  "anchor": { "type": "time_range", "start_ms": 72000, "end_ms": 278000 },
  "navigation": { "type": "video", "seek_ms": 72000 }
}
```

*Material (PDF page):*

```json
{
  "citation_id": "C2",
  "source_type": "material",
  "source_id": "uuid-of-material",
  "anchor": { "type": "page", "page": 4 },
  "navigation": { "type": "pdf", "page": 4 }
}
```

*Link (URL):*

```json
{
  "citation_id": "C3",
  "source_type": "link",
  "source_id": "uuid-of-session-link",
  "anchor": { "type": "link", "url": "https://example.com/doc" },
  "navigation": { "type": "url", "url": "https://example.com/doc" }
}
```

### MCP vs HTTP RAG parity (SCRUM-49)

**Goal:** Avoid drift between **`POST /api/sessions/:id/ask`** ([`internal/handlers/session_ask.go`](../internal/handlers/session_ask.go)) and **`ask_session`** / **`ask_session_question`**, and the same for any tool that calls [`rag.EnsureSessionIndex`](../internal/rag/index.go) / [`rag.IndexSession`](../internal/rag/index.go).

| Concern | Shared building blocks |
|--------|-------------------------|
| Index + embed | [`rag.EnsureSessionIndex`](../internal/rag/index.go), [`rag.IndexSession`](../internal/rag/index.go), [`rag.OpenAIEmbedder`](../internal/rag/embedder.go) |
| Retrieval | [`rag.RetrieveTopK`](../internal/rag/retrieval.go) (cosine + primary-transcript boost) |
| Answer + guardrails | [`utils.GenerateAnswer`](../internal/utils/qa.go), [`utils.ConvertQAResponseToAnswer`](../internal/utils/qa.go) |
| Citations | [`citation.NormalizeCitations`](../internal/citation/normalize.go); MCP ask responses also attach [`citation.ResolveCitationTarget`](../internal/citation/resolver.go) as **`navigation`** (SCRUM-53) |

**Object storage:** HTTP passes the API’s R2 (or nil) client into indexing. **`talkback-mcp`** now does the same: when **`STORAGE_DRIVER=r2`** and R2 env vars match [`cmd/api`](../cmd/api/main.go) ([`internal/storage/r2`](../internal/storage/r2)), the MCP process builds the session index **including R2-backed PDF page chunking**. If R2 is not configured, behavior matches the previous MCP default (index uses DB `extracted_text` paths only — same as API without storage).

**Regression watchlist:** If you change retrieval, indexing, embedding model, or QA behavior, update **both** [`session_ask.go`](../internal/handlers/session_ask.go) and [`session_ask_question.go`](../internal/mcpserver/session_ask_question.go) (shared implementation for **`ask_session`** and **`ask_session_question`**) or extract a shared helper. Search/raw/source-chunk tools use the same `Storage` wiring via [`RegisterConfig.Storage`](../internal/mcpserver/register.go).

## Hosted deployment (containers & ops) — SCRUM-42

This section describes a **single trusted deployment** (one environment, one MCP process or small replica count). Multi-tenant SaaS MCP is out of scope.

### Process model

- **Binary:** `talkback-mcp` from [`cmd/talkback-mcp`](../cmd/talkback-mcp). Same build as local dev; no separate “enterprise” binary.
- **Stdio:** MCP JSON-RPC is on **stdin/stdout**; operational logs go to **stderr** (see [Structured logging](#structured-logging-scrum-40)). Nothing else may write to stdout.
- **Supervisor / probes:** Stdio MCP still closes when **stdin** closes (e.g. a bare Pod with no attached client). For **Kubernetes**, set **`TALKBACK_MCP_HEALTH_ADDR`** (e.g. `:8080`) so the process also serves **GET `/healthz`** and **GET `/ready`** for liveness/readiness without a stdio bridge (SCRUM-69). You can still add a bridge later for remote MCP clients over stdio.

### Container image

The repo includes a **minimal image** (MCP only, not the full TalkBack API stack):

```bash
docker build -f deploy/Dockerfile.mcp -t talkback-mcp:latest .
```

- **Layout:** single static binary at `/usr/local/bin/talkback-mcp`, non-root user `65532`, CA certificates for Postgres TLS.
- **Main app image:** The root [`Dockerfile`](../Dockerfile) builds the HTTP API (`cmd/api`); it does **not** include `talkback-mcp`. Use `deploy/Dockerfile.mcp` when you only need the MCP server.

### Environment (hosted)

| Variable | Typical hosted value | Notes |
|----------|----------------------|--------|
| `TALKBACK_MCP_API_KEY` | **Required** | Inject via secrets manager / K8s Secret / parameter store — **never commit** real values. |
| `TALKBACK_MCP_REQUIRE_CLIENT_KEY` | **`true`** | Clients must send a matching key on each `tools/call` (see **Strict mode** under [Authentication](#authentication)). |
| `DATABASE_URL` | If using `get_session_metadata` | Same Postgres as TalkBack when that tool is required; omit to register only `health_check`. |
| `TALKBACK_MCP_ACTING_USER_ID` | If using `get_session_metadata` | TalkBack `users.id` UUID; inject alongside DB URL. |
| `TALKBACK_MCP_HEALTH_ADDR` | e.g. `:8080` | When set, starts an **HTTP** listener (in addition to stdio MCP) with **GET `/healthz`** (liveness) and **GET `/ready`** (readiness; pings Postgres when `DATABASE_URL` is in use). Omit for IDE-only local use. |
| `STORAGE_DRIVER` + R2 env | Optional | **`STORAGE_DRIVER=r2`** with the same R2 settings as [`cmd/api`](../cmd/api/main.go) so MCP **`EnsureSessionIndex`** can fetch PDFs from object storage — RAG parity with HTTP (SCRUM-49). |

### Secrets injection

- **Kubernetes:** mount with `envFrom.secretRef` or the [External Secrets Operator](https://external-secrets.io/) / cloud-specific sync to Secrets. Example manifest (placeholders only): [`deploy/k8s/mcp-hosted.example.yaml`](../deploy/k8s/mcp-hosted.example.yaml).
- **AWS / GCP / Azure:** resolve secrets at deploy time into env or files; do not bake credentials into images.
- **Rotation:** `TALKBACK_MCP_API_KEY` supports comma-separated keys; roll by adding the new key before retiring the old one.

### Health and observability

| Signal | Use |
|--------|-----|
| **Process** | Exit code non-zero on fatal config (`TALKBACK_MCP_API_KEY` missing, invalid `TALKBACK_MCP_ACTING_USER_ID`, DB connect failure at startup when `DATABASE_URL` is set). Restart policies catch crashes. |
| **Stderr** | Structured lines (`event=tool_complete`, `event=auth_failed`, …) for auth and tool outcomes — see [Structured logging](#structured-logging-scrum-40). |
| **HTTP `/healthz` and `/ready`** | When **`TALKBACK_MCP_HEALTH_ADDR`** is set, **GET `/healthz`** returns JSON matching the `health_check` tool shape (`status`, `service`, `version`). **GET `/ready`** returns `200` when Postgres is not required, or when the pool can **ping** the DB (otherwise `503`). **Unauthenticated** — intended for in-cluster probes; protect with NetworkPolicy or similar if needed. |
| **`health_check` MCP tool** | Still requires a **client** that speaks MCP JSON-RPC over **stdio** (IDE, bridge, or test harness). |

### Failure modes (observable behavior)

| Condition | What happens |
|-----------|----------------|
| Missing / empty `TALKBACK_MCP_API_KEY` | Process **exits** at startup (`LoadAuthFromEnv`). |
| Invalid `TALKBACK_MCP_ACTING_USER_ID` | Process **exits** at startup. |
| `DATABASE_URL` set but DB unreachable at startup | Process **exits** during `database.New()` in [`cmd/talkback-mcp`](../cmd/talkback-mcp). |
| Wrong client key in strict mode | `tools/call` returns unauthorized; stderr logs `event=auth_failed` (no secrets). |
| DB up at start but fails later | `get_session_metadata` may return errors to the client; check stderr and app DB health. |
| **`TALKBACK_MCP_HEALTH_ADDR` set but listen fails** (bad address or port in use) | Process **exits** from the health HTTP goroutine (`log.Fatalf`). |
| **`/ready` with DB** | Returns **503** if Postgres ping fails within ~2s (readiness probe should fail). |

### Kubernetes and Docker references

- **Example manifest (Secret + ConfigMap + Deployment):** [`deploy/k8s/mcp-hosted.example.yaml`](../deploy/k8s/mcp-hosted.example.yaml) — edit image name, resources, and secret wiring before apply. Sets **`TALKBACK_MCP_HEALTH_ADDR=:8080`** and HTTP **liveness/readiness** probes on `/healthz` and `/ready`.
- **Docker Compose:** The main stack is [`deploy/docker-compose.yml`](../deploy/docker-compose.yml) (Postgres + API). There is no default `talkback-mcp` service there; run the MCP image beside your stack when you have a stdio bridge, or use the image in CI/smoke with `docker run -i` and a test client.
