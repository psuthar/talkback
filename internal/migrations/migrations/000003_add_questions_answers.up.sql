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
