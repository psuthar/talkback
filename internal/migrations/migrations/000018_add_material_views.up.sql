-- Track which materials each participant has seen (for "new document" unread marker)
CREATE TABLE material_views (
    session_id UUID NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    participant_ref TEXT NOT NULL,
    material_id UUID NOT NULL REFERENCES materials(id) ON DELETE CASCADE,
    seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (session_id, participant_ref, material_id)
);

CREATE INDEX idx_material_views_session_participant ON material_views(session_id, participant_ref);
