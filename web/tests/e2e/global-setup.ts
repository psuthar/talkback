/**
 * Seeds Playwright storage state so the SPA uses the same API origin as fixtures (TALKBACK_API_BASE)
 * without putting ?api= on every URL — required for preview (:3000) + API (:8081) in CI.
 *
 * Writes tests/e2e/.cache/playwright-storage-state.json (gitignored).
 */
import * as fs from 'node:fs'
import * as path from 'node:path'
import { fileURLToPath } from 'node:url'
import dotenv from 'dotenv'

const __dirname = path.dirname(fileURLToPath(import.meta.url))

if (process.env.E2E_TARGET === 'render') {
  dotenv.config({ path: path.join(__dirname, '../../.env.e2e') })
  dotenv.config({ path: path.join(__dirname, '../../.env.e2e.local') })
}

export default async function globalSetup(): Promise<void> {
  const cacheDir = path.join(__dirname, '.cache')
  fs.mkdirSync(cacheDir, { recursive: true })
  const out = path.join(cacheDir, 'playwright-storage-state.json')

  const apiBase = (process.env.TALKBACK_API_BASE || 'http://localhost:8080').replace(/\/$/, '')
  const appOrigin = process.env.E2E_BASE_URL?.trim()
    ? new URL(process.env.E2E_BASE_URL).origin
    : 'http://localhost:3000'

  const state = {
    cookies: [] as unknown[],
    origins: [
      {
        origin: appOrigin,
        localStorage: [{ name: 'talkback.apiBaseUrl', value: apiBase }],
      },
    ],
  }

  fs.writeFileSync(out, JSON.stringify(state), 'utf-8')
  // eslint-disable-next-line no-console
  console.log(`[global-setup] wrote storage state: origin=${appOrigin} apiBase=${apiBase}`)
}
