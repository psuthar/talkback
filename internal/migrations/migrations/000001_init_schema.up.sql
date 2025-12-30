-- Enable pgcrypto extension for gen_random_uuid()
CREATE EXTENSION IF NOT EXISTS pgcrypto;

-- Create artifacts table
CREATE TABLE artifacts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    title TEXT NOT NULL,
    description TEXT,
    status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'ready')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Create materials table
CREATE TABLE materials (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    artifact_id UUID NOT NULL REFERENCES artifacts(id) ON DELETE CASCADE,
    kind TEXT NOT NULL DEFAULT 'document',
    filename TEXT NOT NULL,
    content_type TEXT NOT NULL,
    storage_url TEXT NOT NULL,
    text_status TEXT NOT NULL DEFAULT 'pending' CHECK (text_status IN ('pending', 'ready', 'failed')),
    extracted_text TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Create video_sources table
CREATE TABLE video_sources (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    artifact_id UUID NOT NULL REFERENCES artifacts(id) ON DELETE CASCADE,
    provider TEXT NOT NULL CHECK (provider IN ('loom', 'zoom', 'other')),
    video_url TEXT NOT NULL,
    transcript_status TEXT NOT NULL DEFAULT 'missing' CHECK (transcript_status IN ('missing', 'pending', 'ready', 'failed')),
    transcript_text TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Create trigger function to update updated_at timestamp
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$ language 'plpgsql';

-- Create trigger on artifacts table
CREATE TRIGGER update_artifacts_updated_at BEFORE UPDATE ON artifacts
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- Create indexes
CREATE INDEX idx_artifacts_status ON artifacts(status);
CREATE INDEX idx_materials_artifact_id ON materials(artifact_id);
CREATE INDEX idx_video_sources_artifact_id ON video_sources(artifact_id);

