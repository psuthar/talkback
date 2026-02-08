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
