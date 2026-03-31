/**
 * Coalesce rapid WebSocket-driven orchestration syncs (SCRUM-16).
 * Creator UI calls POST /api/sessions/:id/orchestration/recommendations/sync at most once per window.
 */
export const ORCHESTRATION_AUTO_REFRESH_DEBOUNCE_MS = 900
