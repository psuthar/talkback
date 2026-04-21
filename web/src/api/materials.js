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
