-- === 000001_init_schema.up.sql ===
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


-- === 000002_update_schema.up.sql ===
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


-- === 000003_add_questions_answers.up.sql ===
-- Phase 2: Add questions and answers tables for Q&A functionality

-- Create questions table
CREATE TABLE questions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    artifact_id UUID NOT NULL REFERENCES artifacts(id) ON DELETE CASCADE,
    asked_by TEXT,
    question_text TEXT NOT NULL,
    question_source TEXT NOT NULL DEFAULT 'text' CHECK (question_source IN ('text')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Create answers table
CREATE TABLE answers (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    question_id UUID NOT NULL REFERENCES questions(id) ON DELETE CASCADE,
    answer_text TEXT NOT NULL,
    answer_status TEXT NOT NULL DEFAULT 'answered' CHECK (answer_status IN ('answered', 'not_covered', 'error')),
    confidence REAL NOT NULL DEFAULT 0 CHECK (confidence >= 0 AND confidence <= 1),
    citations JSONB NOT NULL DEFAULT '[]'::jsonb,
    model TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Create indexes
CREATE INDEX idx_questions_artifact_id ON questions(artifact_id);
CREATE INDEX idx_questions_created_at ON questions(created_at DESC);
CREATE INDEX idx_answers_question_id ON answers(question_id);
CREATE INDEX idx_answers_created_at ON answers(created_at DESC);

-- === 000004_add_sessions.up.sql ===
-- Phase 3: Add sessions, session_participants, and session_events tables

-- Create sessions table
CREATE TABLE sessions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    artifact_id UUID NOT NULL REFERENCES artifacts(id) ON DELETE CASCADE,
    title TEXT NOT NULL,
    created_by TEXT,
    status TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'closed')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Create session_participants table
CREATE TABLE session_participants (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id UUID NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    participant_ref TEXT NOT NULL,
    joined_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    watch_progress REAL NOT NULL DEFAULT 0 CHECK (watch_progress >= 0 AND watch_progress <= 1),
    UNIQUE(session_id, participant_ref)
);

-- Create session_events table
CREATE TABLE session_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id UUID NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    participant_ref TEXT,
    event_type TEXT NOT NULL CHECK (event_type IN ('join', 'leave', 'play', 'pause', 'seek', 'question')),
    video_time_seconds INT,
    payload JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Add session_id to questions table (nullable for backward compatibility)
ALTER TABLE questions ADD COLUMN session_id UUID REFERENCES sessions(id) ON DELETE SET NULL;

-- Create indexes
CREATE INDEX idx_sessions_artifact_id ON sessions(artifact_id);
CREATE INDEX idx_sessions_created_at ON sessions(created_at DESC);
CREATE INDEX idx_sessions_status ON sessions(status);

CREATE INDEX idx_session_participants_session_id ON session_participants(session_id);
CREATE INDEX idx_session_participants_participant_ref ON session_participants(participant_ref);

CREATE INDEX idx_session_events_session_id ON session_events(session_id);
CREATE INDEX idx_session_events_created_at ON session_events(created_at DESC);
CREATE INDEX idx_session_events_event_type ON session_events(event_type);

CREATE INDEX idx_questions_artifact_session_created ON questions(artifact_id, session_id, created_at DESC);

-- Create trigger to update updated_at for sessions
CREATE OR REPLACE FUNCTION update_sessions_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trigger_update_sessions_updated_at
    BEFORE UPDATE ON sessions
    FOR EACH ROW
    EXECUTE FUNCTION update_sessions_updated_at();

-- === 000005_artifacts_belong_to_sessions.up.sql ===
-- Phase 3 Update: Artifacts belong to sessions (reverse relationship)
-- This migration changes the relationship so that artifacts belong to sessions,
-- rather than sessions belonging to artifacts.

-- Step 1: Add session_id column to artifacts (nullable initially)
ALTER TABLE artifacts ADD COLUMN session_id UUID;

-- Step 2: For each existing session, create a default artifact and link it
-- OR: For each artifact that has a session, link the artifact to that session
-- Strategy: For each session, find its artifact and link that artifact to the session
UPDATE artifacts a
SET session_id = s.id
FROM sessions s
WHERE s.artifact_id = a.id;

-- Step 3: For artifacts without sessions, create a default session for each
-- and link the artifact to it
DO $$
DECLARE
    artifact_rec RECORD;
    new_session_id UUID;
BEGIN
    FOR artifact_rec IN SELECT id, title, created_at FROM artifacts WHERE session_id IS NULL
    LOOP
        new_session_id := gen_random_uuid();
        INSERT INTO sessions (id, title, created_by, status, created_at, updated_at)
        VALUES (new_session_id, 'Default Session for ' || artifact_rec.title, NULL, 'open', artifact_rec.created_at, artifact_rec.created_at);
        
        UPDATE artifacts SET session_id = new_session_id WHERE id = artifact_rec.id;
    END LOOP;
END $$;

-- Step 4: Make session_id required (NOT NULL) and add foreign key
ALTER TABLE artifacts ALTER COLUMN session_id SET NOT NULL;
ALTER TABLE artifacts ADD CONSTRAINT fk_artifacts_session_id FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE;

-- Step 5: Remove artifact_id from sessions table
ALTER TABLE sessions DROP COLUMN artifact_id;

-- Step 6: Update indexes
DROP INDEX IF EXISTS idx_sessions_artifact_id;
CREATE INDEX idx_artifacts_session_id ON artifacts(session_id);

-- === 000006_all_objects_belong_to_sessions.up.sql ===
-- Phase 3 Update: All objects belong to sessions
-- This migration adds session_id to materials, video_sources, and makes questions.session_id required

-- Step 1: Add session_id to materials table
ALTER TABLE materials ADD COLUMN session_id UUID;

-- Step 2: Populate session_id for existing materials from their artifacts
UPDATE materials m
SET session_id = a.session_id
FROM artifacts a
WHERE m.artifact_id = a.id;

-- Step 3: Make session_id required for materials and add foreign key
ALTER TABLE materials ALTER COLUMN session_id SET NOT NULL;
ALTER TABLE materials ADD CONSTRAINT fk_materials_session_id FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE;

-- Step 4: Add session_id to video_sources table
ALTER TABLE video_sources ADD COLUMN session_id UUID;

-- Step 5: Populate session_id for existing video_sources from their artifacts
UPDATE video_sources v
SET session_id = a.session_id
FROM artifacts a
WHERE v.artifact_id = a.id;

-- Step 6: Make session_id required for video_sources and add foreign key
ALTER TABLE video_sources ALTER COLUMN session_id SET NOT NULL;
ALTER TABLE video_sources ADD CONSTRAINT fk_video_sources_session_id FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE;

-- Step 7: Make questions.session_id required (populate from artifact if null)
UPDATE questions q
SET session_id = a.session_id
FROM artifacts a
WHERE q.artifact_id = a.id AND q.session_id IS NULL;

-- Step 8: Make session_id required for questions
ALTER TABLE questions ALTER COLUMN session_id SET NOT NULL;
ALTER TABLE questions ADD CONSTRAINT fk_questions_session_id FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE;

-- Step 9: Create indexes for session_id columns
CREATE INDEX idx_materials_session_id ON materials(session_id);
CREATE INDEX idx_video_sources_session_id ON video_sources(session_id);
CREATE INDEX idx_questions_session_id ON questions(session_id);

-- === 000007_add_playback_modes.up.sql ===
-- Migration: Add playback modes and video source enhancements
-- This adds support for direct MP4/WebM playback alongside embed playback

-- Add new columns to video_sources table
ALTER TABLE video_sources
ADD COLUMN playback_mode TEXT NOT NULL DEFAULT 'embed' CHECK (playback_mode IN ('embed', 'direct')),
ADD COLUMN embed_url TEXT NULL,
ADD COLUMN media_url TEXT NULL,
ADD COLUMN duration_seconds INT NULL,
ADD COLUMN poster_url TEXT NULL;

-- Backfill existing records: map video_url to embed_url
UPDATE video_sources
SET embed_url = video_url,
    playback_mode = 'embed'
WHERE embed_url IS NULL;

-- Add video_time_seconds to questions table for timestamped questions
ALTER TABLE questions
ADD COLUMN video_time_seconds INT NULL;

-- Add index for efficient querying by video_time_seconds
CREATE INDEX idx_questions_video_time_seconds ON questions(video_time_seconds);

-- === 000009_add_transcript_jobs.up.sql ===
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

-- === 000010_add_loom_password.up.sql ===
-- Migration: Add password field to transcript_jobs for password-protected Loom videos

ALTER TABLE transcript_jobs
ADD COLUMN IF NOT EXISTS loom_password TEXT NULL;

-- Note: Password is stored as plain text but only used during resolution
-- Security: Passwords are not logged and kept in-memory only during job processing

-- === 000011_add_answer_confirmed.up.sql ===
-- Add confirmed column to answers table
ALTER TABLE answers ADD COLUMN confirmed BOOLEAN NOT NULL DEFAULT false;

-- Create index for confirmed answers
CREATE INDEX idx_answers_confirmed ON answers(confirmed) WHERE confirmed = true;

-- === 000012_add_video_ingestion_fields.up.sql ===
-- Add video ingestion fields to video_sources table
-- This supports MP4 upload, direct URL ingestion, and better error tracking

-- Add source_type column (upload, direct_url, embed_url)
ALTER TABLE video_sources ADD COLUMN source_type TEXT NOT NULL DEFAULT 'embed_url' CHECK (source_type IN ('upload', 'direct_url', 'embed_url'));

-- Add stored_video_object_key column (path to uploaded/downloaded MP4 file)
ALTER TABLE video_sources ADD COLUMN stored_video_object_key TEXT NULL;

-- Add original_url column (original user-provided URL for reference)
ALTER TABLE video_sources ADD COLUMN original_url TEXT NULL;

-- Add failure_reason column (error message when ingestion/transcription fails)
ALTER TABLE video_sources ADD COLUMN failure_reason TEXT NULL;

-- Update transcript_status constraint to include 'processing' state
ALTER TABLE video_sources DROP CONSTRAINT IF EXISTS video_sources_transcript_status_check;
ALTER TABLE video_sources ADD CONSTRAINT video_sources_transcript_status_check 
    CHECK (transcript_status IN ('missing', 'pending', 'processing', 'ready', 'failed'));

-- Add indexes for efficient querying
CREATE INDEX idx_video_sources_source_type ON video_sources(source_type);
CREATE INDEX idx_video_sources_stored_video_object_key ON video_sources(stored_video_object_key) WHERE stored_video_object_key IS NOT NULL;

-- === 000013_zoom_and_session_source.up.sql ===
-- Zoom-centric: Session source, zoom_connections, video_sources VTT fields

-- Session: source_provider ('zoom' | 'upload'), source_reference_url (Zoom recording link)
ALTER TABLE sessions ADD COLUMN IF NOT EXISTS source_provider TEXT DEFAULT 'upload' CHECK (source_provider IN ('zoom', 'upload'));
ALTER TABLE sessions ADD COLUMN IF NOT EXISTS source_reference_url TEXT NULL;

-- Backfill existing sessions
UPDATE sessions SET source_provider = 'upload' WHERE source_provider IS NULL;

-- zoom_connections: OAuth tokens keyed by creator identity
CREATE TABLE IF NOT EXISTS zoom_connections (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    creator_identity_id TEXT NOT NULL UNIQUE,
    zoom_user_id TEXT NOT NULL,
    zoom_account_id TEXT NULL,
    zoom_user_email TEXT NULL,
    access_token_encrypted BYTEA NOT NULL,
    refresh_token_encrypted BYTEA NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_zoom_connections_creator_identity ON zoom_connections(creator_identity_id);

-- video_sources: optional raw VTT and normalized segments for Zoom transcripts
ALTER TABLE video_sources ADD COLUMN IF NOT EXISTS raw_vtt TEXT NULL;
ALTER TABLE video_sources ADD COLUMN IF NOT EXISTS transcript_segments JSONB NULL;

-- === 000014_add_zoom_transcription_source.up.sql ===
-- Allow transcription_source 'zoom_api' for Zoom-imported transcripts
ALTER TABLE video_sources DROP CONSTRAINT IF EXISTS video_sources_transcription_source_check;
ALTER TABLE video_sources ADD CONSTRAINT video_sources_transcription_source_check
  CHECK (transcription_source IS NULL OR transcription_source IN ('manual', 'loom_api', 'whisper', 'zoom_api'));

-- === 000015_add_ingestion_jobs.up.sql ===
-- ingestion_jobs: track Zoom (and future) import jobs per session
CREATE TABLE IF NOT EXISTS ingestion_jobs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id UUID NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    source TEXT NOT NULL DEFAULT 'zoom' CHECK (source IN ('zoom')),
    state TEXT NOT NULL DEFAULT 'queued' CHECK (state IN ('queued', 'fetching', 'ready', 'failed')),
    meeting_uuid TEXT NULL,
    instance_uuid TEXT NULL,
    last_error TEXT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_ingestion_jobs_session_id ON ingestion_jobs(session_id);

-- === 000016_add_transcripts.up.sql ===
-- transcripts: one transcript artifact per session (e.g. zoom, loom, whisper)
CREATE TABLE IF NOT EXISTS transcripts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id UUID NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    source TEXT NOT NULL DEFAULT 'zoom',
    language TEXT NULL,
    status TEXT NOT NULL DEFAULT 'parsing' CHECK (status IN ('parsing', 'ready', 'failed')),
    raw_text TEXT NULL,
    error_message TEXT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(session_id, source)
);

CREATE INDEX IF NOT EXISTS idx_transcripts_session_id ON transcripts(session_id);

-- transcript_segments: atomic units for retrieval and chunking
CREATE TABLE IF NOT EXISTS transcript_segments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    transcript_id UUID NOT NULL REFERENCES transcripts(id) ON DELETE CASCADE,
    session_id UUID NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    idx INT NOT NULL,
    start_ms INT NOT NULL,
    end_ms INT NOT NULL,
    text TEXT NOT NULL,
    speaker_label TEXT NULL,
    source_ref TEXT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(transcript_id, idx)
);

CREATE INDEX IF NOT EXISTS idx_transcript_segments_transcript_id ON transcript_segments(transcript_id);
CREATE INDEX IF NOT EXISTS idx_transcript_segments_session_id_idx ON transcript_segments(session_id, idx);

-- === 000017_session_chunks_embeddings.up.sql ===
-- Mission #3: session_chunks + session_chunk_embeddings for RAG
-- Embeddings stored as JSONB array of floats (no pgvector required); can switch to vector(1536) later.

-- session_chunks: retrievable documents for session-scoped RAG
CREATE TABLE IF NOT EXISTS session_chunks (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id UUID NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    source_type TEXT NOT NULL CHECK (source_type IN ('transcript', 'material')),
    source_id UUID NULL,
    chunk_idx INT NOT NULL,
    text TEXT NOT NULL,
    anchor_json JSONB NULL,
    content_hash TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(session_id, content_hash)
);

CREATE INDEX IF NOT EXISTS idx_session_chunks_session_id ON session_chunks(session_id);

-- session_chunk_embeddings: embeddings for similarity search (JSONB array of 1536 floats)
CREATE TABLE IF NOT EXISTS session_chunk_embeddings (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    chunk_id UUID NOT NULL REFERENCES session_chunks(id) ON DELETE CASCADE,
    session_id UUID NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    embedding_model TEXT NOT NULL,
    embedding JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_session_chunk_embeddings_chunk_id ON session_chunk_embeddings(chunk_id);
CREATE INDEX IF NOT EXISTS idx_session_chunk_embeddings_session_id ON session_chunk_embeddings(session_id);

-- Optional: session index status for "Indexing..." UI
ALTER TABLE sessions ADD COLUMN IF NOT EXISTS index_status TEXT DEFAULT 'none' CHECK (index_status IN ('none', 'building', 'ready', 'failed'));
ALTER TABLE sessions ADD COLUMN IF NOT EXISTS index_updated_at TIMESTAMPTZ NULL;

-- === 000018_add_material_views.up.sql ===
-- Track which materials each participant has seen (for "new document" unread marker)
CREATE TABLE material_views (
    session_id UUID NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    participant_ref TEXT NOT NULL,
    material_id UUID NOT NULL REFERENCES materials(id) ON DELETE CASCADE,
    seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (session_id, participant_ref, material_id)
);

CREATE INDEX idx_material_views_session_participant ON material_views(session_id, participant_ref);

-- === 000019_materials_title_error_processing.up.sql ===
-- Mission #6: materials title, error_message, and processing status
ALTER TABLE materials ADD COLUMN IF NOT EXISTS title TEXT NULL;
ALTER TABLE materials ADD COLUMN IF NOT EXISTS error_message TEXT NULL;

-- Allow text_status to include 'processing' (uploaded -> pending, processing -> processing, ready/failed unchanged)
ALTER TABLE materials DROP CONSTRAINT IF EXISTS materials_text_status_check;
ALTER TABLE materials ADD CONSTRAINT materials_text_status_check
    CHECK (text_status IN ('pending', 'processing', 'ready', 'failed'));

-- Backfill title from filename where title is null
UPDATE materials SET title = filename WHERE title IS NULL;

-- === 000020_session_processing_jobs.up.sql ===
-- Mission #4: Unified pipeline state machine for ingestion + indexing
-- session_processing_jobs: one job per session/source (Zoom import → transcript → chunk → embed)

CREATE TABLE IF NOT EXISTS session_processing_jobs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id UUID NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    source TEXT NOT NULL DEFAULT 'zoom' CHECK (source IN ('zoom')),
    state TEXT NOT NULL DEFAULT 'queued' CHECK (state IN (
        'queued', 'fetching', 'downloading', 'parsing', 'chunking', 'embedding',
        'waiting', 'ready', 'failed_transient', 'failed_permanent', 'canceled'
    )),
    stage TEXT NOT NULL DEFAULT 'fetch' CHECK (stage IN ('fetch', 'download', 'parse', 'chunk', 'embed', 'ready')),
    attempt_count INT NOT NULL DEFAULT 0,
    next_retry_at TIMESTAMPTZ NULL,
    last_error_code TEXT NULL,
    last_error_message TEXT NULL,
    locked_at TIMESTAMPTZ NULL,
    lock_owner TEXT NULL,
    meeting_uuid TEXT NULL,
    instance_uuid TEXT NULL,
    creator_identity TEXT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_session_processing_jobs_session_source
    ON session_processing_jobs(session_id, source);
CREATE INDEX IF NOT EXISTS idx_session_processing_jobs_state_retry
    ON session_processing_jobs(state, next_retry_at);
CREATE INDEX IF NOT EXISTS idx_session_processing_jobs_session_id
    ON session_processing_jobs(session_id);

-- Mirror latest job state on sessions for quick UI
ALTER TABLE sessions ADD COLUMN IF NOT EXISTS processing_state TEXT NULL;
ALTER TABLE sessions ADD COLUMN IF NOT EXISTS processing_updated_at TIMESTAMPTZ NULL;

-- === 000021_fix_office_document_kind.up.sql ===
-- Fix materials that were stored with kind 'other' but are Office Word documents.
-- Participant mode only shows items with kind 'document' under DOCUMENTS.
UPDATE materials
SET kind = 'document'
WHERE kind = 'other'
  AND (
    content_type LIKE '%wordprocessingml%'
    OR LOWER(filename) LIKE '%.docx'
    OR LOWER(filename) LIKE '%.doc'
  );

-- === 000022_users.up.sql ===
CREATE TABLE users (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  email text NOT NULL,
  display_name text NOT NULL,
  status text NOT NULL DEFAULT 'active',
  global_role text NOT NULL DEFAULT 'user',
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now(),
  last_login_at timestamptz NULL,
  CONSTRAINT users_email_unique UNIQUE (email),
  CONSTRAINT users_status_check CHECK (status IN ('active', 'disabled')),
  CONSTRAINT users_global_role_check CHECK (global_role IN ('admin', 'user'))
);

CREATE UNIQUE INDEX users_email_lower_idx ON users (lower(email));

-- === 000023_password_credentials.up.sql ===
CREATE TABLE password_credentials (
  user_id uuid PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
  password_hash text NOT NULL,
  password_updated_at timestamptz NOT NULL DEFAULT now(),
  created_at timestamptz NOT NULL DEFAULT now(),
  updated_at timestamptz NOT NULL DEFAULT now()
);

-- === 000024_login_sessions.up.sql ===
CREATE TABLE login_sessions (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  created_at timestamptz NOT NULL DEFAULT now(),
  expires_at timestamptz NOT NULL,
  revoked_at timestamptz NULL,
  ip text NULL,
  user_agent text NULL
);

CREATE INDEX login_sessions_user_id_idx ON login_sessions (user_id);
CREATE INDEX login_sessions_expires_at_idx ON login_sessions (expires_at);
CREATE INDEX login_sessions_active_idx ON login_sessions (id) WHERE revoked_at IS NULL;

-- === 000025_session_invitations.up.sql ===
CREATE TABLE session_invitations (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  session_id uuid NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
  user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  invited_by_user_id uuid NULL REFERENCES users(id) ON DELETE SET NULL,
  created_at timestamptz NOT NULL DEFAULT now(),
  UNIQUE(session_id, user_id)
);

CREATE INDEX session_invitations_session_id_idx ON session_invitations (session_id);
CREATE INDEX session_invitations_user_id_idx ON session_invitations (user_id);

-- === 000026_global_roles_three.up.sql ===
-- Add creator and participant roles; migrate existing 'user' to 'creator'.
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_global_role_check;
UPDATE users SET global_role = 'creator' WHERE global_role = 'user';
ALTER TABLE users ADD CONSTRAINT users_global_role_check CHECK (global_role IN ('admin', 'creator', 'participant'));
ALTER TABLE users ALTER COLUMN global_role SET DEFAULT 'creator';

-- === 000027_file_artifacts_r2.up.sql ===
-- File artifacts: R2-backed binary files (video, documents, images, exports).
-- Distinct from existing artifacts table (session container with title/description).

-- Ensure trigger function exists (may be missing if DB was created without 000001).
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$ language 'plpgsql';

CREATE TABLE file_artifacts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id UUID NULL REFERENCES sessions(id) ON DELETE SET NULL,
    owner_user_id UUID NULL REFERENCES users(id) ON DELETE SET NULL,
    kind TEXT NOT NULL CHECK (kind IN ('video', 'document', 'image', 'attachment', 'export', 'transcript_raw')),
    filename TEXT NULL,
    content_type TEXT NOT NULL,
    size_bytes BIGINT NULL,
    sha256 TEXT NULL,
    storage_provider TEXT NOT NULL DEFAULT 'r2',
    storage_bucket TEXT NOT NULL,
    storage_key TEXT NOT NULL UNIQUE,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'ready', 'failed')),
    failure_reason TEXT NULL,
    metadata_json JSONB NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_file_artifacts_session_id ON file_artifacts(session_id);
CREATE INDEX idx_file_artifacts_owner_user_id ON file_artifacts(owner_user_id);
CREATE INDEX idx_file_artifacts_storage_key ON file_artifacts(storage_key);
CREATE INDEX idx_file_artifacts_status ON file_artifacts(status);

CREATE TRIGGER update_file_artifacts_updated_at BEFORE UPDATE ON file_artifacts
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();

-- Sessions: link primary video to a file_artifact (R2) instead of streaming from Zoom.
ALTER TABLE sessions ADD COLUMN primary_video_artifact_id UUID NULL REFERENCES file_artifacts(id) ON DELETE SET NULL;
CREATE INDEX idx_sessions_primary_video_artifact_id ON sessions(primary_video_artifact_id);

