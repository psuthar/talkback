# TalkBack Testing and Validation Policy

Source of truth: This file owns test planning, required test coverage rules, and validation gates.

## Test Planning (Before Implementation)

Before writing implementation code:

1. Determine whether the ticket changes executable behavior or is docs/config only.
2. For product-code changes, identify:
   - new behavior that needs tests,
   - existing tests that must be updated,
   - security/correctness-sensitive paths,
   - appropriate test type matching existing repository patterns.
3. Record a brief test plan and treat it as acceptance criteria for done.

## Required Test Type Matrix

| Changed area | Required test type |
|---|---|
| Any file outside `web/` (Go code) | At minimum one `*_test.go` file in the diff |
| New API handler or changed handler behavior | Handler test via `setupTestHandlersParallel` |
| New DB query or schema change | DB integration test via `internal/test/testdb` |
| MCP tool added or changed | MCP behavioral test |
| `web/` logic change (state/API/conditional rendering) | E2E spec added or updated in `web/tests/` |
| `web/` style-only change | No test required; use `Style-only:` commit convention |
| Docs/config only, no executable behavior | No test required |

## Hard Stops Before Commit

- If any file outside `web/` changed, at least one `*_test.go` must be present in diff.
- If `web/` logic changed (not style-only), at least one E2E spec must be added/updated.
- Run:

`go run ./cmd/prrisk --repo-root . --base-ref origin/main --output-dir /tmp/prrisk-check`

If `tests_missing` appears and change is not style-only, do not commit until tests are added.

## Validation Before Completion

- Run relevant validation before completion:
  - `go test ./...`
  - affected integration/E2E flows
- Do not proceed when validation fails.

`go test ./...` partial failure rule:

- If only DB-backed packages fail due to missing `DATABASE_URL`/`TEST_DATABASE_URL` and none were touched, document exact failures as acceptable known skip in Jira completion comment.
- If any touched package fails for any reason, this is a hard blocker.

