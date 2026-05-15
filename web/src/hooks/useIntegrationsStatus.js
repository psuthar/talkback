// SCRUM-418: read GET /api/integrations/status once on mount. Consumed by
// the Add Content tiles + Manage Connections modal that land in
// SCRUM-XX8a / 10 / 11. Returns { status, loading, error, refresh }. The
// status shape mirrors the backend's IntegrationsStatusResponse:
//   {
//     zoom:        { enabled, connected, account_email },
//     google_meet: { enabled, connected, account_email },
//     teams:       { enabled, connected, account_email },
//   }
import { useCallback, useEffect, useState } from 'react'

export function useIntegrationsStatus(apiBaseUrl) {
  const [status, setStatus] = useState(null)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState(null)

  const refresh = useCallback(async () => {
    if (!apiBaseUrl) return
    setLoading(true)
    setError(null)
    try {
      const base = apiBaseUrl.replace(/\/$/, '')
      const res = await fetch(`${base}/api/integrations/status`, {
        credentials: 'include',
        headers: { 'Accept': 'application/json' },
      })
      if (!res.ok) {
        throw new Error(`integrations status: ${res.status}`)
      }
      const body = await res.json()
      setStatus(body)
    } catch (err) {
      setError(err)
    } finally {
      setLoading(false)
    }
  }, [apiBaseUrl])

  useEffect(() => {
    refresh()
  }, [refresh])

  return { status, loading, error, refresh }
}
