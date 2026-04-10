# MCP fallbacks: structured decisions & action items (SCRUM-62)

Shared semantics for **successful** and **failed** calls to:

- **`get_session_decisions`** / **`get_decisions`** (persisted fields — [decisions contract](mcp-session-decisions-schema.md))
- **`get_session_action_items`** / **`get_action_items`** (ephemeral on read — [action items contract](mcp-session-action-items-schema.md))

Implementation: `internal/mcpserver/session_decisions.go`, `internal/mcpserver/session_action_items.go`. Tool errors use the repo-wide pattern in [`docs/mcp-server.md`](mcp-server.md) (**`error`**, **`http_status`**, optional **`error_code`**).

---

## Success responses (200-class tool result)

### Decisions (persisted)

| Situation | Client expectation |
|-----------|-------------------|
| No premise / primary_decision / decision_outcome in DB | Properties **omitted** (not `null`). |
| No stance rows | **`decision_stances`** is **`[]`** (never omit the array). |
| Partial text fields set | Only populated fields appear; others omitted. |

**Not an error:** “Empty” decision content is a **valid** v1 payload with **`schema_version`**, **`session_id`**, and **`decision_stances`** (possibly empty).

### Action items (ephemeral)

| Situation | Client expectation |
|-----------|-------------------|
| No indexed chunks / nothing retrieved | **`action_items`**: **`[]`**, **`low_signal`**: **`true`**, optional **`note`** explains missing index content. **`llm_model`** omitted (no LLM call). |
| Chunks present but no actionable items | **`action_items`**: **`[]`**, **`low_signal`**: **`true`**, optional **`note`** (e.g. low-signal explanation). **`llm_model`** set when an LLM call ran. |
| Items returned | **`low_signal`**: **`false`** (unless generator set it); **`action_items`** non-empty; **`owner`** only when grounded. |

**Not an error:** Empty **`action_items`** with **`low_signal: true`** is **agent-safe** — automation should branch on **`low_signal`** and **`note`**, not assume failure.

---

## Errors (non-2xx-style tool errors)

Both tool families share the same **access and input** errors as other session tools (see `mcp-server.md`):

| Typical `http_status` | When |
|----------------------|------|
| **400** | Bad **`session_id`** (not a UUID); action items: session has **no artifacts**. |
| **403** | Acting user not configured, not found, or **no access** to session (`error_code` may distinguish). |
| **404** | Session not found. |
| **503** | Action items: **`OPENAI_API_KEY`** missing, embedding/index failures, etc. (`error_code` in payload). |

On failure, clients receive **structured error JSON** (string content in the tool result), **not** the success schema above. Do not parse success-shaped fields from error responses.

---

## Cross-tool consistency (v1)

1. **Machine-parseable:** Prefer **`error_code`** + **`http_status`** for branching; use **`note`** / **`low_signal`** only on **success** bodies for action items.
2. **No synthetic filler:** Do not invent premise/decision text or action items when data is missing; use omission, **`[]`**, **`low_signal`**, and **`note`** as documented.
3. **Aliases:** **`get_decisions`** and **`get_action_items`** behave identically to their **`get_session_*`** counterparts for fallbacks and errors.

---

## References

- [mcp-session-decisions-schema.md](mcp-session-decisions-schema.md) (SCRUM-57)
- [mcp-session-action-items-schema.md](mcp-session-action-items-schema.md) (SCRUM-58)
- [mcp-server.md](mcp-server.md) — tool list, env vars, error JSON
