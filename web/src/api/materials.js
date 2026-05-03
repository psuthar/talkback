/**
 * Fetch helpers for session materials (e.g. slides endpoint).
 */

/**
 * @typedef {{ index: number; image_url: string }} MaterialSlide
 * @typedef {{ material_id: string; slides: MaterialSlide[] }} MaterialSlidesResponse
 */

/**
 * Load slide metadata and presigned image URLs for a slides material.
 * @param {string} apiBaseUrl
 * @param {string} sessionId
 * @param {string} materialId
 * @returns {Promise<MaterialSlidesResponse>}
 */
export async function getMaterialSlides(apiBaseUrl, sessionId, materialId) {
  const base = (apiBaseUrl || '').replace(/\/$/, '')
  const res = await fetch(
    `${base}/sessions/${sessionId}/materials/${materialId}/slides`,
    { credentials: 'include' }
  )
  if (!res.ok) {
    throw new Error(`Failed to load slides: ${res.status}`)
  }
  return res.json()
}

/**
 * SCRUM-287: re-run the slides derivation pipeline for a .ppt/.pptx material
 * whose previous attempt failed. Creator-only on the server. Resolves on a
 * 202 enqueue; the actual outcome propagates via the session_updated WS
 * event when the background goroutine finishes.
 * @param {string} apiBaseUrl
 * @param {string} sessionId
 * @param {string} materialId
 * @returns {Promise<void>}
 */
export async function retryMaterialSlides(apiBaseUrl, sessionId, materialId) {
  const base = (apiBaseUrl || '').replace(/\/$/, '')
  const res = await fetch(
    `${base}/api/sessions/${sessionId}/materials/${materialId}/retry-slides`,
    {
      method: 'POST',
      credentials: 'include',
    }
  )
  if (!res.ok && res.status !== 202) {
    throw new Error(`Failed to retry slides: ${res.status}`)
  }
}

/**
 * Set or clear the display title for a video source.
 * @param {string} apiBaseUrl
 * @param {string} sessionId
 * @param {string} videoSourceId
 * @param {string|null} title - pass null to clear
 * @returns {Promise<void>}
 */
export async function updateVideoDisplayTitle(apiBaseUrl, sessionId, videoSourceId, title) {
  const base = (apiBaseUrl || '').replace(/\/$/, '')
  const res = await fetch(
    `${base}/sessions/${sessionId}/video-sources/${videoSourceId}/display-title`,
    {
      method: 'PATCH',
      credentials: 'include',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ display_title: title || null }),
    }
  )
  if (!res.ok) {
    throw new Error(`Failed to update display title: ${res.status}`)
  }
}
