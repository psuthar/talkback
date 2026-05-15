-- SCRUM-403: rework session_processing_jobs uniqueness to allow N recordings per
-- (session, source) keyed by meeting_uuid/instance_uuid, and add
-- video_sources.external_recording_id for cross-platform dedupe of imported
-- recordings.

-- 1) session_processing_jobs: drop the strict UNIQUE(session_id, source) and
-- replace it with an expression-based UNIQUE that treats NULL meeting/instance
-- UUIDs as the empty string. Existing single-recording rows (meeting_uuid IS
-- NULL) keep the same uniqueness semantics; multi-recording rows differ on
-- meeting_uuid or instance_uuid.
DROP INDEX IF EXISTS idx_session_processing_jobs_session_source;

CREATE UNIQUE INDEX IF NOT EXISTS idx_session_processing_jobs_session_source_meeting
    ON session_processing_jobs (
        session_id,
        source,
        COALESCE(meeting_uuid, ''),
        COALESCE(instance_uuid, '')
    );

-- 2) video_sources.external_recording_id: platform-native ID for the recording
-- (Zoom meeting UUID, Meet recording resource name, Teams recordingId). NULL
-- for upload/link rows that don't have a platform recording.
ALTER TABLE video_sources
    ADD COLUMN IF NOT EXISTS external_recording_id TEXT NULL;

-- 3) Partial UNIQUE: same external recording cannot be imported twice into the
-- same session for the same provider. NULL external_recording_id rows are not
-- constrained (idempotency only applies to platform imports).
CREATE UNIQUE INDEX IF NOT EXISTS idx_video_sources_session_provider_external_id
    ON video_sources (session_id, provider, external_recording_id)
    WHERE external_recording_id IS NOT NULL;
