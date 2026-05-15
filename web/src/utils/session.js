// SCRUM-405: shared accessors that hide the 1:1 / 1:N cardinality detail from
// call sites. A session may have N recordings post-SCRUM-401; the primary is
// addressable via `session.primary_video` (server-set), and the rest live in
// `session.video_sources`.

// getRecordings returns the recordings list for a session in upload order
// (oldest first). Empty array when the session has none — never null/undefined.
//
// Input can be either the top-level session payload (object with
// `video_sources`) or, defensively, the inner `session` row alone — we just
// read `video_sources` if it's there.
export function getRecordings(session) {
  if (!session) return []
  const list = Array.isArray(session.video_sources) ? session.video_sources : []
  // Stable, deterministic order: `created_at` ASC; rows without `created_at`
  // sort last (preserving their relative array order). One existing recording
  // is unaffected by sorting.
  if (list.length <= 1) return list
  return [...list].sort((a, b) => {
    const aHas = !!a?.created_at
    const bHas = !!b?.created_at
    if (aHas && bHas) {
      // ISO timestamps compare lexicographically.
      if (a.created_at < b.created_at) return -1
      if (a.created_at > b.created_at) return 1
      return 0
    }
    if (aHas && !bHas) return -1
    if (!aHas && bHas) return 1
    return 0
  })
}

// getPrimaryRecording returns the primary recording for a session, or null
// when the session has no recordings. Resolution order:
//   1. `session.primary_video` (server-set; explicit primary)
//   2. First recording by upload time (`created_at` ASC)
//   3. null (no recordings)
//
// Behavior is identical to the legacy `session.primary_video ??
// session.video_sources[0] ?? null` for any current single-recording session.
export function getPrimaryRecording(session) {
  if (!session) return null
  if (session.primary_video) return session.primary_video
  const recordings = getRecordings(session)
  return recordings[0] ?? null
}

// hasAnyRecording is a small convenience for empty-state branches that don't
// care which recording is primary, only whether there is one at all.
export function hasAnyRecording(session) {
  return !!getPrimaryRecording(session)
}
