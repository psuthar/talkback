-- Migration to update existing schema to Phase 1 requirements
-- This handles the case where the old schema exists

-- Add description column to artifacts if it doesn't exist
DO $$ 
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns 
        WHERE table_name = 'artifacts' AND column_name = 'description'
    ) THEN
        ALTER TABLE artifacts ADD COLUMN description TEXT;
    END IF;
END $$;

-- Update artifacts status constraint if needed
DO $$
BEGIN
    -- Drop old constraint if it exists and doesn't match
    IF EXISTS (
        SELECT 1 FROM information_schema.table_constraints 
        WHERE constraint_name = 'artifacts_status_check'
    ) THEN
        ALTER TABLE artifacts DROP CONSTRAINT IF EXISTS artifacts_status_check;
    END IF;
END $$;

ALTER TABLE artifacts ADD CONSTRAINT artifacts_status_check 
    CHECK (status IN ('draft', 'ready'));

-- Drop old materials columns if they exist
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns 
        WHERE table_name = 'materials' AND column_name = 'file_path'
    ) THEN
        ALTER TABLE materials DROP COLUMN IF EXISTS file_path;
        ALTER TABLE materials DROP COLUMN IF EXISTS file_name;
        ALTER TABLE materials DROP COLUMN IF EXISTS file_size;
        ALTER TABLE materials DROP COLUMN IF EXISTS mime_type;
    END IF;
END $$;

-- Add new materials columns if they don't exist
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns 
        WHERE table_name = 'materials' AND column_name = 'kind'
    ) THEN
        ALTER TABLE materials ADD COLUMN kind TEXT NOT NULL DEFAULT 'document';
        ALTER TABLE materials ADD COLUMN filename TEXT NOT NULL DEFAULT '';
        ALTER TABLE materials ADD COLUMN content_type TEXT NOT NULL DEFAULT 'application/octet-stream';
        ALTER TABLE materials ADD COLUMN storage_url TEXT NOT NULL DEFAULT '';
        ALTER TABLE materials ADD COLUMN text_status TEXT NOT NULL DEFAULT 'pending';
        ALTER TABLE materials ADD COLUMN extracted_text TEXT;
    END IF;
END $$;

-- Update materials constraints
ALTER TABLE materials DROP CONSTRAINT IF EXISTS materials_text_status_check;
ALTER TABLE materials ADD CONSTRAINT materials_text_status_check 
    CHECK (text_status IN ('pending', 'ready', 'failed'));

-- Drop old video_sources columns if they exist
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM information_schema.columns 
        WHERE table_name = 'video_sources' AND column_name = 'transcript'
    ) THEN
        ALTER TABLE video_sources DROP COLUMN IF EXISTS transcript;
        ALTER TABLE video_sources DROP COLUMN IF EXISTS updated_at;
    END IF;
END $$;

-- Add new video_sources columns if they don't exist
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns 
        WHERE table_name = 'video_sources' AND column_name = 'provider'
    ) THEN
        ALTER TABLE video_sources ADD COLUMN provider TEXT NOT NULL DEFAULT 'other';
        ALTER TABLE video_sources ADD COLUMN transcript_status TEXT NOT NULL DEFAULT 'missing';
        ALTER TABLE video_sources ADD COLUMN transcript_text TEXT;
    END IF;
END $$;

-- Update video_sources constraints
ALTER TABLE video_sources DROP CONSTRAINT IF EXISTS video_sources_provider_check;
ALTER TABLE video_sources ADD CONSTRAINT video_sources_provider_check 
    CHECK (provider IN ('loom', 'zoom', 'other'));

ALTER TABLE video_sources DROP CONSTRAINT IF EXISTS video_sources_transcript_status_check;
ALTER TABLE video_sources ADD CONSTRAINT video_sources_transcript_status_check 
    CHECK (transcript_status IN ('missing', 'pending', 'ready', 'failed'));

-- Drop old unique constraint on video_sources if it exists
ALTER TABLE video_sources DROP CONSTRAINT IF EXISTS video_sources_artifact_id_key;

-- Drop embeddings table if it exists (not needed for Phase 1)
DROP TABLE IF EXISTS embeddings CASCADE;

