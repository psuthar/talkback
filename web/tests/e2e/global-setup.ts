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

  // Mirror fixtures.ts/global-teardown.ts: honor TALKBACK_API_BASE (CI sets port 8081). Guard
  // against a stale remote URL in web/.env pointing the browser at an unreachable origin by
  // only accepting non-localhost values when E2E_TARGET=render. Localhost with any port is fine.
  const envBase = process.env.TALKBACK_API_BASE?.trim()
  const isLocalhost = envBase ? /^https?:\/\/(localhost|127\.0\.0\.1)(:|\/|$)/.test(envBase) : false
  const useEnvBase = envBase && (isLocalhost || process.env.E2E_TARGET === 'render')
  if (envBase && !useEnvBase) {
    // eslint-disable-next-line no-console
    console.warn(`[global-setup] ignoring non-localhost TALKBACK_API_BASE=${envBase}; set E2E_TARGET=render to use it`)
  }
  const apiBase = (useEnvBase ? envBase! : 'http://localhost:8080').replace(/\/$/, '')
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
