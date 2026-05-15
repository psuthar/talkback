-- SCRUM-415: add 'waiting_native_transcript' to the session_processing_jobs.state
-- CHECK constraint. This is the new state Meet (and eventually Teams) jobs
-- enter when ListTranscripts returns transcripts in non-terminal states
-- (STARTED / ENDED but not FILE_GENERATED). Distinct from 'waiting' so the
-- worker can poll with a longer cadence and apply the SCRUM-415 max-age
-- escalation to Whisper rather than the generic waiting → failed_permanent
-- escalation.
ALTER TABLE session_processing_jobs DROP CONSTRAINT IF EXISTS session_processing_jobs_state_check;
ALTER TABLE session_processing_jobs ADD CONSTRAINT session_processing_jobs_state_check
    CHECK (state IN (
        'queued', 'fetching', 'downloading', 'parsing', 'chunking', 'embedding',
        'waiting', 'awaiting_whisper', 'waiting_native_transcript',
        'ready', 'failed_transient', 'failed_permanent', 'canceled'
    ));
