/**
 * Single source of truth for API base URL.
 * - Build time: VITE_API_BASE_URL (set in Render Static Site env for production).
 * - Local dev: defaults to http://localhost:8081 when unset.
 */
export function getDefaultApiBaseUrl() {
  const v = import.meta.env?.VITE_API_BASE_URL
  if (typeof v === 'string' && v.trim()) return v.trim()
  return 'http://localhost:8081'
}
