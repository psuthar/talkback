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
| `provider` | no | Defaults to `"other"`. Must be in the existing video_sources_provider_check allowlist (`loom`, `zoom`, `other`, `teams`, `google_meet`). |

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

```json
{
  "version": 1,
  "title": "Weekly Decision Review — Template",
  "premise": "Are we on track to ship feature X this quarter?",
  "primary_decision": "Ship / hold / extend by 2 weeks.",
  "elements": [
    {
      "kind": "material",
      "id": "agenda",
      "title": "Agenda",
      "url": "https://templates.example.com/weekly/agenda.pdf",
      "content_type": "application/pdf"
    },
    {
      "kind": "link",
      "id": "decision-policy",
      "title": "Decision policy",
      "url": "https://wiki.example.com/decision-policy"
    },
    {
      "kind": "video_source",
      "id": "session-recording",
      "embed_url": "https://recordings.example.com/embed/abc123"
    }
  ],
  "primary": {
    "kind": "document",
    "ref": "agenda"
  }
}
```
