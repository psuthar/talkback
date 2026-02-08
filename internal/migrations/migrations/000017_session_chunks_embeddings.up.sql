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
