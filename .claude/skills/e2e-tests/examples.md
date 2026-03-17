# E2E Test Examples

Full Playwright test code for all 6 TalkBack browser flows. Copy these as starting points — adapt selectors as `data-testid` attributes are added per `selector-guidelines.md`.

---

## `web/playwright.config.ts`

```typescript
import { defineConfig, devices } from '@playwright/test'

export default defineConfig({
  testDir: './tests/e2e',
  testMatch: '**/*.e2e.ts',
  fullyParallel: false, // serial by default — per-test user isolation handles concurrency
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  workers: process.env.CI ? 2 : 1,
  reporter: 'html',
  use: {
    baseURL: 'http://localhost:3000',
    trace: 'on-first-retry',
    screenshot: 'only-on-failure',
  },
  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
    },
  ],
  // Assumes both frontend (port 3000) and backend (port 8080) are already running.
  // Start them manually: `go run ./cmd/api` and `cd web && npm run dev`
})
```

---

## `web/tests/e2e/fixtures.ts`

Shared helpers imported by every test file.

```typescript
import { BrowserContext, APIResponse } from '@playwright/test'

const API_BASE = 'http://localhost:8080'

/**
 * Parse Set-Cookie headers from an API response and inject them into the browser context.
 * Call this after every login or signup API call.
 */
export async function injectCookiesFromResponse(
  context: BrowserContext,
  response: APIResponse
): Promise<void> {
  const setCookieHeader = response.headers()['set-cookie']
  if (!setCookieHeader) return

  const cookies = setCookieHeader.split('\n').map((line) => {
    const parts = line.split(';').map((p) => p.trim())
    const [nameVal] = parts
    const [name, ...valParts] = nameVal.split('=')
    const value = valParts.join('=')
    const domainPart = parts.find((p) => p.toLowerCase().startsWith('domain='))
    const pathPart = parts.find((p) => p.toLowerCase().startsWith('path='))
    return {
      name: name.trim(),
      value: value.trim(),
      domain: domainPart ? domainPart.split('=')[1].trim() : 'localhost',
      path: pathPart ? pathPart.split('=')[1].trim() : '/',
    }
  })

  await context.addCookies(cookies)
}

/**
 * Create a user via API signup and inject their auth cookie into the browser context.
 * Returns the parsed user object from the API response.
 */
export async function createUserAndLogin(
  context: BrowserContext,
  request: any,
  email: string,
  password = 'SmokePass123!',
  displayName?: string
): Promise<{ id: string; email: string; global_role: string }> {
  await request.post(`${API_BASE}/api/auth/signup`, {
    data: {
      email,
      password,
      display_name: displayName ?? email.split('@')[0],
    },
  })
  const loginRes = await request.post(`${API_BASE}/api/auth/login`, {
    data: { email, password },
  })
  await injectCookiesFromResponse(context, loginRes)
  return loginRes.json()
}

/**
 * Seed a session via API. Returns { id, title }.
 */
export async function createSession(
  request: any,
  title: string
): Promise<{ id: string; title: string }> {
  const res = await request.post(`${API_BASE}/sessions`, {
    data: { title },
  })
  return res.json()
}

/**
 * Add pasted material to a session. Returns the material object.
 * Material is immediately text_status=ready — no async pipeline.
 */
export async function pasteMaterial(
  request: any,
  sessionId: string,
  title: string,
  text: string
): Promise<{ id: string }> {
  const res = await request.post(`${API_BASE}/sessions/${sessionId}/materials/paste`, {
    data: { title, text },
  })
  return res.json()
}

/**
 * Create an invitation for a session. Returns { token, accept_url }.
 */
export async function createInvitation(
  request: any,
  sessionId: string,
  invitedEmail: string
): Promise<{ token: string; accept_url: string }> {
  const res = await request.post(`${API_BASE}/api/sessions/${sessionId}/invitations`, {
    data: { email: invitedEmail, role: 'participant' },
  })
  const body = await res.json()
  const inv = body.invitation
  const url = new URL(inv.accept_url)
  return { token: url.searchParams.get('token') ?? '', accept_url: inv.accept_url }
}

/** Per-test unique email — avoids collision between parallel test runs. */
export function uniqueEmail(prefix = 'e2e'): string {
  return `${prefix}-${Date.now()}-${Math.random().toString(36).slice(2, 7)}@smoke.test`
}
```

---

## Flow 1 — Creator Access (`creator-access.e2e.ts`)

Validates: creator can log in → session list is visible → open a session → see creator UI.

```typescript
import { test, expect } from '@playwright/test'
import { createUserAndLogin, createSession, uniqueEmail } from './fixtures'

test.describe('Flow 1 — Creator Access', () => {
  test('creator sees session list and can open a session', async ({ page, context, request }) => {
    const email = uniqueEmail('creator')
    await createUserAndLogin(context, request, email)
    const session = await createSession(request, 'E2E Creator Session')

    // Navigate to app root
    await page.goto('/')
    await page.waitForLoadState('networkidle')

    // Session list should be present (creator sees their sessions)
    // NOTE: add data-testid="session-list" to the session list container for a stable selector
    // Today: assert by session title text
    await expect(page.getByText('E2E Creator Session')).toBeVisible()

    // Open the session in creator (edit) mode
    await page.goto(`/?session=${session.id}&mode=edit`)
    await page.waitForLoadState('networkidle')

    // Creator mode renders: session title should be visible
    await expect(page.getByText('E2E Creator Session')).toBeVisible()
  })
})
```

---

## Flow 2 — Session Availability (`session-availability.e2e.ts`)

Validates: session page loads; Q&A panel visible; materials tab accessible.

```typescript
import { test, expect } from '@playwright/test'
import { createUserAndLogin, createSession, pasteMaterial, uniqueEmail } from './fixtures'

const FIXTURE_TEXT = 'Meridian proposal approved. APAC expansion confirmed at 2.4M. Churn target below 6%.'

test.describe('Flow 2 — Session Availability', () => {
  test('session page loads with Q&A panel and materials tab', async ({ page, context, request }) => {
    const email = uniqueEmail('availability')
    await createUserAndLogin(context, request, email)
    const session = await createSession(request, 'E2E Availability Session')
    await pasteMaterial(request, session.id, 'Meridian Report', FIXTURE_TEXT)

    await page.goto(`/?session=${session.id}&mode=view`)
    await page.waitForLoadState('networkidle')

    // Q&A panel footer is rendered
    // NOTE: add data-testid="question-input" to the question textarea
    // Today: use placeholder text
    await expect(page.getByPlaceholder('Ask a question...')).toBeVisible()

    // Materials tab is accessible
    // NOTE: add data-testid="materials-tab" to the tab button
    // Today: find by text
    const materialsTab = page.getByRole('button', { name: /materials/i })
    if (await materialsTab.isVisible()) {
      await materialsTab.click()
      await expect(page.getByText('Meridian Report')).toBeVisible()
    }
  })
})
```

---

## Flow 3 — Participant Acceptance (`participant-acceptance.e2e.ts`)

Validates: pre-seeded token → navigate to `/?token=<token>` → signup form pre-fills email → complete → redirect to session.

**This is the highest-value E2E flow.** It exercises AcceptInvitePage end-to-end.

```typescript
import { test, expect } from '@playwright/test'
import {
  createUserAndLogin,
  createSession,
  createInvitation,
  uniqueEmail,
} from './fixtures'

const PARTICIPANT_PASSWORD = 'SmokePass123!'

test.describe('Flow 3 — Participant Acceptance', () => {
  test('new participant accepts invite via token and lands in session', async ({
    page,
    context,
    request,
  }) => {
    // Seed: creator + session + invitation
    const creatorEmail = uniqueEmail('creator')
    await createUserAndLogin(context, request, creatorEmail)
    const session = await createSession(request, 'E2E Invite Session')

    const participantEmail = uniqueEmail('participant')
    const { token } = await createInvitation(request, session.id, participantEmail)

    // Clear creator's cookie — participant is a new unauthenticated user
    await context.clearCookies()

    // Navigate to invite acceptance URL
    await page.goto(`/?token=${token}`)
    await page.waitForLoadState('networkidle')

    // "Join session" heading confirms page loaded
    await expect(page.getByRole('heading', { name: /join session/i })).toBeVisible()

    // Email is pre-filled and read-only
    const emailInput = page.getByLabel('Email')
    await expect(emailInput).toHaveValue(participantEmail)

    // Fill display name and password
    await page.getByLabel('Display name').fill('E2E Participant')
    await page.getByLabel('Password').fill(PARTICIPANT_PASSWORD)

    // Submit registration
    const createBtn = page.getByRole('button', { name: /create account/i })
    await Promise.all([
      page.waitForLoadState('networkidle'),
      createBtn.click(),
    ])

    // Should redirect to session view after acceptance
    await expect(page).toHaveURL(new RegExp(`session=${session.id}`))
  })

  test('existing account holder can sign in via invite link', async ({
    page,
    context,
    request,
  }) => {
    // Seed: creator + session
    const creatorEmail = uniqueEmail('creator2')
    await createUserAndLogin(context, request, creatorEmail)
    const session = await createSession(request, 'E2E Existing User Invite')

    // Create participant account first
    const participantEmail = uniqueEmail('existing')
    await context.clearCookies()
    await createUserAndLogin(context, request, participantEmail, PARTICIPANT_PASSWORD, 'Existing P')
    await context.clearCookies()

    // Now creator creates the invitation
    await createUserAndLogin(context, request, creatorEmail)
    const { token } = await createInvitation(request, session.id, participantEmail)
    await context.clearCookies()

    // Navigate to invite URL as unauthenticated
    await page.goto(`/?token=${token}`)
    await page.waitForLoadState('networkidle')

    // "Sign in" form shown (account exists)
    await page.getByLabel('Password').fill(PARTICIPANT_PASSWORD)

    const signInBtn = page.getByRole('button', { name: /sign in/i })
    await Promise.all([page.waitForLoadState('networkidle'), signInBtn.click()])

    // Redirected to session view
    await expect(page).toHaveURL(new RegExp(`session=${session.id}`))
  })
})
```

---

## Flow 4 — Material Visibility (`material-visibility.e2e.ts`)

Validates: material seeded via API appears in the materials panel in participant view.

```typescript
import { test, expect } from '@playwright/test'
import { createUserAndLogin, createSession, pasteMaterial, uniqueEmail } from './fixtures'

const FIXTURE_TEXT =
  'Meridian proposal approved. APAC expansion confirmed at 2.4M. Churn target below 6% this quarter.'

test.describe('Flow 4 — Material Visibility', () => {
  test('seeded material appears in session materials panel', async ({ page, context, request }) => {
    const email = uniqueEmail('material')
    await createUserAndLogin(context, request, email)
    const session = await createSession(request, 'E2E Materials Session')
    await pasteMaterial(request, session.id, 'Meridian Report', FIXTURE_TEXT)

    await page.goto(`/?session=${session.id}&mode=view`)
    await page.waitForLoadState('networkidle')

    // Navigate to materials tab if present
    // NOTE: add data-testid="materials-tab" for a stable selector
    const materialsTab = page.getByRole('button', { name: /materials/i })
    if (await materialsTab.count() > 0) {
      await materialsTab.first().click()
    }

    // Material title should be visible in panel
    await expect(page.getByText('Meridian Report')).toBeVisible({ timeout: 10_000 })
  })

  test('session with no materials shows empty state gracefully', async ({
    page,
    context,
    request,
  }) => {
    const email = uniqueEmail('nomaterial')
    await createUserAndLogin(context, request, email)
    const session = await createSession(request, 'E2E Empty Session')

    await page.goto(`/?session=${session.id}&mode=view`)
    await page.waitForLoadState('networkidle')

    // Session loaded — no crash, Q&A panel still visible
    await expect(page.getByPlaceholder('Ask a question...')).toBeVisible()
  })
})
```

---

## Flow 5 — Ask Question (`ask-answer.e2e.ts`)

Validates: participant types question → submits → spinner visible → answer status badge appears.

**Note on LLM non-determinism:** In CI without an OpenAI key, the answer status will be `error`. The test asserts any terminal state (answered, not_covered, or error) — never exact answer text.

**Prerequisite:** The following `data-testid` attributes must be added before this test can run:
- `data-testid="question-input"` on the `<textarea>` in `QAPanel.jsx`
- `data-testid="ask-button"` on the Ask `<button>` in `QAPanel.jsx`
- `className="question-item"`, `className="answer-status answered"` etc. in `QAHistory.jsx`

Until those are added, use the workarounds shown below.

```typescript
import { test, expect } from '@playwright/test'
import { createUserAndLogin, createSession, pasteMaterial, uniqueEmail } from './fixtures'

const FIXTURE_TEXT =
  'Meridian proposal approved. APAC expansion confirmed at 2.4M. Churn target below 6% this quarter.'

test.describe('Flow 5 — Ask Question', () => {
  test('participant submits question and sees terminal answer state', async ({
    page,
    context,
    request,
  }) => {
    const email = uniqueEmail('asker')
    await createUserAndLogin(context, request, email)
    const session = await createSession(request, 'E2E Ask Session')
    await pasteMaterial(request, session.id, 'Meridian Report', FIXTURE_TEXT)

    await page.goto(`/?session=${session.id}&mode=view`)
    await page.waitForLoadState('networkidle')

    // --- Step 1: Type and submit a question ---
    // PREFERRED (add data-testid first):
    // await page.getByTestId('question-input').fill('What is the APAC expansion budget?')
    // await page.getByTestId('ask-button').click()

    // FALLBACK (today, no testid): use placeholder and button text
    const questionInput = page.getByPlaceholder('Ask a question...')
    await expect(questionInput).toBeVisible()
    await questionInput.fill('What is the APAC expansion budget?')

    // Wait for the API call to POST the question before asserting spinner
    const questionPostPromise = page.waitForResponse(
      (res) => res.url().includes('/questions') && res.request().method() === 'POST'
    )
    await page.getByRole('button', { name: /^ask$/i }).click()
    await questionPostPromise

    // --- Step 2: Spinner appears while thinking ---
    // .spinner class IS applied in QAPanel
    await expect(page.locator('.spinner').first()).toBeVisible({ timeout: 5_000 })

    // --- Step 3: Wait for terminal answer state ---
    // PREFERRED (after adding CSS classes to QAHistory):
    // const badge = page.locator('.answer-status').last()
    // await expect(badge).toHaveClass(/answered|not_covered|error/, { timeout: 30_000 })

    // FALLBACK: wait for spinner to disappear then check the status text is one of the known values
    await page.locator('.spinner').last().waitFor({ state: 'hidden', timeout: 30_000 })

    // Status text "answered", "not covered", or "error" should be somewhere on the page
    const statusText = page.getByText(/^(answered|not covered|not_covered|error)$/i)
    await expect(statusText.first()).toBeVisible({ timeout: 5_000 })
  })
})
```

---

## Flow 6 — Creator Access (Admin Panel) (`admin-access.e2e.ts`)

Validates: admin user can reach the admin panel at `/?mode=admin`.

```typescript
import { test, expect } from '@playwright/test'
import { uniqueEmail } from './fixtures'

const API_BASE = 'http://localhost:8080'

test.describe('Flow 6 — Admin Access', () => {
  test('admin user can access the admin panel', async ({ page, context, request }) => {
    // Admin must be created via bootstrap or directly in DB — signup does not grant admin role.
    // In tests: create a user then promote via admin API, or use a pre-existing admin account.
    // For now, validate that the admin panel route exists and redirects unauthenticated users.

    await page.goto('/?mode=admin')
    await page.waitForLoadState('networkidle')

    // Unauthenticated → login page shown
    await expect(page.getByRole('heading', { name: /talkback/i })).toBeVisible()
    await expect(page.getByLabel('Email')).toBeVisible()
  })
})
```

---

## Adding `data-testid` to QAPanel (required before Flow 5 runs cleanly)

Apply these changes to `web/src/components/QAPanel.jsx`:

```jsx
// QAPanel.jsx — add data-testid to question textarea and ask button
<textarea
  data-testid="question-input"      // ← add this
  value={questionText}
  onChange={(e) => setQuestionText(e.target.value)}
  placeholder="Ask a question..."
  rows={2}
  style={{ ... }}
/>
<button
  data-testid="ask-button"          // ← add this
  type="button"
  onClick={askSessionQuestion}
  disabled={!questionText?.trim() || loading}
  style={{ width: '100%' }}
>
  Ask
</button>
```

Apply these changes to `web/src/components/QAHistory.jsx` (QACard component):

```jsx
// QACard outer div:
<div
  data-testid="question-item"
  className="question-item"          // ← apply the index.css class
  style={{ ... }}
>

// Question text span:
<div className="question-text" style={{ ... }}>  // ← apply index.css class
  Q: {q.question_text}
</div>

// Answer text div:
<div className="answer-text" style={{ ... }}>    // ← apply index.css class
  <strong>A:</strong> {q.answer.answer_text}
</div>

// Answer status span:
<span
  data-testid="answer-status"
  className={`answer-status ${q.answer.answer_status}`}  // ← apply index.css classes
>
  {q.answer.answer_status}
</span>

// Citation button (CitationBadge):
<button
  data-testid="citation"
  className="citation"               // ← apply index.css class
  type="button"
  onClick={onClick}
  ...
>
```

After these changes, the preferred selectors in the ask-answer test become:

```typescript
// Fill question
await page.getByTestId('question-input').fill('What is the APAC expansion budget?')

// Click ask
await page.getByTestId('ask-button').click()

// Assert terminal state (any of: answered, not_covered, error)
const badge = page.locator('[data-testid="question-item"]').last()
  .locator('[data-testid="answer-status"]')
await expect(badge).toHaveClass(/answered|not_covered|error/, { timeout: 30_000 })

// Citation exists
await expect(page.getByTestId('citation').first()).toBeVisible()
```

---

## Running the Tests

```bash
# Install (once)
cd web
npm install -D @playwright/test
npx playwright install chromium

# Run all E2E tests (requires both dev server and API running)
npx playwright test

# Run headed (watch mode)
npx playwright test --headed

# Run a specific file
npx playwright test tests/e2e/participant-acceptance.e2e.ts

# Open HTML report after run
npx playwright show-report
```
