-- SCRUM-269: persisted "primary" material on sessions, broader than the
-- legacy primary_video_artifact_id. Document- or link-first sessions can now
-- anchor the center pane without relying on UI selection state.
--
-- Backward-compat: existing rows with primary_video_artifact_id IS NOT NULL
-- continue to imply kind=video at the app layer. primary_content_kind stays
-- NULL until a session explicitly designates a primary, at which point the
-- matching FK column (primary_video_artifact_id / primary_material_id /
-- primary_session_link_id) carries the reference.
--
-- DB-level invariants are intentionally lax — handler-side validation in
-- SCRUM-272 rejects kind/id mismatches and enforces session-ownership of the
-- pointer. Down-migration is fully reversible (drops columns + indexes only).

ALTER TABLE sessions
    ADD COLUMN IF NOT EXISTS primary_content_kind TEXT NULL
        CHECK (primary_content_kind IS NULL
               OR primary_content_kind IN ('video', 'document', 'link')),
    ADD COLUMN IF NOT EXISTS primary_material_id UUID NULL
        REFERENCES materials(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS primary_session_link_id UUID NULL
        REFERENCES session_links(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_sessions_primary_material_id
    ON sessions(primary_material_id);
CREATE INDEX IF NOT EXISTS idx_sessions_primary_session_link_id
    ON sessions(primary_session_link_id);
