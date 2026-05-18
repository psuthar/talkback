import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { renderHook, waitFor } from '@testing-library/react'
import { useIntegrationsStatus } from '../hooks/useIntegrationsStatus'

const sample = {
  zoom: { enabled: true, connected: true, account_email: 'z@example.com' },
  google_meet: { enabled: true, connected: false, account_email: null },
  teams: { enabled: false, connected: false, account_email: null },
}

describe('useIntegrationsStatus', () => {
  let originalFetch
  beforeEach(() => {
    originalFetch = global.fetch
  })
  afterEach(() => {
    global.fetch = originalFetch
  })

  it('returns the parsed JSON after a successful GET', async () => {
    global.fetch = vi.fn().mockResolvedValueOnce({
      ok: true,
      status: 200,
      json: async () => sample,
    })
    const { result } = renderHook(() => useIntegrationsStatus('http://api.test'))
    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.status).toEqual(sample)
    expect(result.current.error).toBeNull()
    expect(global.fetch).toHaveBeenCalledWith(
      'http://api.test/api/integrations/status',
      expect.objectContaining({ credentials: 'include' })
    )
  })

  it('surfaces a non-ok response as an error', async () => {
    global.fetch = vi.fn().mockResolvedValueOnce({ ok: false, status: 500, json: async () => ({}) })
    const { result } = renderHook(() => useIntegrationsStatus('http://api.test'))
    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(result.current.status).toBeNull()
    expect(result.current.error).not.toBeNull()
  })

  // SCRUM-459: empty string is a valid same-origin apiBaseUrl, NOT a
  // missing-configuration signal. The hook must still fetch with a
  // relative URL so the deployed SPA (which is served same-origin with
  // the API) works without an explicit base.
  it('fetches with a same-origin relative URL when apiBaseUrl is empty (SCRUM-459)', async () => {
    global.fetch = vi.fn().mockResolvedValueOnce({ ok: true, status: 200, json: async () => sample })
    const { result } = renderHook(() => useIntegrationsStatus(''))
    await waitFor(() => expect(result.current.loading).toBe(false))
    expect(global.fetch).toHaveBeenCalledWith(
      '/api/integrations/status',
      expect.objectContaining({ credentials: 'include' })
    )
    expect(result.current.status).toEqual(sample)
  })

  it('does nothing when apiBaseUrl is null or undefined', () => {
    global.fetch = vi.fn()
    renderHook(() => useIntegrationsStatus(null))
    renderHook(() => useIntegrationsStatus(undefined))
    expect(global.fetch).not.toHaveBeenCalled()
  })
})
