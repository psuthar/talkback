CREATE TABLE IF NOT EXISTS orchestration_recommendation_status_audit (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id UUID NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    recommendation_id UUID NOT NULL REFERENCES orchestration_recommendations(id) ON DELETE CASCADE,
    actor_user_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    from_status TEXT NULL CHECK (from_status IS NULL OR from_status IN (
        'new',
        'reviewed',
        'approved',
        'dismissed',
        'completed'
    )),
    to_status TEXT NOT NULL CHECK (to_status IN (
        'new',
        'reviewed',
        'approved',
        'dismissed',
        'completed'
    )),
    client_surface TEXT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_orch_rec_status_audit_session_created
    ON orchestration_recommendation_status_audit (session_id, created_at DESC);
