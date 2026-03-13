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
