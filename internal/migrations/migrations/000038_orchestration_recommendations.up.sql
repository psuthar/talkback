CREATE TABLE IF NOT EXISTS orchestration_recommendations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id UUID NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    recommendation_type TEXT NOT NULL CHECK (recommendation_type IN (
        'unanswered_question',
        'missing_participant_input',
        'review_draft_answer',
        'decision_readiness'
    )),
    status TEXT NOT NULL DEFAULT 'new' CHECK (status IN (
        'new',
        'reviewed',
        'approved',
        'dismissed',
        'completed'
    )),
    summary TEXT NOT NULL,
    suggested_action TEXT NULL,
    evidence_json JSONB NOT NULL DEFAULT '[]'::jsonb,
    confidence_score REAL NULL CHECK (confidence_score >= 0 AND confidence_score <= 1),
    metadata_json JSONB NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_orchestration_recommendations_session_id
    ON orchestration_recommendations(session_id);
CREATE INDEX IF NOT EXISTS idx_orchestration_recommendations_status
    ON orchestration_recommendations(status);
CREATE INDEX IF NOT EXISTS idx_orchestration_recommendations_type
    ON orchestration_recommendations(recommendation_type);

DROP TRIGGER IF EXISTS update_orchestration_recommendations_updated_at ON orchestration_recommendations;
CREATE TRIGGER update_orchestration_recommendations_updated_at
    BEFORE UPDATE ON orchestration_recommendations
    FOR EACH ROW EXECUTE FUNCTION update_updated_at_column();
