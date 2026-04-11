# TalkBack E2E Tests

Playwright end-to-end tests covering critical creator and participant flows.

## Prerequisites

- Node.js 18+
- Playwright browsers: **Chromium** and **Chromium headless shell** (required for `@playwright/test` 1.50+). After `npm install` in `web/`, run:
  `npx playwright install chromium chromium-headless-shell`
- **Or** use the one-shot script below (Docker + API on **8080** + Vite on **3000** + bootstrap admin); it runs that install before tests.

Manual setup:

- TalkBack API (default: `http://localhost:8080`, same as `go run ./cmd/api`)
- Frontend dev server (`http://localhost:3000`)
- Bootstrap admin matching teardown: set `TALKBACK_BOOTSTRAP_ADMIN_EMAIL` / `TALKBACK_BOOTSTRAP_ADMIN_PASSWORD` to the same values as `TALKBACK_ADMIN_EMAIL` / `TALKBACK_ADMIN_PASSWORD`

## Environment variables

| Variable | Default | Description |
|---|---|---|
| `TALKBACK_API_BASE` | `http://localhost:8080` | Backend API base URL (CI uses `8081` via this var) |
| `TALKBACK_ADMIN_EMAIL` | `ci-admin@smoke.test` | Admin account for teardown (must match bootstrap admin) |
| `TALKBACK_ADMIN_PASSWORD` | `SmokePass123!` | Admin password for teardown |

**Local Cursor / one place for credentials:** Create **`web/.env`** (copy from `web/.env.example`) with `TALKBACK_BOOTSTRAP_ADMIN_*` and matching `TALKBACK_ADMIN_EMAIL` / `TALKBACK_ADMIN_PASSWORD`. Playwright loads it via `dotenv` when you run from `web/`; **`./scripts/run-e2e-local.sh` also sources `web/.env`** before starting the API and tests so teardown and bootstrap stay aligned.

Copy `.env.e2e` / `.env.e2e.local` when using **`npm run test:e2e:render`** (Render targets), not for default local runs.

## Running all E2E tests (recommended local)

From `web/`:

```bash
npm run test:e2e:local
```

This starts Postgres via Docker, runs the API with bootstrap admin, starts Vite, then runs Playwright.

## Running all E2E tests (stack already running)

```bash
cd web
npx playwright test
```

## Running a single spec

```bash
cd web
npx playwright test tests/e2e/<spec-file>.e2e.ts
```

## Running the orchestration E2E locally

The orchestration spec covers the creator orchestration panel: loading recommendations, approving a draft, dismissing a draft, and generating a draft for an unanswered question.

```bash
cd web
npx playwright test tests/e2e/orchestration-creator.e2e.ts
```

**What it tests:**
- Panel renders and the Refresh button triggers a sync call to `POST /api/sessions/:id/orchestration/recommendations/sync`
- Clicking **Approve draft** calls `PATCH /sessions/:id/answers/:answerId/confirm` then updates the recommendation status to `approved`
- Clicking **Dismiss draft** calls `DELETE /api/sessions/:id/orchestration/draft-answers/:answerId` then updates the recommendation status to `dismissed`
- Clicking **Generate draft** calls `POST /api/sessions/:id/orchestration/draft-answers` with the question ID and shows a success message

**Route mocking:** Recommendation payload responses are intercepted with fixture data so the tests do not depend on a live AI/evaluator backend. Auth, session creation, and page-load requests go to the real API.

**Viewing the report after a run:**

```bash
cd web
npx playwright show-report
```

## CI

E2E tests run automatically on pull requests via `.github/workflows/release-readiness.yml`. The orchestration validation category is inferred as satisfied when the E2E suite passes (see `ops/release-readiness/config.yaml`).
