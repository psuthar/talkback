# Fixture Guidelines

Rules for test data in TalkBack smoke tests. Follow these to keep fixtures minimal,
deterministic, and fast.

---

## Core Principle

> Seed exactly what the test needs to fail clearly if the behavior breaks.
> Nothing more.

A smoke test for the invite flow needs: one session, one creator user, one invitation.
It does not need multiple materials, a video, or a transcript.

---

## Fixture Format

Fixtures are **inline Go values**, not external files.

**Allowed:**
```go
const smokeTextFixture = "Quarterly review: revenue up 12%, churn down 8%, APAC expansion confirmed."

var smokeSession = &models.Session{
    Title:  "Smoke Test Session",
    Status: models.SessionStatusOpen,
}
```

**Not allowed:**
- JSON files loaded from disk
- SQL dump files
- External CSV or spreadsheet imports
- Real recordings, transcripts, or PDFs from production

---

## IDs

Let the database generate UUIDs. Only hardcode UUIDs when you need to reference an entity
across multiple helper calls in the same test.

```go
// Good: DB generates the ID
session, err := db.CreateSession(ctx, &models.Session{Title: "Smoke"})

// Acceptable: hardcode only when cross-referencing
knownSessionID := uuid.MustParse("00000000-0000-0000-0000-000000000001")
// ...and reference it in both the session and the material
```

Never hardcode the same UUID across different test files — UUID collisions cause
parallel test failures.

---

## Text Fixtures

Material text and transcript fixtures must be:

- **Short**: 2–5 sentences maximum.
- **Distinctive**: include 2–3 keywords that are unlikely to appear in a generic LLM response by chance. Good: `APAC expansion`, `churn reduced 8%`, `Meridian proposal`. Bad: `the meeting`, `we discussed`, `important topics`.
- **Stored as constants** at the top of the test file:

```go
const (
    smokeDocText    = "Meridian proposal approved. APAC expansion budget set at $2.4M. Churn target: below 6%."
    smokeTranscript = "[00:00:05 --> 00:00:12] Speaker 1: The Meridian proposal has been approved by the board."
)
```

- **Not duplicated** — if two tests use the same fixture text, extract it to `smoke_fixtures_test.go`.

---

## User Fixtures

Use the existing auth helpers. Do not create users directly via `db.CreateUser` in smoke
tests unless you are testing the user creation path itself.

```go
// Preferred — helper handles user + login session + cookie in one call:
creator := addUserSessionCookie(t, h, req, "creator@smoke.test")
participant := addParticipantSessionCookie(t, h, req)
admin := addAdminSessionCookie(t, h, req)

// Only use direct DB creation when testing the creation endpoint itself:
user, err := h.DB.CreateUser(ctx, &models.User{
    Email:      "direct@smoke.test",
    GlobalRole: models.GlobalRoleCreator,
})
```

**Email convention for smoke tests:** `<role>@smoke.test` — never use real email addresses.

---

## Session Fixtures

```go
// Preferred: use the existing test helper
session := createTestSessionForHandlers(t, h.DB, "Smoke Session Title")

// Only create manually if testing fields not covered by the helper
session, err := h.DB.CreateSession(ctx, &models.Session{
    Title:           "Smoke Decision Session",
    PrimaryDecision: ptr("Approve Meridian expansion"),
})
```

---

## Material Fixtures

For upload-path tests, use an in-memory `bytes.Buffer` with the text constant:

```go
var buf bytes.Buffer
mw := multipart.NewWriter(&buf)
fw, _ := mw.CreateFormFile("file", "report.txt")
fw.Write([]byte(smokeDocText))
mw.WriteField("kind", "document")
mw.Close()
```

For DB-path tests (testing retrieval, not ingestion):

```go
material, err := h.DB.CreateMaterial(ctx, &models.Material{
    SessionID:     session.ID,
    Kind:          "document",
    TextStatus:    models.MaterialTextStatusReady,
    ExtractedText: ptr(smokeDocText),
})
```

---

## Citation Fixtures

When testing answer retrieval, citation fixtures must use valid `SourceType` values:

```go
models.Citation{SourceType: "material",    ChunkID: "chunk-material-001"}
models.Citation{SourceType: "transcript",  ChunkID: "chunk-transcript-001"}
models.Citation{SourceType: "link",        ChunkID: "chunk-link-001"}
```

Do not fabricate anchor fields (page numbers, time ranges) unless the test specifically
covers anchor rendering.

---

## Transcript Fixtures

Minimal VTT snippet:

```go
const smokeVTT = `WEBVTT

00:00:05.000 --> 00:00:12.000
The Meridian proposal has been approved.

00:00:13.000 --> 00:00:19.000
APAC expansion budget confirmed at two point four million.
`
```

---

## What Not to Fixture

| Do not seed | Why |
|---|---|
| Multiple sessions for a single test | Noise; one is enough |
| A video source unless testing video flow | Extra async pipeline complexity |
| Processing jobs manually | Let the handler enqueue them naturally |
| Admin users for participant-only tests | Principle of least privilege in fixtures |
| Real passwords that match production patterns | Use `SmokePass123!` or similar |

---

## Shared Fixture File

If three or more smoke tests share the same constant or helper value, extract to:

```
internal/handlers/smoke_fixtures_test.go
```

Keep it small. Do not build a "fixture framework" — a few constants and one or two
small helper funcs is the right scope.

---

## Helper Naming Conventions

```go
// Builders: create<Thing>ForSmoke
func createSmokeMaterial(t *testing.T, db *database.DB, sessionID uuid.UUID) *models.Material

// Factories using existing infra: prefer createTestSessionForHandlers from handlers_test.go
session := createTestSessionForHandlers(t, h.DB, "Smoke")

// Pointer helpers (already common in the codebase):
func ptr[T any](v T) *T { return &v }
```
