import { describe, it, expect } from 'vitest'
import { ORCHESTRATION_AUTO_REFRESH_DEBOUNCE_MS } from './orchestrationAutoRefresh.js'

describe('orchestrationAutoRefresh', () => {
  it('uses a bounded debounce window for coalescing syncs', () => {
    expect(ORCHESTRATION_AUTO_REFRESH_DEBOUNCE_MS).toBeGreaterThanOrEqual(400)
    expect(ORCHESTRATION_AUTO_REFRESH_DEBOUNCE_MS).toBeLessThanOrEqual(5000)
  })
})
