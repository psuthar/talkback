-- SCRUM-283: drop the audit trail table.

DROP INDEX IF EXISTS idx_sessions_primary_history_actor_user_id;
DROP INDEX IF EXISTS idx_sessions_primary_history_session_id;
DROP TABLE IF EXISTS sessions_primary_history;
