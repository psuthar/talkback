-- Revert Zoom-centric migration

ALTER TABLE video_sources DROP COLUMN IF EXISTS transcript_segments;
ALTER TABLE video_sources DROP COLUMN IF EXISTS raw_vtt;

DROP TABLE IF EXISTS zoom_connections;

ALTER TABLE sessions DROP COLUMN IF EXISTS source_reference_url;
ALTER TABLE sessions DROP COLUMN IF EXISTS source_provider;
