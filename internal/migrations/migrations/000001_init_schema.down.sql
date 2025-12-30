-- Drop triggers
DROP TRIGGER IF EXISTS update_artifacts_updated_at ON artifacts;

-- Drop function
DROP FUNCTION IF EXISTS update_updated_at_column();

-- Drop tables in reverse order
DROP TABLE IF EXISTS video_sources;
DROP TABLE IF EXISTS materials;
DROP TABLE IF EXISTS artifacts;

-- Drop extension
DROP EXTENSION IF EXISTS pgcrypto;

