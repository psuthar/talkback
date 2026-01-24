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
