-- Migration: Add transcript jobs table for auto-transcription of Loom videos
-- This enables background processing of video transcription via Whisper

-- Create transcript_jobs table to track transcription status
CREATE TABLE IF NOT EXISTS transcript_jobs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    video_source_id UUID NOT NULL REFERENCES video_sources(id) ON DELETE CASCADE,
    session_id UUID NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    status TEXT NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'downloading', 'transcribing', 'saving', 'completed', 'failed')),
    error_message TEXT NULL,
    
    -- Source information
    source_url TEXT NOT NULL,
    resolved_media_url TEXT NULL,
    
    -- Job metadata
    queued_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    started_at TIMESTAMPTZ NULL,
    completed_at TIMESTAMPTZ NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    
    -- Transcription metadata
    whisper_model TEXT NULL,
    detected_language TEXT NULL,
    duration_seconds INT NULL,
    
    -- Idempotency: prevent duplicate jobs for same video
    job_key TEXT NOT NULL, -- hash(video_source_id + source_url)
    UNIQUE(job_key)
);

-- Indexes for efficient querying
CREATE INDEX idx_transcript_jobs_video_source_id ON transcript_jobs(video_source_id);
CREATE INDEX idx_transcript_jobs_session_id ON transcript_jobs(session_id);
CREATE INDEX idx_transcript_jobs_status ON transcript_jobs(status);
CREATE INDEX idx_transcript_jobs_job_key ON transcript_jobs(job_key);

-- Add columns to video_sources for tracking auto-transcription
ALTER TABLE video_sources
ADD COLUMN IF NOT EXISTS auto_transcribe_enabled BOOLEAN NOT NULL DEFAULT false,
ADD COLUMN IF NOT EXISTS transcription_source TEXT NULL CHECK (transcription_source IN ('manual', 'loom_api', 'whisper')),
ADD COLUMN IF NOT EXISTS transcription_job_id UUID NULL REFERENCES transcript_jobs(id) ON DELETE SET NULL;

CREATE INDEX IF NOT EXISTS idx_video_sources_transcription_job_id ON video_sources(transcription_job_id);
