-- SCRUM-295: link video_sources rows to a file_artifacts row so MP4 uploads
-- (and future video sources) can be referenced as the session primary via
-- the SCRUM-271/272 primary system, which keys off file_artifacts.id.
--
-- Nullable + ON DELETE SET NULL: pre-existing video_sources rows have no
-- file_artifact (legacy artifacts table only); they get NULL here and the
-- frontend short-circuits Make-primary affordances for those rows.

ALTER TABLE video_sources
    ADD COLUMN file_artifact_id UUID NULL REFERENCES file_artifacts(id) ON DELETE SET NULL;

CREATE INDEX idx_video_sources_file_artifact_id ON video_sources(file_artifact_id);
