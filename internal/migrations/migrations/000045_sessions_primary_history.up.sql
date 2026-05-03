-- SCRUM-283: persistent audit trail for session primary changes. Every
-- successful PATCH primary writes one row capturing actor + before/after
-- {kind, id} so we can answer "who set what when" without scraping logs.
--
-- Schema is intentionally minimal:
--   * actor_user_id is nullable so we don't lose rows when the actor isn't
--     a user model (e.g. system-driven repair from SCRUM-282).
--   * prev_/new_kind store the closed-set string ("video"/"document"/"link"
--     /""), with empty meaning "no explicit primary".
--   * prev_/new_id are nullable UUIDs of the matching pointer at the time of
--     the change. We don't FK them to materials/links/file_artifacts because
--     audit history must survive deletion of the referenced rows.
--
-- Down-migration drops the table and indexes.

CREATE TABLE IF NOT EXISTS sessions_primary_history (
    id              UUID PRIMARY KEY,
    session_id      UUID NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    actor_user_id   UUID NULL REFERENCES users(id) ON DELETE SET NULL,
    prev_kind       TEXT NOT NULL DEFAULT '',
    prev_id         UUID NULL,
    new_kind        TEXT NOT NULL DEFAULT '',
    new_id          UUID NULL,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_sessions_primary_history_session_id
    ON sessions_primary_history(session_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_sessions_primary_history_actor_user_id
    ON sessions_primary_history(actor_user_id);
