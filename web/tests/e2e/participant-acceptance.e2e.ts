import { test, expect } from '@playwright/test'
import { API_BASE, createSession, createInvitation, uniqueEmail } from './fixtures'

// Page load + invite resolve + registration — no LLM call needed, 30 s is generous.
test.setTimeout(30_000)

test(
  'new participant accepts invite via token link, signs up, and lands on session',
  async ({ page, request }) => {
    // --- Seed: creator logs in via API then creates session + invitation ---
    // Note: cookies for the creator are stored in the request fixture's own cookie jar
    // and are NOT injected into the browser — the browser stays unauthenticated throughout
    // seeding so the invite form renders for an anonymous visitor.
    const creatorEmail = uniqueEmail('invite-creator')
    await request.post(`${API_BASE}/api/auth/signup`, {
      data: { email: creatorEmail, password: 'SmokePass123!', display_name: 'Invite Creator' },
    })
    await request.post(`${API_BASE}/api/auth/login`, {
      data: { email: creatorEmail, password: 'SmokePass123!' },
    })

    const session = await createSession(request, 'Invite Acceptance E2E Session')

    const participantEmail = uniqueEmail('invite-participant')
    const invitation = await createInvitation(request, session.id, participantEmail)

    // accept_url is a full URL like "http://localhost:3000/accept-invite?token=XXX".
    // Extract just the token so we can build the correct path relative to the baseURL.
    const qs = invitation.accept_url.includes('?') ? invitation.accept_url.split('?')[1] : ''
    const token = new URLSearchParams(qs).get('token')
    expect(token).toBeTruthy()

    // --- Navigate unauthenticated browser to the invite link ---
    await page.goto(`/accept-invite?token=${token}`)

    // --- Signup form must appear (invited email has no account yet) ---
    // The form renders after the resolve API call resolves — 10 s covers any slow CI startup.
    const nameInput = page.getByLabel('Display name')
    await expect(nameInput).toBeVisible({ timeout: 10_000 })

    // Email is pre-filled (read-only) by the component — just fill name and password.
    await nameInput.fill('New Participant')
    await page.getByLabel('Password').fill('SmokePass123!')

    // --- Submit: listen for the register-and-accept response BEFORE clicking ---
    const registerDone = page.waitForResponse(
      (res) => res.url().includes('/register-and-accept') && res.request().method() === 'POST',
      { timeout: 15_000 }
    )
    await page.getByRole('button', { name: /create account/i }).click()
    const regResponse = await registerDone

    expect(regResponse.ok()).toBe(true)

    // --- After onRegisterSuccess the app pushes /?session=<id> ---
    await expect(page).toHaveURL(new RegExp(`session=${session.id}`), { timeout: 10_000 })
  }
)
