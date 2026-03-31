# TalkBack E2E Tests

Playwright end-to-end tests covering critical creator and participant flows.

## Prerequisites

- Node.js 18+
- A running TalkBack API (default: `http://localhost:8081`)
- A running TalkBack frontend (default: `http://localhost:3000`)
- An admin user bootstrapped in the database

## Environment variables

| Variable | Default | Description |
|---|---|---|
| `TALKBACK_API_BASE` | `http://localhost:8081` | Backend API base URL |
| `TALKBACK_ADMIN_EMAIL` | `paresh@suthar.com` | Admin account email for teardown |
| `TALKBACK_ADMIN_PASSWORD` | *(see fixtures.ts)* | Admin account password |

Copy `.env.e2e.local` (or set the vars in your shell) before running.

## Running all E2E tests

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
