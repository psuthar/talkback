# Skill: smoke-tests

Policy source: `docs/agent/testing-validation.md`.
This skill focuses on backend smoke/integration test tactics and examples, not global test policy ownership.

## Purpose

Write and refine deterministic smoke/integration tests for TalkBack's core backend flows. Tests must produce a reliable green/red signal fast, without browser automation, without flaky timing, and without mocking the database.

**This skill covers API- and service-level tests only.** Do not introduce Playwright, Selenium, or any browser driver unless the repo explicitly forces it for a specific assertion.

---

## Repo Test Infrastructure (what already exists)

| Location | What it provides |
|---|---|
| `internal/test/testdb.go` | `SetupTestDB(t)` – spins up an isolated per-test Postgres DB; `CreateSharedTestDB()` – one DB for a whole package; `TruncateTables(t, pool)` |
| `internal/handlers/*_test.go` | `setupTestHandlersParallel(t)` – isolated DB + `*Handlers` + cleanup func; `setupTestHandlersWithInvitations(t)` – adds invitation service |
| `internal/handlers/admin_users_test.go` | `addAdminSessionCookie`, `addUserSessionCookie`, `addParticipantSessionCookie` – create user + login session + attach cookie |
| `internal/database/*_test.go` | `createTestSession` and per-package DB helpers |
| `internal/migrations/` | Embedded FS; `runTestMigrations(databaseURL)` pattern used in every TestMain |

**Key test patterns in use:**

```go
// Per-test isolated DB (preferred for handler tests)
h, cleanup := setupTestHandlersParallel(t)
defer cleanup()

// Shared DB per package (preferred for pure DB-layer tests)
func TestMain(m *testing.M) {
    sharedURL, cleanupDB, _ := test.CreateSharedTestDB()
    runTestMigrations(sharedURL)
    os.Setenv("DATABASE_URL", sharedURL)
    code := m.Run()
    cleanupDB()
    os.Exit(code)
}

// Auth
addAdminSessionCookie(t, h, req)
addUserSessionCookie(t, h, req, "creator@test.com")   // returns *models.User
addParticipantSessionCookie(t, h, req)                 // returns *models.User
```

---

## Smoke Test Standard for TalkBack

A smoke test in this repo is a Go test (`_test.go`) that:

1. **Exercises a complete vertical slice** of one TalkBack flow end-to-end at the HTTP handler or service layer.
2. **Passes on a fresh, migrated Postgres DB** with no pre-seeded data beyond what the test creates.
3. **Completes in under 10 seconds** on a local developer machine (no LLM calls, no real Zoom API calls in CI).
4. **Makes all assertions on response bodies, DB state, or service return values** — not on logs, console output, or timing.
5. **Cleans up after itself** via `TruncateTables` or per-test DB teardown.

---

## Covered Flows

The five smoke flows to cover, in priority order:

### Flow 1 — Admin/Session Setup
`POST /api/auth/login` → `POST /sessions` → `GET /sessions/{id}`

Verify: session created, creator linked, status and index_status default values correct.

### Flow 2 — Material Ingestion
`POST /sessions/{id}/materials/upload` → poll `GET /materials/{id}` until `text_status = "ready"` (or assert job enqueued if pipeline is async).

Verify: material row exists, `session_id` correct, job enqueued with expected source type.

### Flow 3 — Invite Flow
`POST /api/sessions/{id}/invitations` (as creator) → `POST /api/invitations/resolve` (with token) → `POST /api/invitations/accept` (new account) → `GET /api/me`

Verify: invitation row exists, token resolves to correct session, accepted user has `GlobalRoleParticipant`, session membership created.

### Flow 4 — Participant Access
Authenticated participant → `GET /sessions/{id}` → `GET /sessions/{id}/artifacts`

Verify: 200 response, participant cannot reach creator-only endpoints (expect 403/401).

### Flow 5 — Ask/Answer Validation
`POST /sessions/{id}/questions` → `POST /questions/{id}/answers` (stub or real RAG) → `GET /sessions/{id}/questions`

Verify: answer row persisted, `answer_status` is `"answered"` or `"not_covered"`, citations array present (may be empty for stub), confidence in [0.0, 1.0].

---

## Workflow (apply this every time)

1. **Read before writing** — open `internal/handlers/handlers_test.go` and the relevant `*_test.go` for the flow you're covering. Match the existing test structure exactly.
2. **Identify the handler under test** — trace the route in `cmd/api/main.go` to the handler func in `internal/handlers/`.
3. **Use existing helpers** — never re-implement `setupTestHandlersParallel`, `addAdminSessionCookie`, etc. Import from the same package.
4. **Seed only what the test needs** — one session, one material, one user per role. No shared global fixtures.
5. **Assert on response status first**, then unmarshal body, then assert specific fields.
6. **For async pipeline steps** — assert that the job row was enqueued (DB query), not that it completed, unless you control a synchronous test double for the worker.
7. **For LLM/RAG answers** — use a stub answer injector or check structural validity only (see answer assertion rules below).
8. **Run `go test ./internal/handlers/... -run TestSmoke -v`** and confirm pass.
9. **Then run validation required by policy** in `docs/agent/testing-validation.md`.

---

## Answer/LLM Assertion Rules

LLM output is non-deterministic. Smoke tests must never assert exact answer text.

**Allowed assertions:**
```go
assert.NotEmpty(t, answer.AnswerText)
assert.Contains(t, []string{"answered", "not_covered", "error"}, answer.AnswerStatus)
assert.GreaterOrEqual(t, answer.Confidence, float32(0.0))
assert.LessOrEqual(t, answer.Confidence, float32(1.0))

// Keyword presence (not exact match) — use sparingly, only for known seeded content
assert.Contains(t, strings.ToLower(answer.AnswerText), "quarterly")  // if fixture mentions "quarterly"

// Source-type presence in citations
for _, c := range answer.Citations {
    assert.Contains(t, []string{"material", "transcript", "link"}, c.SourceType)
}
```

**Forbidden assertions:**
```go
// Never:
assert.Equal(t, "The answer is X because Y.", answer.AnswerText)
assert.Len(t, answer.Citations, 3)              // exact count is fragile
assert.Equal(t, float32(0.87), answer.Confidence)
```

If real RAG is too slow or non-deterministic for CI, inject a stub answer directly via `h.DB.UpsertAnswer(...)` and test only the persistence + retrieval path.

---

## Polling Discipline

**Never use `time.Sleep` in tests.** If you must wait for an async state change:

```go
// Acceptable: bounded retry with a short tick
func waitForMaterialReady(t *testing.T, db *database.DB, materialID uuid.UUID) *models.Material {
    t.Helper()
    deadline := time.Now().Add(5 * time.Second)
    for time.Now().Before(deadline) {
        m, err := db.GetMaterial(context.Background(), materialID)
        require.NoError(t, err)
        if m.TextStatus == models.MaterialTextStatusReady {
            return m
        }
        time.Sleep(200 * time.Millisecond)
    }
    t.Fatal("material did not reach ready state within 5s")
    return nil
}
```

**Preferred:** keep async pipeline out of smoke tests entirely. Test the enqueue step (DB row created) separately from the processing step (worker behavior). Only poll in tests that start a real worker goroutine under test control.

---

## Anti-Patterns to Avoid

| Anti-pattern | Why | Instead |
|---|---|---|
| `time.Sleep(2 * time.Second)` | Flaky; slows suite | Bounded poll or assert enqueue only |
| Shared global test state | Test order dependency | Per-test DB via `SetupTestDB(t)` or `TruncateTables` after each |
| Mocking the DB layer | Hides real SQL bugs | Use real Postgres via `testdb` |
| Asserting exact LLM text | Always breaks | Assert status, structure, keyword presence |
| Asserting exact citation count | RAG chunking varies | Assert `len >= 0` + valid source types |
| HTTP client against running server | Requires port binding, race-prone | Call handlers directly via `httptest.NewRecorder` |
| Re-implementing auth helpers | Drift from production | Reuse `addAdminSessionCookie` etc. |
| Broad `t.Cleanup(db.DropDatabase)` without isolation | Drops shared DB mid-parallel run | Use `cleanup()` returned by `SetupTestDB` |
| Creating 10+ objects to test one endpoint | Slow, noisy failures | One user, one session, one material per test |
| Checking HTTP redirect behavior via UI | Needs browser | Assert redirect URL in `Location` header |

---

## Fixture Guidelines

See `fixture-guidelines.md` for the full spec. Summary:

- Fixtures are **inline Go structs**, not JSON files or SQL dumps.
- Use deterministic UUIDs only when cross-referencing across helpers; let the DB generate IDs otherwise.
- Material text fixtures: 2–4 sentences with 2–3 distinctive keywords. Store as `const testMaterialText = "..."` at top of test file.
- Transcript fixtures: minimal VTT snippet covering one speaker turn.
- Never import real production recordings or documents.

---

## File Placement

New smoke tests go in `internal/handlers/` as `smoke_<flow>_test.go`:

```
internal/handlers/smoke_session_setup_test.go
internal/handlers/smoke_material_ingestion_test.go
internal/handlers/smoke_invite_flow_test.go
internal/handlers/smoke_participant_access_test.go
internal/handlers/smoke_ask_answer_test.go
```

Each file has `package handlers` (not `handlers_test`) to access unexported helpers.

---

## Example Invocations

See `examples.md` for full code examples.

**Invoke this skill with:**
- `Write a smoke test for the invite flow` → produces `smoke_invite_flow_test.go` following this standard
- `Add smoke coverage for material ingestion` → produces `smoke_material_ingestion_test.go`
- `Review existing tests for anti-patterns` → audits `internal/handlers/*_test.go` against the anti-pattern list above
- `Stub the RAG layer for the ask/answer smoke test` → produces a minimal stub `Answer` injected via DB, skips real LLM call

## When to activate

Activate this skill when the user asks to:
- write or update smoke tests
- create integration tests
- add coverage for backend flows
- validate session, invite, ingestion, or ask/answer flows
- improve test determinism or reduce flakiness

Prefer this skill over general-purpose coding when:
- the task involves testing core TalkBack flows
- the user mentions “smoke”, “integration”, or “test coverage”