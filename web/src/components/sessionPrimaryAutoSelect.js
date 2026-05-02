/**
 * Pure helper for SCRUM-274's "default the center pane to the session's
 * primary on load". Decoupled from ParticipantMode so it can be unit-tested
 * without React rendering / DOM.
 *
 * Returns one of:
 *   null                          — no auto-selection should happen.
 *   { type: 'material', material } — Caller should set selectedDocument to the
 *                                    material object and selectedDocumentId
 *                                    to material.id.
 *   { type: 'link', link }         — Caller should construct the same
 *                                    selectedDocument shape ParticipantMode's
 *                                    handleSelectLink uses (type='link', url,
 *                                    title, id) and selectedDocumentId
 *                                    'link-${link.id}'.
 *
 * Skip rules:
 *   - currentSession is missing / has no primary → null.
 *   - selectedDocumentId is truthy → user already chose something; don't
 *     override on refetch.
 *   - primary.kind === 'video' → existing center-pane default renders the
 *     video; no auto-select needed.
 *   - primary.kind === 'document' but no matching material in
 *     currentSession.materials → null (caller should leave the pane alone
 *     so the resolver's "missing pointer" state shows the legacy fallback).
 *   - primary.kind === 'link' but no matching link with a non-empty url → null.
 */
export function resolvePrimaryAutoSelection(currentSession, selectedDocumentId) {
	if (!currentSession) return null
	const primary = currentSession.primary
	if (!primary || !primary.id) return null
	if (selectedDocumentId) return null

	if (primary.kind === 'document') {
		const materials = Array.isArray(currentSession.materials) ? currentSession.materials : []
		const material = materials.find(m => m && String(m.id) === String(primary.id))
		if (!material) return null
		return { type: 'material', material }
	}

	if (primary.kind === 'link') {
		const links = Array.isArray(currentSession.links)
			? currentSession.links
			: (Array.isArray(currentSession.session?.links) ? currentSession.session.links : [])
		const link = links.find(l => l && String(l.id) === String(primary.id))
		if (!link?.url) return null
		return { type: 'link', link }
	}

	// kind === 'video' (or any other value): no auto-select; the existing
	// center-pane default renders correctly.
	return null
}
