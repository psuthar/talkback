# E2E Test Checklist

Use this checklist when writing or reviewing any Playwright test in TalkBack.

---

## Before Writing

- [ ] Read the component file(s) involved — know the real DOM structure before selecting anything
- [ ] Check `selector-guidelines.md` — confirm stable selectors exist for the elements under test
- [ ] If a required element has no stable selector: add `data-testid` to the component first
- [ ] Confirm the Playwright dev server config points to `http://localhost:3000`
- [ ] Decide: what is the minimal browser journey? Everything before that is API setup.

---

## Setup / Fixtures

- [ ] Users created via `request.post('/api/auth/signup', ...)` — not browser signup form
- [ ] Sessions created via `request.post('/sessions', ...)` — not browser UI
- [ ] Materials added via `request.post('/sessions/:id/materials/paste', ...)` — not file upload UI
- [ ] Invitations created via `request.post('/api/sessions/:id/invitations', ...)` — token extracted from response
- [ ] Auth cookie injected into browser context via `context.addCookies(...)` — not typed into login form
- [ ] Per-test unique email used (e.g. `e2e-${Date.now()}@smoke.test`) to avoid cross-test collision
- [ ] `storageState` saved when the same auth is reused across multiple tests in a file

---

## Navigation

- [ ] `page.goto('/?session=<id>&mode=view')` — never navigate session-by-session via UI clicks
- [ ] URL params used directly: `/?token=<token>` for invite, `/?mode=admin` for admin
- [ ] `page.waitForLoadState('networkidle')` after navigation if initial data fetch is needed
- [ ] First assertion is on a landmark element that proves the page loaded (not a spinner)

---

## Selectors

- [ ] `data-testid` used for every interactive element (input, button, status badge)
- [ ] Falling back to CSS class from `index.css` only when `data-testid` is not yet added
- [ ] No positional CSS selectors (`div:nth-child(3)`, `> div > span`)
- [ ] No selector that includes the exact LLM-generated answer text
- [ ] `getByRole` used for standard controls where ARIA role is unambiguous (button, textbox)
- [ ] `getByLabel` used when form fields have visible labels

---

## Waiting

- [ ] `waitForResponse` used when a button click triggers an API call
- [ ] `waitForSelector` / `locator.waitFor` used for async DOM state changes
- [ ] Answer terminal state waited with `timeout: 30_000` minimum
- [ ] No `page.waitForTimeout()` anywhere in test code
- [ ] Loading spinners checked to disappear before asserting content: `locator('.spinner').waitFor({ state: 'hidden' })`

---

## Assertions

- [ ] Answer assertion checks `.answer-status` class, not exact text
- [ ] `expect(badge).toHaveClass(/answered|not_covered/)` for LLM answer terminal state
- [ ] Keyword assertion (if used) limited to words from the seeded fixture text constant
- [ ] Citation assertion checks element exists and has expected structure, not exact count
- [ ] Access control: negative assertions (`not.toBeVisible()`) used for elements the user should not see
- [ ] All assertions use Playwright's `expect()` (auto-retry built in) — not manual `if` checks

---

## Invite / Participant Flow

- [ ] Invitation token extracted from API response, not scraped from email
- [ ] `/?token=<token>` navigated directly — not clicking a link in a notification
- [ ] Signup form: only fields visible to user are interacted with (email pre-filled by app, not typed)
- [ ] Redirect to session view confirmed after acceptance

---

## Voice Recording

- [ ] Voice recording tests **not included** in E2E suite — test transcription at API level via smoke-tests
- [ ] If a flow touches the voice mic button, skip or mock it

---

## Before Marking Done

- [ ] `npx playwright test --headed` passes locally with the dev stack running
- [ ] `npx playwright test` (headless) passes
- [ ] No `waitForTimeout` in new test code
- [ ] No exact LLM text assertions
- [ ] Test completes in under 60 seconds on local machine
- [ ] Test is isolated: runs independently with no dependency on other tests' order
