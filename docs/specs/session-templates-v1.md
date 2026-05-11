# Session Templates — v1 Schema Specification

**Status:** v1 (shipped)
**Owner:** SCRUM-350 epic
**Implemented across:** SCRUM-351 (schema), SCRUM-352 (preflight), SCRUM-357 (worker + import job model), SCRUM-358 (storage refactor), SCRUM-362 (top-level metadata + primary pointer), SCRUM-363 (element-shape variants + segments_url)

A session template is a JSON descriptor that materializes a fresh session by fetching public HTTP(S) references and threading them through the existing `internal/sessionimport` primitives. Templates and `CopySession` share the same destination-side primitives via the `SourceData` adapter seam.

## Top-level object

```json
{
  "version": 1,
  "title": "string (required, ≤ 512 chars)",
  "premise": "string (optional)",
  "primary_decision": "string (optional)",
  "decision_outcome": "string (optional)",
  "source_reference_url": "https://... (optional)",
  "elements": [ /* element objects, see below */ ],
  "primary": { "kind": "document|link|video", "ref": "<element_id>" }
}
```

| Field | Type | Required | Notes |
|---|---|---|---|
| `version` | int | yes | Must be `1`; future versions may be additive. |
| `title` | string | yes | Becomes the new session's `title`. ≤ 512 chars. |
| `premise` | string | no | Verbatim onto `sessions.premise`. |
| `primary_decision` | string | no | Verbatim onto `sessions.primary_decision`. |
| `decision_outcome` | string | no | Verbatim onto `sessions.decision_outcome`. |
| `source_reference_url` | string | no | Recorded on the new session. Must be `https://` if supplied. |
| `elements` | array | yes (≥ 0) | Stable order is preserved. Max length: see *Limits*. |
| `primary` | object | no | If supplied, must reference an existing element. |

**Strict parsing.** Unknown top-level fields and unknown fields on element objects cause validation to fail with `unknown_field`. This is deliberate — additive fields land in v2.

## Element kinds

Every element has the common shape:

```json
{
  "kind": "material|link|video_source|transcript",
  "id": "stable-element-id",
  ... kind-specific fields ...
}
```

| Common field | Type | Notes |
|---|---|---|
| `kind` | enum | One of `material`, `link`, `video_source`, `transcript`. |
| `id` | string | `^[a-zA-Z0-9_-]{1,64}$`. Must be unique across `elements[]`. Referenced by `primary.ref`. |

### `material`

A document, image, or other file that lands as a row in `materials` with content fetched from `url`.

```json
{
  "kind": "material",
  "id": "agenda-pdf",
  "title": "Agenda",
  "url": "https://example.com/agenda.pdf",
  "content_type": "application/pdf"
}
```

| Field | Required | Notes |
|---|---|---|
| `title` | yes | Becomes `materials.title`. ≤ 256 chars. |
| `url` | yes | `https://` only. Preflight fetches and rejects per *Preflight Rejection Catalog*. |
| `content_type` | yes | Must be in the allowlist below. |

**Allowed content types for `material`:**
- `application/pdf`
- `application/vnd.openxmlformats-officedocument.presentationml.presentation` (pptx)
- `application/vnd.openxmlformats-officedocument.wordprocessingml.document` (docx)
- `text/csv`, `text/plain`
- `application/vnd.ms-excel` (xls), `application/vnd.openxmlformats-officedocument.spreadsheetml.sheet` (xlsx)
- `image/png`, `image/jpeg`

### `link`

A URL added to the session's `session_links` for citation/RAG. The fetcher records URL + (optionally) extracted text.

```json
{
  "kind": "link",
  "id": "policy-doc",
  "title": "Decision policy",
  "url": "https://wiki.example.com/decision-policy"
}
```

| Field | Required | Notes |
|---|---|---|
| `title` | no | Becomes `session_links.title`. |
| `url` | yes | `https://` only. |

No `content_type` constraint; the existing URL-extract path handles HTML / text content normalization.

### `video_source`

A reference to a hosted video. v1 supports embed-mode only (`playback_mode = "embed"`); the actual MP4 is **not** fetched and stored as a `file_artifact`. (MP4 ingest from a URL is a follow-up.)

```json
{
  "kind": "video_source",
  "id": "session-recording",
  "embed_url": "https://recordings.example.com/embed/abc123",
  "video_url": "https://recordings.example.com/abc123",
  "duration_seconds": 1800
}
```

| Field | Required | Notes |
|---|---|---|
| `embed_url` | one of {embed_url, video_url} | Iframe-embeddable URL. |
| `video_url` | one of {embed_url, video_url} | Direct URL surfaced to UI. |
| `duration_seconds` | no | Optional metadata. |
| `provider` | no | Defaults to `"other"`. Must be in the existing video_sources_provider_check allowlist (`zoom`, `other`, `teams`, `google_meet`). |

**Limits:** `playback_mode` is forced to `embed`; `source_type` is forced to `embed_url`; `transcript_status` defaults to `missing`.

### `transcript`

A pre-existing transcript imported into the new session's `transcripts` + `transcript_segments` tables.

```json
{
  "kind": "transcript",
  "id": "session-transcript",
  "source": "whisper",
  "text_url": "https://example.com/transcript.txt",
  "segments_url": "https://example.com/transcript.json"
}
```

| Field | Required | Notes |
|---|---|---|
| `source` | yes | Free-form string (e.g. `whisper`, `zoom`, `manual`). Must be unique-per-session across the `elements[]` (matches the `transcripts.UNIQUE(session_id, source)` constraint). |
| `text_url` | one of {text_url, segments_url} | Plain-text transcript body. |
| `segments_url` | one of {text_url, segments_url} | JSON segments (array of `{idx, start_ms, end_ms, text, speaker_label?, source_ref?}`). When supplied, populates `transcript_segments`. |

## `primary` pointer

Optional. If supplied, sets the new session's explicit primary content via `UpdateSessionPrimary`.

```json
"primary": {
  "kind": "document",
  "ref": "agenda-pdf"
}
```

| Field | Required | Notes |
|---|---|---|
| `kind` | yes | One of `document`, `link`, `video`. Maps to `models.SessionPrimaryContentKind`. |
| `ref` | yes | An `elements[].id` that exists in this template. |

**Pointer/kind compatibility (must match):**
- `kind: document` → ref must point to an element with `kind: material`.
- `kind: link` → ref must point to an element with `kind: link`.
- `kind: video` → ref must point to an element with `kind: video_source`.

A mismatch is a validation error (`primary_kind_mismatch`).

## Validation contract

The server-side validator is a pure function — no I/O, no DB, no URL fetches. URL accessibility is a separate **preflight** stage (SCRUM-352).

```go
func ParseAndValidate(jsonBytes []byte) (*Template, []ValidationError)
```

Errors are accumulated (not short-circuited at the first failure) so the API can return all problems in one round-trip. Each error has shape:

```go
type ValidationError struct {
    ElementID    string `json:"element_id,omitempty"`    // empty for top-level errors
    DeclaredType string `json:"declared_type,omitempty"` // e.g. "application/pdf"
    ObservedType string `json:"observed_type,omitempty"` // populated only by preflight, not the schema validator
    ReasonCode   string `json:"reason_code"`             // see catalog
    Message      string `json:"message"`                 // human-readable
}
```

### Schema validator reason codes

| Code | When |
|---|---|
| `unknown_field` | Top-level or element field not in the v1 schema. |
| `missing_required` | Required field absent. |
| `version_unsupported` | `version != 1`. |
| `title_too_long` | `title` > 512 chars. |
| `element_id_invalid` | `id` violates `^[a-zA-Z0-9_-]{1,64}$`. |
| `element_id_duplicate` | Two elements share the same `id`. |
| `element_kind_unknown` | `kind` not in the v1 enum. |
| `material_content_type_unsupported` | `content_type` not in the material allowlist. |
| `url_scheme_invalid` | URL is not `https://`. |
| `transcript_source_duplicate` | Two `transcript` elements share the same `source`. |
| `primary_ref_unknown` | `primary.ref` does not match any `elements[].id`. |
| `primary_kind_mismatch` | `primary.kind` and the referenced element's `kind` don't satisfy the compatibility table. |
| `video_source_url_missing` | Neither `embed_url` nor `video_url` supplied. |
| `transcript_url_missing` | Neither `text_url` nor `segments_url` supplied. |
| `provider_unsupported` | `video_source.provider` not in the constraint allowlist. |
| `descriptor_too_large` | Template descriptor body exceeds 256 KiB (`MaxDescriptorBytes` in `internal/sessionimport/template/template.go`). |
| `too_many_elements` | More than 100 elements in `elements[]` (`MaxElements`). |

The preflight stage (SCRUM-352) emits its own reason codes (`url_unreachable`, `content_type_mismatch`, `body_too_large`, `redirect_loop`, `auth_required`, `private_ip_blocked`, …); the schema validator does not.

## Limits (v1)

These are enforced by the validator OR the preflight, whichever is appropriate. Defaults are conservative; subject to tuning in SCRUM-353.

| Limit | Default | Where enforced |
|---|---|---|
| Max template descriptor body size | 256 KiB | Validator (parse-time) |
| Max elements per template | 100 | Validator |
| Max body size per fetched URL | 100 MiB (material), 32 KiB (link extracted text), 1 MiB (transcript) | Preflight |
| Max redirects per fetch | 5 | Preflight |
| Per-fetch timeout | 30s | Preflight |
| Per-job total wall clock | 30 min | Worker (SCRUM-357) |
| Per-user concurrent jobs | 3 | Worker |

## Out of scope (v1)

Mirrors SCRUM-350 non-goals:
- Authenticated source fetching (`Authorization` header, cookies, OAuth). Public URLs only.
- Local filesystem path imports.
- `r2://` or other backend-native URIs.
- MP4 ingest from a URL (video_source storage). Embed-mode only.
- Importing members, questions, answers, decision_stances, orchestration_recommendations, sessions_primary_history, material_views, question_views, session_participants, session_events, transcript_jobs, ingestion_jobs.
- Webhook callbacks. Job status is polled.

## Integration with `internal/sessionimport`

The TemplateSource adapter populates `sessionimport.SourceData` with synthetic `*models.Material` / `*models.SessionLink` / `*models.VideoSource` / `*models.Transcript` rows constructed from fetched URL bodies. The existing primitives — `ImportArtifacts`, `ImportFileArtifacts`, `ImportMaterials`, `ImportSessionLinks`, `ImportVideoSources`, `ImportTranscripts`, `ImportSessionMetadata` — run unchanged. Per-category remap maps are populated by element ID instead of source row UUID.

After SCRUM-358 lands, `ImportVideoSources` no longer requires `SourceStorageNamespace`, so the template path doesn't need a fake namespace.

## Worked example

The canonical, runnable example is in [`psuthar/talkback-templates`](https://github.com/psuthar/talkback-templates) — a public repo with one full hiring-committee session (6 materials: PDF, DOCX, CSV, PNG) plus the markdown / Pillow sources used to generate the binaries. Reference its template by URL when authoring your own:

```
https://psuthar.github.io/talkback-templates/hiring-committee/template.json
```

A minimal at-a-glance shape (full schema and per-kind details above):

```json
{
  "version": 1,
  "title": "Weekly Decision Review — Template",
  "premise": "Are we on track to ship feature X this quarter?",
  "primary_decision": "Ship / hold / extend by 2 weeks.",
  "elements": [
    { "kind": "material", "id": "agenda", "title": "Agenda",
      "url": "https://templates.example.com/weekly/agenda.pdf",
      "content_type": "application/pdf" },
    { "kind": "link", "id": "decision-policy", "title": "Decision policy",
      "url": "https://wiki.example.com/decision-policy" },
    { "kind": "video_source", "id": "session-recording",
      "embed_url": "https://recordings.example.com/embed/abc123" }
  ],
  "primary": { "kind": "document", "ref": "agenda" }
}
```

## Authoring a template

This section is the end-to-end recipe for taking an idea ("here's a decision I want a TalkBack session for") to a populated session in the UI. The schema sections above describe what a template must look like; this section describes what to do with one.

### 1. Where to host the template JSON and its referenced materials

Recommended: a small public GitHub repo published via [GitHub Pages](https://pages.github.com/). The canonical example lives at [`psuthar/talkback-templates`](https://github.com/psuthar/talkback-templates) — fork it, replace `hiring-committee/` with your own scenario, push, enable Pages, and your URLs are live within a minute. Pages serves every file extension we use with the correct MIME type out of the box.

**Pin to a stable URL.** GitHub Pages always serves the current state of `main`; if you want versioned URLs, version the directory (`hiring-committee-v2/`) rather than mutate the existing one, since outstanding TalkBack sessions reference the URL.

### 2. Hosts that DON'T work, and why

The TalkBack template worker stores the **HTTP-observed** Content-Type on the resulting `materials` row (see `internal/sessionimport/template/worker.go` — `observedCT` from the response headers wins over the template's declared `content_type`, which is validation-only). A wrong Content-Type lands materials mis-classified with no surfaced error.

| Host | Symptom | Verdict |
|---|---|---|
| `raw.githubusercontent.com/...` | Every file served as `text/plain; charset=utf-8`, regardless of extension. | ❌ Out for binaries. |
| Gist raw URLs | Same `text/plain` behavior. | ❌ Same problem. |
| `cdn.jsdelivr.net/gh/...@<tag>/...` | Correct MIME for PDF / PNG / CSV / TXT, but **HTTP 403 Forbidden on Office formats** (`.docx`, `.xlsx`, `.pptx`) per their content policy. | ⚠️ Works only when no Office files. |
| **GitHub Pages** | Correct MIME for every file extension we use. | ✅ Recommended. |

### 3. Pre-flight Content-Type check

Before submitting an import, run this one-liner against your hosted `template.json`:

```bash
for url in $(jq -r '.elements[].url // empty' template.json); do
  printf '%-100s  %s\n' "$url" "$(curl -sI -L "$url" | awk -F': ' 'tolower($1)=="content-type"{print $2}')"
done
```

Every URL must respond with the Content-Type the v1 allowlist expects (`application/pdf`, the openxmlformats DOCX/XLSX/PPTX MIMEs, `text/csv`, `text/plain`, `image/png`, `image/jpeg`). Any `text/plain` for a binary means a wrong host.

### 4. Trigger an import

The import endpoint is `POST /api/import-jobs` with the template descriptor as the request body:

```bash
curl -sf -X POST "https://your-talkback.example.com/api/import-jobs" \
  -H "Cookie: <your TalkBack session cookie>" \
  -H "Content-Type: application/json" \
  --data-binary "$(curl -sL https://psuthar.github.io/talkback-templates/hiring-committee/template.json)"
```

Notes:

- Authentication is required (`UserFromContext`); the calling user must have a role that allows session creation (`GlobalRole.CanCreateSessions()`).
- The body must be the descriptor JSON itself, not a `{"template_url": ...}` envelope. The endpoint validates the body in two passes: schema validation (the same `ParseAndValidate` documented above) and a synchronous preflight pass that fetches every referenced URL with SSRF protection and confirms reachability + size limits. **A non-empty error response means zero DB writes have occurred** — the destination session row is created only by the worker after preflight passes.
- See `docs/superpowers/specs/2026-05-08-session-template-http-import-design.md` for the full design rationale and the SSRF / size-limit defaults.

### 5. Response and polling

A successful submit returns HTTP 202 with the new job's id (and the destination session id, if the worker has already created the row):

```json
{ "job_id": "550e8400-e29b-41d4-a716-446655440000",
  "session_id": "f47ac10b-58cc-4372-a567-0e02b2c3d479" }
```

Validation or preflight failures return HTTP 400 with a structured `errors[]` array using the reason codes documented in `### Schema validator reason codes` (schema layer) plus the preflight-stage codes `url_unreachable`, `content_type_mismatch`, `body_too_large`, `redirect_loop`, `auth_required`, and `private_ip_blocked`.

Poll the job state at `GET /api/import-jobs/<job_id>`:

```bash
curl -sf "https://your-talkback.example.com/api/import-jobs/<job_id>" \
  -H "Cookie: <your TalkBack session cookie>" | jq '.state, .elements_state'
```

`state` is one of `queued`, `running`, `succeeded`, `partial`, `failed`. Per-element results live under `elements_state` keyed by the element id; each entry has a `status`, an optional `error_code`, and a human `message`. A terminal state of `succeeded` means every element was imported; `partial` means at least one element failed but the destination session was created and other elements landed; `failed` is a hard failure.

### 6. End-to-end runnable reference

[`psuthar/talkback-templates`](https://github.com/psuthar/talkback-templates) is the live example used to validate this section. Walk it end-to-end (host check → import → poll → verify the session in the UI) before authoring your own; it's the fastest path to a working baseline.
