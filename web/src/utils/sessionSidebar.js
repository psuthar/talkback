/**
 * Helpers for the lifted session-sidebar sections (Context, Members, Materials)
 * shared by CreatorMode and ParticipantMode. Kept as small pure functions so
 * they can be unit-tested without rendering the modes themselves.
 */

/** Below this viewport width, Context and Members start collapsed on initial
 *  render regardless of the stored preference. Materials sub-collapse still
 *  honors its stored value so the materials tree remains discoverable. */
export const NARROW_VIEWPORT_PX = 1024

/**
 * Count of materials-tree items for the sub-header label "Materials (N)".
 * Matches the formula used by MaterialsPanelHeader's itemCount derivation.
 */
export function sessionMaterialsCount(session) {
  if (!session) return 0
  const videos = Array.isArray(session.video_sources) ? session.video_sources.length : 0
  const materials = Array.isArray(session.materials) ? session.materials.length : 0
  const links = Array.isArray(session.links) ? session.links.length : 0
  return videos + materials + links
}

/**
 * Returns true when the viewport is narrow enough that the lifted Context and
 * Members blocks should start collapsed on initial render, overriding any
 * stored expanded preference. Accepts an injected window object for testing.
 */
export function shouldForceCollapseSidebarOnLoad(win = typeof window !== 'undefined' ? window : null) {
  if (!win || typeof win.innerWidth !== 'number') return false
  return win.innerWidth < NARROW_VIEWPORT_PX
}

/**
 * Resolves the initial expanded state for a sidebar section, applying the
 * narrow-viewport override for Context and Members.
 *
 * @param {object} args
 * @param {boolean|null} args.stored - null = no preference stored
 * @param {boolean} args.defaultExpanded - fallback when no stored preference
 * @param {boolean} args.honorNarrowOverride - when true, force false if viewport is narrow
 * @param {Window|null} args.win
 */
export function resolveInitialExpanded({ stored, defaultExpanded, honorNarrowOverride, win }) {
  if (honorNarrowOverride && shouldForceCollapseSidebarOnLoad(win)) return false
  if (stored === null || stored === undefined) return !!defaultExpanded
  return !!stored
}
