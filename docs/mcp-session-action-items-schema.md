# MCP contract: `get_session_action_items` / `get_action_items` (v1)

**SCRUM-58 — source of truth** for the JSON shape returned by the MCP tools **`get_session_action_items`** and **`get_action_items`** (same handler — SCRUM-61). Implementation: `internal/mcpserver/session_action_items.go`. On-read generation uses `internal/utils.ExtractActionItemsFromContext` (SCRUM-59).

**v1 scope:** **Ephemeral / not persisted** — action items are generated per request; there is no `action_items` table. Output may include optional diagnostic fields (`note`, `llm_model`) for agent-safe interpretation.

---

## Versioning

| Field | Meaning |
|-------|---------|
| `schema_version` | String **`"1"`** for this contract. Increment when a **breaking** change is introduced (new required properties, renamed fields, changed null semantics). |

---

## Top-level object

| Property | Type | Required | Semantics |
|----------|------|----------|-----------|
| `schema_version` | string | yes | Must be **`"1"`**. |
| `session_id` | string (UUID) | yes | The TalkBack session id. |
| `action_items` | array | yes | Always present; **empty array** `[]` when nothing is returned or signal is low. |
| `low_signal` | boolean | yes | **`true`** when retrieval or the LLM indicates weak or no actionable content; clients should not treat empty `action_items` as an error by itself. |
| `note` | string | no | Human-readable explanation (e.g. no indexed chunks, or low-signal explanation). **Omitted** when not set. |
| `llm_model` | string | no | Model id used for extraction when an LLM call ran (e.g. `gpt-4o-mini`). **Omitted** when no LLM call occurred (e.g. empty chunks path). |

---

## `action_items[]` items

Each element is one **ephemeral** action item (not stored in Postgres v1).

| Property | Type | Required | Semantics |
|----------|------|----------|-----------|
| `description` | string | yes | Short, imperative next step grounded in session content. |
| `owner` | string | no | Present only when explicitly grounded in content (named person/role). **Omitted** when not inferable; never guessed from thin air. |

---

## Examples

**Empty list, low signal (no retrieval):**

```json
{
  "schema_version": "1",
  "session_id": "550e8400-e29b-41d4-a716-446655440000",
  "action_items": [],
  "low_signal": true,
  "note": "No indexed content was retrieved for this session; action items require transcript or material text in the index."
}
```

**Items with optional owner:**

```json
{
  "schema_version": "1",
  "session_id": "550e8400-e29b-41d4-a716-446655440000",
  "action_items": [
    { "description": "Send the revised budget to Finance" },
    { "description": "Schedule follow-up with the vendor", "owner": "Alice Chen" }
  ],
  "low_signal": false,
  "llm_model": "gpt-4o-mini"
}
```

---

## Relationship to other contracts

- **Decisions (persisted):** `docs/mcp-session-decisions-schema.md` — different tool and lifecycle; do not mix fields.
- **Fallbacks (empty / partial / errors):** [`docs/mcp-structured-intelligence-fallbacks.md`](mcp-structured-intelligence-fallbacks.md) (SCRUM-62) — shared semantics with decision tools.
