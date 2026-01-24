-- Phase 3: Remove sessions, session_participants, and session_events tables

-- Drop trigger
DROP TRIGGER IF EXISTS trigger_update_sessions_updated_at ON sessions;
DROP FUNCTION IF EXISTS update_sessions_updated_at();

-- Remove session_id from questions
ALTER TABLE questions DROP COLUMN IF EXISTS session_id;

-- Drop indexes
DROP INDEX IF EXISTS idx_questions_artifact_session_created;
DROP INDEX IF EXISTS idx_session_events_event_type;
DROP INDEX IF EXISTS idx_session_events_created_at;
DROP INDEX IF EXISTS idx_session_events_session_id;
DROP INDEX IF EXISTS idx_session_participants_participant_ref;
DROP INDEX IF EXISTS idx_session_participants_session_id;
DROP INDEX IF EXISTS idx_sessions_status;
DROP INDEX IF EXISTS idx_sessions_created_at;
DROP INDEX IF EXISTS idx_sessions_artifact_id;

-- Drop tables (order matters due to foreign keys)
DROP TABLE IF EXISTS session_events;
DROP TABLE IF EXISTS session_participants;
DROP TABLE IF EXISTS sessions;
