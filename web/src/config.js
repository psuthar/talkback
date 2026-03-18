/**
 * Single source of truth for API base URL.
 * - Build time: VITE_API_BASE_URL (set in Render Static Site env for production).
 * - Local dev: defaults to same-origin-relative paths (''), so Vite can proxy to the API.
 */
export function getDefaultApiBaseUrl() {
  const v = import.meta.env?.VITE_API_BASE_URL
  if (typeof v === 'string' && v.trim()) return v.trim()
  return ''
}

/**
 * How long (ms) of silence after speech before auto-stopping voice recording.
 * Override with VITE_VOICE_SILENCE_MS (e.g. 2000 for 2 seconds).
 */
export function getVoiceSilenceMs() {
  const v = import.meta.env?.VITE_VOICE_SILENCE_MS
  if (v != null && v !== '') {
    const n = Number(v)
    if (!Number.isNaN(n) && n > 0) return Math.round(n)
  }
  return 2000
}
