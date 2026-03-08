-- Allow state 'awaiting_whisper' for Zoom Whisper fallback (video stored, transcript job enqueued)
ALTER TABLE session_processing_jobs DROP CONSTRAINT IF EXISTS session_processing_jobs_state_check;
ALTER TABLE session_processing_jobs ADD CONSTRAINT session_processing_jobs_state_check CHECK (state IN (
    'queued', 'fetching', 'downloading', 'parsing', 'chunking', 'embedding',
    'waiting', 'awaiting_whisper', 'ready', 'failed_transient', 'failed_permanent', 'canceled'
));
