-- SCRUM-404 down: drop session_speaker_aliases and its indexes.

DROP INDEX IF EXISTS idx_speaker_alias_canonical;
DROP INDEX IF EXISTS uniq_speaker_alias_per_session;
DROP TABLE IF EXISTS session_speaker_aliases;
