# Skill: e2e-tests

Policy source: `docs/agent/testing-validation.md`.
This skill focuses on browser E2E tactics and Playwright execution details, not global test policy ownership.

## Purpose

Write and refine browser-based end-to-end tests for TalkBack's core user journeys. Tests run a real browser against a real running stack (frontend + backend + Postgres) and validate what the user actually sees and can do.

**This is the browser-first sibling of `smoke-tests`.** The difference is the test surface:

| | `smoke-tests` | `e2e-tests` |
|---|---|---|
| Layer | HTTP handler / service | Real browser, real DOM |
| Confidence | Service correctness | User-visible workflow |
| Setup speed | Fast (direct DB) | Slower (browser boot) |
| Default for | Logic, access control, API shape | Journey completion, selector stability, UX flow |
| When to reach for | Anything testable at the API boundary | Things only visible in a real browser: renders, scroll-into-view, focus, UI feedback |

**Use browser tests for the flows where the question is "did the user see the right thing and could they take the next action?" — not for "did the API return the right status code?"**

---

## Framework: Playwright

Playwright is the chosen framework. It is **not yet installed** — run once to bootstrap:

```bash
cd web
npm install -D @playwright/test
npx playwright install chromium
```

Add to `web/package.json` scripts:

```json
"test:e2e": "playwright test",
"test:e2e:ui": "playwright test --ui",
"test:e2e:headed": "playwright test --headed"
```

Config file: `web/playwright.config.ts` (see `examples.md`).

**Why Playwright over Cypress:**
- Works with Vite dev server out of the box
- Native network interception (`page.route`, `waitForResponse`)
- API request context for backend seeding without a browser
- No iframe restrictions for the Loom embeds TalkBack uses

---

## Repo Realities (read before writing any test)

### Routing

TalkBack uses **URL query parameters, not path-based routing** (no React Router). All navigation is via:

| Intent | URL |
|---|---|
| App root / login | `http://localhost:3000/` |
| Accept invite | `/?token=<invite-token>` |
| Participant session view | `/?session=<id>&mode=view` |
| Creator session edit | `/?session=<id>&mode=edit` |
| Admin panel | `/?mode=admin` |

Always `page.goto(url)` — do not use `page.click` on links to navigate between sessions.

### Auth

Cookie-based (`tb_session` HTTP-only cookie). **Do not type credentials in the browser for setup.** Instead:

```typescript
// In beforeEach / test.beforeAll: seed auth via API request context
const res = await request.post('/api/auth/login', {
  data: { email: 'user@smoke.test', password: 'SmokePass123!' },
});
const cookies = res.headers()['set-cookie'];
await context.addCookies(parseCookies(cookies));
```

Or use Playwright's `storageState` to save/restore auth state across tests.

### CSS Classes (the selectors you have today)

No `data-testid` exists anywhere. Until they are added, use these stable CSS classes from `index.css`:

| Selector | What it targets |
|---|---|
| `.question-item` | A single Q&A pair in history |
| `.question-text` | The question text inside a `.question-item` |
| `.answer-text` | The answer text inside a `.question-item` |
| `.answer-status` | The status badge (contains text "answered", "not covered", "error") |
| `.answer-status.answered` | Specifically the green "answered" badge |
| `.answer-status.not_covered` | The grey "not covered" badge |
| `.citation` | A citation chip |
| `.spinner` | Loading indicator |

**These are the only reliable selectors until `data-testid` attributes are added.** The `selector-guidelines.md` tracks which elements need instrumentation and what attribute names to use.

### LLM Answer Assertions

Answers are non-deterministic. **Never assert exact answer text.**

```typescript
// ✅ Assert status badge class — deterministic
await expect(page.locator('.answer-status.answered').first()).toBeVisible();

// ✅ Assert one of two valid terminal states
const badge = page.locator('.question-item').last().locator('.answer-status');
await expect(badge).toHaveClass(/answered|not_covered/);

// ✅ Keyword present in answer (only for seeded fixture content)
const answerText = await page.locator('.answer-text').last().textContent();
expect(answerText?.toLowerCase()).toMatch(/meridian|apac|churn/);

// ❌ Never:
await expect(page.locator('.answer-text')).toHaveText('Revenue increased by 12%...');
```

### Async Answer Waiting

Answers arrive asynchronously. Use `waitFor` with a generous timeout — not `sleep`.

```typescript
// Wait for any terminal status badge to appear
await page.locator('.answer-status').last()
  .waitFor({ state: 'visible', timeout: 30_000 });
```

In tests without a real OpenAI key (CI), `answer-status.error` is also a valid terminal state. Assert "not error" only when OpenAI is available.

---

## Backend Seeding (Critical Pattern)

**Never use the browser UI to create sessions, users, or upload materials as part of test setup.** Use Playwright's `APIRequestContext` to call the backend directly:

```typescript
test.beforeEach(async ({ request, context }) => {
  // 1. Create user and log in via API
  await request.post('/api/auth/signup', {
    data: { email: 'e2e@smoke.test', password: 'SmokePass123!', display_name: 'E2E User' },
  });
  const login = await request.post('/api/auth/login', {
    data: { email: 'e2e@smoke.test', password: 'SmokePass123!' },
  });
  // 2. Inject cookie into browser context
  await injectCookiesFromResponse(context, login);

  // 3. Create a session via API
  const sessionRes = await request.post('/sessions', {
    data: { title: 'E2E Session' },
  });
  const session = await sessionRes.json();

  // 4. Paste material via API (no file upload, no async pipeline)
  await request.post(`/sessions/${session.id}/materials/paste`, {
    data: { title: 'Meridian Report', text: 'Meridian proposal approved. APAC expansion at 2.4M. Churn target below 6%.' },
  });

  // 5. Navigate browser directly to the seeded session
  await page.goto(`/?session=${session.id}&mode=view`);
});
```

This approach:
- Avoids flaky UI-based session creation
- Avoids waiting for async file ingestion pipeline
- Keeps browser tests focused on the user journey, not setup steps

---

## E2E Test Standard for TalkBack

A browser test in this repo is a Playwright test (`*.e2e.ts`) that:

1. **Validates a complete user-visible journey** from URL navigation to final visible state.
2. **Seeds fixtures via API**, not via browser UI, for anything that isn't the flow under test.
3. **Uses stable selectors**: `data-testid` first, CSS class second, text/role last.
4. **Completes in under 60 seconds** on a local machine with the dev stack running.
5. **Asserts terminal state** — not intermediate loading states.
6. **Tolerates LLM non-determinism** — asserts status class, never exact text.

---

## Covered Flows

### Flow 1 — Creator Access
Navigate as creator → session list visible → open session → see creator UI.

### Flow 2 — Session Availability
Session page loads: video player area visible, materials tab present, Q&A panel visible.

### Flow 3 — Invite Flow
Creator invites via UI → email link shown → copy invite link → open in new tab → resolve → signup form → complete.

### Flow 4 — Participant Acceptance
Pre-seeded invitation token → navigate to `/?token=<token>` → signup form → complete → redirected to session view.

### Flow 5 — Material Visibility
Session has seeded material → participant navigates → material appears in panel → clicking opens document viewer.

### Flow 6 — Ask Question
Participant types question → submits → loading state visible → answer status badge appears (answered or not_covered).

---

## Workflow (apply this every time)

1. **Read the component first** — open the relevant `.jsx` file from `web/src/` before writing any test. Know the actual DOM before selecting it.
2. **Check `selector-guidelines.md`** — confirm which selectors are stable today and which need `data-testid` added.
3. **Add `data-testid` attributes before writing the test** — if the target element lacks one, add it first and test-drive the selector.
4. **Seed via API** — create users, sessions, and materials via `request` fixture, not via browser UI.
5. **Use `waitForResponse` for async state changes** — when clicking "Ask", wait for the answer API response, not a DOM timeout.
6. **Assert terminal state** — for answers, wait until `.answer-status` is visible and has a terminal class.
7. **Run `npx playwright test --headed` locally** and confirm pass.
8. **Then run validation required by policy** in `docs/agent/testing-validation.md`.

---

## Anti-Patterns to Avoid

| Anti-pattern | Why | Instead |
|---|---|---|
| `await page.waitForTimeout(5000)` | Flaky; wrong on slow CI | `waitForResponse`, `waitForSelector`, `waitFor` with condition |
| `page.locator('div > div > div:nth-child(3)')` | Breaks on any layout change | `data-testid` or CSS class from `index.css` |
| Asserting exact LLM answer text | Always breaks | Assert `.answer-status.answered` class |
| Creating sessions via browser clicks in `beforeEach` | Flaky, slow | Seed via `request.post(...)` API |
| Running tests against production / shared env | Data pollution | Local stack only; per-test user email to avoid collisions |
| `page.locator('text=Ask')` when button has no text fallback | Brittle if copy changes | `data-testid="ask-button"` or `role=button, name=/ask/i` |
| Testing voice recording end-to-end | Requires microphone access in browser | Cover voice transcription at the API level only |
| Checking pixel positions or layout | CSS-dependent, fragile | Assert element visibility and logical containment only |
| One global shared login for all tests in parallel | Auth state collision | Per-test user email (e.g. `e2e-${test.info().testId}@smoke.test`) |

---

## File Placement

New E2E tests go in `web/tests/e2e/` as `<flow>.e2e.ts`:

```
web/
├── playwright.config.ts
└── tests/
    └── e2e/
        ├── fixtures.ts          # shared APIRequestContext helpers, cookie utils
        ├── creator-access.e2e.ts
        ├── session-availability.e2e.ts
        ├── invite-flow.e2e.ts
        ├── participant-acceptance.e2e.ts
        ├── material-visibility.e2e.ts
        └── ask-answer.e2e.ts
```

---

## Example Invocations

See `examples.md` for full test code.

**Invoke this skill with:**
- `Write an E2E test for the ask-question flow` → produces `ask-answer.e2e.ts`
- `Add E2E coverage for invite acceptance` → produces `participant-acceptance.e2e.ts`
- `Add data-testid to the QA panel` → instruments `web/src/components/QAPanel.jsx`
- `Review E2E tests for flaky patterns` → audits against anti-patterns above
- `Set up Playwright for TalkBack` → produces `playwright.config.ts` + installs deps

## When to activate

Activate this skill when the user asks to:
- write or update E2E or browser tests
- add Playwright coverage for a user journey
- instrument components with data-testid attributes
- validate a flow "from the user's perspective" or "in the browser"
- reduce flakiness in existing browser tests

Prefer this skill over `smoke-tests` when:
- the question is "can the user do X in the UI"
- the test requires a real browser render (forms, navigation, visibility)
- the user mentions "Playwright", "browser test", "E2E", or "user journey"
