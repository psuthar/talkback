/**
 * SCRUM-275 client for the SCRUM-272 PATCH session primary endpoint.
 *
 * patchSessionPrimary calls PATCH /api/sessions/{sessionId} with body
 *   { primary: { kind, id } }
 * and returns a parsed response. Errors carry the API's structured
 * { error, code } payload when present so callers can map known
 * codes (PRIMARY_BAD_KIND, PRIMARY_MATERIAL_NOT_FOUND, etc.) to
 * recoverable copy.
 *
 * Pass primary=null to clear the explicit primary (falls back to legacy
 * primary_video_artifact_id semantics on the server).
 */
export class SessionPrimaryError extends Error {
	constructor(status, code, message) {
		super(message || code || `HTTP ${status}`)
		this.status = status
		this.code = code
		this.name = 'SessionPrimaryError'
	}
}

export async function patchSessionPrimary(apiBaseUrl, sessionId, primary, { fetchImpl = fetch } = {}) {
	const base = (apiBaseUrl || '').replace(/\/$/, '')
	const body = primary === null
		? { primary: { kind: '' } }
		: { primary: { kind: primary.kind, id: primary.id } }
	const res = await fetchImpl(`${base}/api/sessions/${sessionId}`, {
		method: 'PATCH',
		credentials: 'include',
		headers: { 'Content-Type': 'application/json' },
		body: JSON.stringify(body),
	})
	if (!res.ok) {
		let payload = null
		try { payload = await res.json() } catch { /* ignore non-JSON body */ }
		const code = payload?.code || ''
		const msg = payload?.error || `Failed to update session primary: ${res.status}`
		throw new SessionPrimaryError(res.status, code, msg)
	}
	try { return await res.json() } catch { return null }
}
