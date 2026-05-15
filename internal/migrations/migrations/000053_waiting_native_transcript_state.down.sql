-- SCRUM-415 down: drop 'waiting_native_transcript' from the state CHECK
-- constraint. Any rows currently in that state will be left as-is — pgsql
-- ALTER CONSTRAINT does not validate existing rows when DROP+ADD pattern
-- is used. Caller is responsible for resetting any
-- 'waiting_native_transcript' rows before running the down migration; the
-- safer path is a snapshot restore.
ALTER TABLE session_processing_jobs DROP CONSTRAINT IF EXISTS session_processing_jobs_state_check;
ALTER TABLE session_processing_jobs ADD CONSTRAINT session_processing_jobs_state_check
    CHECK (state IN (
        'queued', 'fetching', 'downloading', 'parsing', 'chunking', 'embedding',
        'waiting', 'awaiting_whisper',
        'ready', 'failed_transient', 'failed_permanent', 'canceled'
    ));
