import path from 'node:path'
import { fileURLToPath } from 'node:url'
import 'dotenv/config'
import { defineConfig, devices } from '@playwright/test'
import dotenv from 'dotenv'

const __dirname = path.dirname(fileURLToPath(import.meta.url))

// When E2E_TARGET=render, load .env.e2e so tests hit Render (API + app) instead of localhost
if (process.env.E2E_TARGET === 'render') {
  dotenv.config({ path: '.env.e2e' })
  dotenv.config({ path: '.env.e2e.local' })
}

/** Pre-seeds localStorage (talkback.apiBaseUrl) via global-setup — split-origin preview+API without ?api= in URLs */
const e2eStorageState = path.join(__dirname, 'tests/e2e/.cache/playwright-storage-state.json')

export default defineConfig({
  testDir: './tests/e2e',
  testMatch: '**/*.e2e.ts',
  globalSetup: './tests/e2e/global-setup.ts',
  globalTeardown: './tests/e2e/global-teardown.ts',
  fullyParallel: false,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  workers: process.env.CI ? 1 : undefined,
  timeout: 30000,
  expect: { timeout: 10000 },
  reporter: [
    ['line'],
    ['json', { outputFile: '../playwright-results.json' }],
    ['html', { outputFolder: 'playwright-report', open: 'never' }],
  ],
  use: {
    // Only use E2E_BASE_URL when targeting Render; otherwise local runs always use localhost (avoids .env or shell env leaking Render URL).
    baseURL: process.env.E2E_TARGET === 'render' ? (process.env.E2E_BASE_URL || 'http://localhost:3000') : 'http://localhost:3000',
    storageState: e2eStorageState,
    trace: 'on-first-retry',
    screenshot: 'only-on-failure',
  },
  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
    },
  ],
  // Local (npm run test:e2e): API at localhost:8080, app at localhost:3000. Start both first.
  // If API runs elsewhere, set TALKBACK_API_BASE. Render: npm run test:e2e:render.
})
