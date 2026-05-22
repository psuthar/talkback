-- SCRUM-558: extend session_chunks.source_type CHECK to allow 'link' and 'session_metadata'.
-- 'link' has been a silent latent bug since link-chunk indexing was added (the constraint
-- in the baseline migration was never updated). 'session_metadata' is new in this ticket.
ALTER TABLE session_chunks DROP CONSTRAINT IF EXISTS session_chunks_source_type_check;
ALTER TABLE session_chunks ADD CONSTRAINT session_chunks_source_type_check
    CHECK (source_type IN ('transcript', 'material', 'link', 'session_metadata'));
