# Smoke Test Checklist

Use this checklist when writing or reviewing any smoke test in TalkBack.

---

## Before Writing

- [ ] Read the existing test file for the handler package (`internal/handlers/*_test.go`)
- [ ] Trace the route under test in `cmd/api/main.go` → `internal/handlers/`
- [ ] Confirm the relevant DB method in `internal/database/`
- [ ] Identify which auth role the flow requires (admin / creator / participant)
- [ ] Decide: per-test isolated DB (`setupTestHandlersParallel`) or shared DB + truncate?

**Rule:** use `setupTestHandlersParallel` for handler-level smoke tests. Only use shared DB + `TruncateTables` for pure DB-layer tests.

---

## Test Structure

- [ ] File named `smoke_<flow>_test.go` in `internal/handlers/`
- [ ] Package declaration: `package handlers` (not `handlers_test`)
- [ ] Test function named `TestSmoke_<Flow>_<Scenario>` (e.g. `TestSmoke_InviteFlow_AcceptCreatesParticipant`)
- [ ] `t.Parallel()` at the top of every test
- [ ] `h, cleanup := setupTestHandlersParallel(t); defer cleanup()`
- [ ] No global variables mutated by the test

---

## Fixture Seeding

- [ ] Seed only what is needed: one user per role, one session, one material max
- [ ] Auth cookies attached via `addAdminSessionCookie` / `addUserSessionCookie` / `addParticipantSessionCookie`
- [ ] No hardcoded UUIDs unless cross-referencing across helpers
- [ ] Material text is a short constant with 2–3 distinctive keywords
- [ ] No real recordings, PDFs, or production data

---

## HTTP Calls

- [ ] Use `httptest.NewRecorder()` — never bind to a real port
- [ ] Call handler directly: `h.RequireAuth(h.HandleFoo)(w, req)` or via router if route matching is under test
- [ ] Assert `w.Code` first before unmarshaling body
- [ ] Unmarshal into a typed struct, not `map[string]interface{}`
- [ ] Assert required response fields (IDs, status fields, role fields)

---

## Async / Pipeline Steps

- [ ] Is the step synchronous or does it enqueue a job?
- [ ] If async: assert job row exists in DB, not that it completed
- [ ] If polling is unavoidable: use bounded retry helper (max 5s, 200ms tick), never bare `time.Sleep`
- [ ] Worker goroutines started in tests must be stopped before test returns

---

## LLM / RAG Assertions

- [ ] Never assert exact `AnswerText` string
- [ ] Assert `AnswerStatus` is one of `{"answered", "not_covered", "error"}`
- [ ] Assert `Confidence` is in `[0.0, 1.0]`
- [ ] Keyword assertions only for terms known to be in seeded fixture text
- [ ] Citation `SourceType` values are within `{"material", "transcript", "link"}`
- [ ] If real RAG is too slow for CI: inject stub answer via `h.DB.UpsertAnswer(...)` and skip RAG call

---

## Access Control

- [ ] Participant-only endpoints: verify creator/admin gets 403 or endpoint is not exposed
- [ ] Creator-only endpoints: verify participant gets 401/403
- [ ] Unauthenticated requests: verify 401 for all protected endpoints

---

## Cleanup

- [ ] `defer cleanup()` present and called unconditionally
- [ ] No leftover goroutines after test returns
- [ ] No file system artifacts written outside of `t.TempDir()`

---

## Before Marking Done

- [ ] `go test ./internal/handlers/... -run TestSmoke -v` passes
- [ ] `go test ./...` passes (no regressions)
- [ ] Test runs in under 10 seconds on local machine
- [ ] No `time.Sleep` in new test code
- [ ] No exact LLM text assertions
