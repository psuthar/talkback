# MCP contract: `get_session_decisions` (v1)

**SCRUM-57 — source of truth** for the JSON shape returned by the MCP tools **`get_session_decisions`** and **`get_decisions`** (same handler — SCRUM-60; implementation: `internal/mcpserver/session_decisions.go`). Use this document and the machine-readable schema below for clients and tests.

**v1 scope:** **Persisted fields only** — `sessions` columns `premise`, `primary_decision`, `decision_outcome`, and rows in `decision_stances` (plus submitter email). No LLM extraction or inferred decision fields.

---

## Versioning

| Field | Meaning |
|-------|---------|
| `schema_version` | String **`"1"`** for this contract. Increment when a **breaking** change is introduced (new required properties, renamed fields, changed null semantics). Non-breaking additions may still bump this at maintainers’ discretion. |

---

## Top-level object

| Property | Type | Required | Semantics |
|----------|------|----------|-----------|
| `schema_version` | string | yes | Must be **`"1"`**. |
| `session_id` | string (UUID) | yes | The TalkBack session id. |
| `premise` | string | no | Maps to `sessions.premise`. **Omitted** when the DB value is `NULL` (Go: `omitempty` on nil pointer). |
| `primary_decision` | string | no | Maps to `sessions.primary_decision`. Omitted when `NULL`. |
| `decision_outcome` | string | no | Maps to `sessions.decision_outcome`. Omitted when `NULL`. |
| `decision_stances` | array | yes | Always present; **empty array** `[]` when there are no stance rows. |

### Null and empty strings

- **Database `NULL`** for `premise` / `primary_decision` / `decision_outcome` → JSON **omits** the property (no `null` value in typical encoder output).
- If a future product change stores empty string vs NULL differently, clients should treat **missing** and **empty string** as “no user-visible value” unless documented otherwise.

---

## `decision_stances[]` items

Each element corresponds to one **`decision_stances`** row, with **`user_email`** from the **`users`** join (same as HTTP `GET /api/sessions/{id}/stances`).

| Property | Type | Required | Semantics |
|----------|------|----------|-----------|
| `id` | string (UUID) | yes | Row id. |
| `session_id` | string (UUID) | yes | Session id. |
| `user_id` | string (UUID) | yes | Submitter’s `users.id`. |
| `stance` | string | yes | One of: `agree`, `disagree`, `conditional`, `abstain`, `need_more_info` (see `internal/models.ValidStances`). |
| `rationale` | string | no | Omitted when `NULL` in DB. |
| `user_email` | string | yes | Submitter email. |
| `created_at` | string (RFC3339) | yes | Timestamps are serialized in **UTC** (e.g. `2026-04-10T12:00:00Z`). |
| `updated_at` | string (RFC3339) | yes | Same as above. |

**Ordering:** Array order follows the implementation (currently newest-first from the DB query). **Do not** rely on a stable sort across releases without checking release notes.

---

## JSON Schema (validation)

Canonical file (Draft 2020-12):

**[`docs/schemas/mcp-session-decisions-v1.schema.json`](schemas/mcp-session-decisions-v1.schema.json)**

---

## Examples

### Minimal session (no premise/decision text, no stances)

```json
{
  "schema_version": "1",
  "session_id": "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
  "decision_stances": []
}
```

### Session with text fields and one stance

```json
{
  "schema_version": "1",
  "session_id": "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
  "premise": "We need a vendor for Q3.",
  "primary_decision": "Select vendor A or B.",
  "decision_outcome": "Vendor A selected.",
  "decision_stances": [
    {
      "id": "11111111-2222-3333-4444-555555555555",
      "session_id": "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
      "user_id": "66666666-7777-8888-9999-aaaaaaaaaaaa",
      "stance": "agree",
      "rationale": "Aligns with budget",
      "user_email": "participant@example.com",
      "created_at": "2026-04-10T15:00:00Z",
      "updated_at": "2026-04-10T15:00:00Z"
    }
  ]
}
```

---

## Related

- **Implementation:** `internal/mcpserver/session_decisions.go`
- **Product model:** `internal/models.Session`, `internal/models.DecisionStance`, `internal/database` session/stance accessors
- **MCP overview:** [`docs/mcp-server.md`](mcp-server.md)
