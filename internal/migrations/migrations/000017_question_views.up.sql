-- Track when a participant/viewer last expanded a question (for unread indicator).
-- participant_ref identifies the viewer (participant ref or creator email).
CREATE TABLE question_views (
    session_id UUID NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    participant_ref TEXT NOT NULL,
    question_id UUID NOT NULL REFERENCES questions(id) ON DELETE CASCADE,
    viewed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (session_id, participant_ref, question_id)
);
CREATE INDEX idx_question_views_session_participant ON question_views(session_id, participant_ref);
