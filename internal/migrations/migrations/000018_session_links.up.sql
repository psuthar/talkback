-- Session links: URLs added by creator; verified and extracted for RAG.
CREATE TABLE session_links (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id UUID NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    url TEXT NOT NULL,
    title TEXT,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'verified', 'failed')),
    extracted_text TEXT,
    error_message TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_session_links_session_id ON session_links(session_id);

-- Allow session_chunks to reference links (source_type = 'link', source_id = session_links.id).
ALTER TABLE session_chunks DROP CONSTRAINT IF EXISTS session_chunks_source_type_check;
ALTER TABLE session_chunks ADD CONSTRAINT session_chunks_source_type_check
    CHECK (source_type IN ('transcript', 'material', 'link'));
