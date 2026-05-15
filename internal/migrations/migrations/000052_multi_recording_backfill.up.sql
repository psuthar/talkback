-- SCRUM-410: backfill video_sources.external_recording_id for legacy rows
-- and reconcile the two primary-recording pointers
-- (sessions.primary_video_artifact_id vs video_sources.video_role='primary')
-- to a single canonical source of truth (video_role).
--
-- Idempotent: every UPDATE is gated by an IS NULL or "no other row already
-- carries this state" predicate, so re-running is a no-op.

-- 1) Backfill external_recording_id where it is NULL. Precedence per the
--    SCRUM-410 plan:
--      a) provider-matched session_processing_jobs.meeting_uuid (legacy
--         single-recording rows almost always have one job per session).
--      b) provider='teams' falls back to session_processing_jobs.instance_uuid.
--      c) provider='other' (uploaded MP4) → file_artifact_id::text when set.
--      d) synthetic '<session_id>:<video_source_id>' so the row carries a
--         stable, unique value that satisfies the SCRUM-403 partial UNIQUE
--         without enabling accidental dedupe against real platform IDs
--         (UUIDs don't contain ':').
UPDATE video_sources vs
SET external_recording_id = COALESCE(
    (
        SELECT NULLIF(j.meeting_uuid, '')
        FROM session_processing_jobs j
        WHERE j.session_id = vs.session_id
          AND j.source = vs.provider
          AND j.meeting_uuid IS NOT NULL
        ORDER BY j.created_at ASC
        LIMIT 1
    ),
    (
        SELECT NULLIF(j.instance_uuid, '')
        FROM session_processing_jobs j
        WHERE j.session_id = vs.session_id
          AND j.source = vs.provider
          AND vs.provider = 'teams'
          AND j.instance_uuid IS NOT NULL
        ORDER BY j.created_at ASC
        LIMIT 1
    ),
    CASE
        WHEN vs.provider = 'other' AND vs.file_artifact_id IS NOT NULL
        THEN vs.file_artifact_id::text
    END,
    vs.session_id::text || ':' || vs.id::text
)
WHERE external_recording_id IS NULL;

-- 2) Reconcile primary pointers. Three branches per the SCRUM-410 plan:
--    a) sessions.primary_video_artifact_id is set AND no row in that session
--       already has video_role='primary' → mark the matching row primary.
UPDATE video_sources vs
SET video_role = 'primary'
FROM sessions s
WHERE s.id = vs.session_id
  AND s.primary_video_artifact_id IS NOT NULL
  AND vs.file_artifact_id IS NOT NULL
  AND vs.file_artifact_id = s.primary_video_artifact_id
  AND NOT EXISTS (
    SELECT 1 FROM video_sources existing_primary
    WHERE existing_primary.session_id = vs.session_id
      AND existing_primary.video_role = 'primary'
  );

--    b) Sessions with exactly one recording, no video_role='primary', and no
--       primary_video_artifact_id → mark that single recording primary so
--       the canonical pointer is populated everywhere.
WITH single_recording_sessions AS (
    SELECT vs.session_id, (array_agg(vs.id))[1] AS only_video_id
    FROM video_sources vs
    LEFT JOIN sessions s ON s.id = vs.session_id
    GROUP BY vs.session_id
    HAVING COUNT(*) = 1
       AND NOT EXISTS (
           SELECT 1 FROM video_sources existing_primary
           WHERE existing_primary.session_id = vs.session_id
             AND existing_primary.video_role = 'primary'
       )
       AND BOOL_AND(s.primary_video_artifact_id IS NULL)
)
UPDATE video_sources vs
SET video_role = 'primary'
FROM single_recording_sessions srs
WHERE vs.id = srs.only_video_id;

-- Note: case (c) from the SCRUM-410 plan — both pointers set and disagree —
-- is a no-op here because video_role='primary' already wins by being the row
-- field. The follow-up to drop sessions.primary_video_artifact_id is filed
-- separately (NOT executed in this migration).
