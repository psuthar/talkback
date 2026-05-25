# GenAI call-site inventory

This is the contract input to the GenAI Guardrails epic (SCRUM-560).
Every LLM call inside TalkBack is listed here with the trust boundary
on each of its inputs and the slice that will own its guardrails.

When a new LLM call site is added, append a row here in the same PR.
When the actual code differs from what this file claims, the
implementing slice is responsible for correcting the row, not silently
diverging.

## Trust boundaries — definitions

Each input to an LLM call has one of three trust levels:

- **system** — controlled by TalkBack code (prompt templates, fixed
  instructions). The LLM can treat it as instructions.
- **user** — controlled by the authenticated caller (a question typed
  in the moment). High-leverage attack vector, but the user only ever
  attacks themselves; cross-user impact requires another bug.
- **session-content** — text extracted from session materials,
  transcripts, comments, or messages authored by *any* session
  participant (not just the caller). **Always treat as untrusted
  data, never as instructions**; a malicious participant can plant
  injection payloads here that a different user would later trigger.

## Sites

| # | Call site (file) | Called by | Inputs (trust) | Output | Known risks | Slice(s) |
|---|---|---|---|---|---|---|
| 1 | `internal/utils/qa.go` (RAG Q&A) | `internal/handlers/session_ask.go` (HTTP), `internal/mcpserver/session_ask_question.go` (MCP) | `question` (user); retrieved chunks (session-content); system prompt (system) | Free-text answer with citations | Prompt injection via transcript; PII leakage; ungrounded claims; cross-session leakage via hallucinated cite; refusal-bombing legit questions | 2, 3, 4a, 4b, 4c |
| 2 | `internal/utils/action_items.go` | Worker / pipeline | Session transcript chunks (session-content); extraction prompt (system) | Structured JSON list of action items | Injection-via-transcript; malformed JSON output; over-extraction | 4c (schema validation) |
| 3 | `internal/utils/polish.go::PolishSpokenQuestionWithLLM` (user-question rewrite) | Question-input handlers in `internal/handlers` / `internal/mcpserver` (the polished question is then submitted to `qa.go`) | Raw spoken-input question (user); polish prompt (system) | Plain text — a clarified version of the user's question | Injection-at-question-rewrite — but the rewritten question is fed straight into `qa.go`, so **Slice 3's `CheckQuestion` covers it downstream**. No separate guardrail at this boundary. | Covered downstream by 3; write-path only — Slice 5 |
| 4 | `internal/obsworker/analysis.go::GenerateAIAnalysis` (observability worker) — *not* `internal/obsworker/llm.go`, which is the OpenAI/Anthropic HTTP client; the call site is in `analysis.go` | Cron / internal | Log/metric content (system); analysis prompt (system) | Analysis text | Low — all inputs are system-controlled | (write-path only — Slice 5) |
| 5 | markitdown sidecar (`services/markitdown-sidecar`, vision/OCR) | `internal/markitdown` client → upload handlers | Uploaded image bytes (user-supplied file); markitdown's prompts (system) | Extracted text/markdown | Malicious image content; sidecar prompt-injection-from-image | **Out of scope for SCRUM-560** — revisit after Slice 5 telemetry shows whether the surface is hot. |

## Out of scope (and why)

| Surface | Why deferred |
|---|---|
| Whisper STT (`internal/utils/whisper_*.go`) | Audio → text; no LLM generation step the user sees directly. Lower leverage. |
| Embeddings (`internal/utils/embeddings.go`, `internal/rag/embedder.go`) | No generation, no free-text output. ACL is enforced at retrieval (`search_all_sessions`), not embedding. |
| Markitdown image/file extraction (sidecar) | Listed above as site #5; out of this epic. Has its own observability surface; revisit after Slice 5 (SCRUM-568) telemetry shows whether image-prompt-injection is happening in practice. |

## How to extend

When a PR adds a new LLM call site:

1. Append a row to the table with the call-site file, its callers, and
   the trust level of each input.
2. Decide which existing slice owns its guardrails, or file a new
   ticket if none fit.
3. Update the `guardrails_fired` enum in
   [`log-shape.md`](log-shape.md) if a new guardrail name is needed,
   and add the refusal entry to [`refusal-shape.md`](refusal-shape.md).
