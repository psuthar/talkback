import 'dotenv/config'
import { defineConfig, devices } from '@playwright/test'
import dotenv from 'dotenv'

// When E2E_TARGET=render, load .env.e2e so tests hit Render (API + app) instead of localhost
if (process.env.E2E_TARGET === 'render') {
  dotenv.config({ path: '.env.e2e' })
  dotenv.config({ path: '.env.e2e.local' })
}

export default defineConfig({
  testDir: './tests/e2e',
  testMatch: '**/*.e2e.ts',
  fullyParallel: false,
  forbidOnly: !!process.env.CI,
  retries: process.env.CI ? 1 : 0,
  workers: 1,
  reporter: 'html',
  use: {
    // Only use E2E_BASE_URL when targeting Render; otherwise local runs always use localhost (avoids .env or shell env leaking Render URL).
    baseURL: process.env.E2E_TARGET === 'render' ? (process.env.E2E_BASE_URL || 'http://localhost:3000') : 'http://localhost:3000',
    trace: 'on-first-retry',
    screenshot: 'only-on-failure',
  },
  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
    },
  ],
  // Local (npm run test:e2e): API at localhost:8081 (match debugger PORT=8081 or .env), app at localhost:3000. Start both first.
  // If API runs on 8080 (go run without PORT), set TALKBACK_API_BASE=http://localhost:8080. Render: npm run test:e2e:render.
})
