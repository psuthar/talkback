# Subagent and Test Routing

Source of truth: This file owns subagent routing policy and test-fixer routing guidance.

## Subagent Routing

- `talkback-architect`
  - multi-file backend/frontend/data-model design
  - migrations, API contract changes, rollout/backward-compatibility decisions
  - premise/decision model evolution

- `talkback-backend`
  - Go API handlers, DB access, auth/session, invitations, processing, RAG, storage
  - endpoint behavior fixes, backend business logic, backend test updates

- `talkback-frontend`
  - React/Vite changes in `web/`
  - client state, API wiring, UI behavior fixes

- `talkback-ux`
  - interaction/usability improvements
  - layout/content hierarchy changes while preserving existing visual language unless requested

- `talkback-reviewer`
  - regression/risk/missing-tests reviews

- `talkback-e2e-fixer`
  - run Playwright tests
  - diagnose/fix E2E failures, selectors, waits, setup issues, small UI bugs surfaced by tests

- `talkback-smoke-fixer`
  - run smoke/integration tests
  - diagnose/fix smoke failures and small backend issues surfaced by tests

## Test Routing

- Use `smoke-tests` skill to create/refine deterministic backend smoke/integration tests.
- Use `talkback-smoke-fixer` for smoke execution/failures/repairs.
- Use `e2e-tests` skill and `talkback-e2e-fixer` for browser E2E validation.
- Use `talkback-reviewer` for post-change risk and missing-test reviews.

