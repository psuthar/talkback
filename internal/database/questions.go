package database

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/psuthar/talkback/internal/models"
)

// CreateQuestion creates a new question
func (db *DB) CreateQuestion(ctx context.Context, question *models.Question) error {
	query := `
		INSERT INTO questions (id, artifact_id, asked_by, question_text, question_source)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING created_at
	`

	err := db.Pool.QueryRow(ctx, query,
		question.ID,
		question.ArtifactID,
		question.AskedBy,
		question.QuestionText,
		question.QuestionSource,
	).Scan(&question.CreatedAt)
	if err != nil {
		return fmt.Errorf("failed to create question: %w", err)
	}

	return nil
}

// CreateAnswer creates a new answer
func (db *DB) CreateAnswer(ctx context.Context, answer *models.Answer) error {
	// Convert citations to JSONB
	citationsJSON, err := json.Marshal(answer.Citations)
	if err != nil {
		return fmt.Errorf("failed to marshal citations: %w", err)
	}

	query := `
		INSERT INTO answers (id, question_id, answer_text, answer_status, confidence, citations, model)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING created_at
	`

	err = db.Pool.QueryRow(ctx, query,
		answer.ID,
		answer.QuestionID,
		answer.AnswerText,
		answer.AnswerStatus,
		answer.Confidence,
		citationsJSON,
		answer.Model,
	).Scan(&answer.CreatedAt)
	if err != nil {
		return fmt.Errorf("failed to create answer: %w", err)
	}

	return nil
}

// GetQuestionByID retrieves a question by ID
func (db *DB) GetQuestionByID(ctx context.Context, questionID uuid.UUID) (*models.Question, error) {
	question := &models.Question{}
	query := `
		SELECT id, artifact_id, asked_by, question_text, question_source, created_at
		FROM questions
		WHERE id = $1
	`

	err := db.Pool.QueryRow(ctx, query, questionID).Scan(
		&question.ID,
		&question.ArtifactID,
		&question.AskedBy,
		&question.QuestionText,
		&question.QuestionSource,
		&question.CreatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("question not found: %w", err)
		}
		return nil, fmt.Errorf("failed to get question: %w", err)
	}

	return question, nil
}

// GetQuestionsByArtifactID retrieves questions for an artifact with their latest answers
func (db *DB) GetQuestionsByArtifactID(ctx context.Context, artifactID uuid.UUID, limit int) ([]*models.Question, []*models.Answer, error) {
	if limit <= 0 {
		limit = 20
	}

	query := `
		SELECT 
			q.id, q.artifact_id, q.asked_by, q.question_text, q.question_source, q.created_at,
			a.id, a.question_id, a.answer_text, a.answer_status, a.confidence, a.citations, a.model, a.created_at
		FROM questions q
		LEFT JOIN LATERAL (
			SELECT id, question_id, answer_text, answer_status, confidence, citations, model, created_at
			FROM answers
			WHERE question_id = q.id
			ORDER BY created_at DESC
			LIMIT 1
		) a ON true
		WHERE q.artifact_id = $1
		ORDER BY q.created_at DESC
		LIMIT $2
	`

	rows, err := db.Pool.Query(ctx, query, artifactID, limit)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to query questions: %w", err)
	}
	defer rows.Close()

	var questions []*models.Question
	var answers []*models.Answer

	for rows.Next() {
		q := &models.Question{}
		var answerID *uuid.UUID
		var answerQuestionID *uuid.UUID
		var answerText *string
		var answerStatus *string
		var answerConfidence *float32
		var answerCitationsJSON []byte
		var answerModel *string
		var answerCreatedAt *time.Time

		err := rows.Scan(
			&q.ID,
			&q.ArtifactID,
			&q.AskedBy,
			&q.QuestionText,
			&q.QuestionSource,
			&q.CreatedAt,
			&answerID,
			&answerQuestionID,
			&answerText,
			&answerStatus,
			&answerConfidence,
			&answerCitationsJSON,
			&answerModel,
			&answerCreatedAt,
		)
		if err != nil {
			return nil, nil, fmt.Errorf("failed to scan question: %w", err)
		}

		questions = append(questions, q)

		// If answer exists, parse it
		if answerID != nil {
			var citations []models.Citation
			if len(answerCitationsJSON) > 0 {
				if err := json.Unmarshal(answerCitationsJSON, &citations); err != nil {
					return nil, nil, fmt.Errorf("failed to unmarshal citations: %w", err)
				}
			}

			answer := &models.Answer{
				ID:           *answerID,
				QuestionID:   *answerQuestionID,
				AnswerText:   *answerText,
				AnswerStatus: models.AnswerStatus(*answerStatus),
				Confidence:   *answerConfidence,
				Citations:    citations,
				Model:        answerModel,
				CreatedAt:    *answerCreatedAt,
			}
			answers = append(answers, answer)
		}
	}

	return questions, answers, rows.Err()
}

// GetLatestAnswerByQuestionID retrieves the latest answer for a question
func (db *DB) GetLatestAnswerByQuestionID(ctx context.Context, questionID uuid.UUID) (*models.Answer, error) {
	answer := &models.Answer{}
	var citationsJSON []byte

	query := `
		SELECT id, question_id, answer_text, answer_status, confidence, citations, model, created_at
		FROM answers
		WHERE question_id = $1
		ORDER BY created_at DESC
		LIMIT 1
	`

	err := db.Pool.QueryRow(ctx, query, questionID).Scan(
		&answer.ID,
		&answer.QuestionID,
		&answer.AnswerText,
		&answer.AnswerStatus,
		&answer.Confidence,
		&citationsJSON,
		&answer.Model,
		&answer.CreatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil // No answer yet
		}
		return nil, fmt.Errorf("failed to get answer: %w", err)
	}

	// Parse citations
	if len(citationsJSON) > 0 {
		if err := json.Unmarshal(citationsJSON, &answer.Citations); err != nil {
			return nil, fmt.Errorf("failed to unmarshal citations: %w", err)
		}
	}

	return answer, nil
}

// FindExistingQuestionByText finds an existing question with the same text for the same artifact
// Returns the question and its latest answer if found
func (db *DB) FindExistingQuestionByText(ctx context.Context, artifactID uuid.UUID, questionText string) (*models.Question, *models.Answer, error) {
	// Normalize question text (trim and lowercase for comparison)
	normalizedText := strings.TrimSpace(strings.ToLower(questionText))
	
	query := `
		SELECT 
			q.id, q.artifact_id, q.asked_by, q.question_text, q.question_source, q.created_at,
			a.id, a.question_id, a.answer_text, a.answer_status, a.confidence, a.citations, a.model, a.created_at
		FROM questions q
		LEFT JOIN LATERAL (
			SELECT id, question_id, answer_text, answer_status, confidence, citations, model, created_at
			FROM answers
			WHERE question_id = q.id
			ORDER BY created_at DESC
			LIMIT 1
		) a ON true
		WHERE q.artifact_id = $1 
			AND LOWER(TRIM(q.question_text)) = $2
		ORDER BY q.created_at DESC
		LIMIT 1
	`

	question := &models.Question{}
	var answerID *uuid.UUID
	var answerQuestionID *uuid.UUID
	var answerText *string
	var answerStatus *string
	var answerConfidence *float32
	var answerCitationsJSON []byte
	var answerModel *string
	var answerCreatedAt *time.Time

	err := db.Pool.QueryRow(ctx, query, artifactID, normalizedText).Scan(
		&question.ID,
		&question.ArtifactID,
		&question.AskedBy,
		&question.QuestionText,
		&question.QuestionSource,
		&question.CreatedAt,
		&answerID,
		&answerQuestionID,
		&answerText,
		&answerStatus,
		&answerConfidence,
		&answerCitationsJSON,
		&answerModel,
		&answerCreatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil, nil // No existing question found
		}
		return nil, nil, fmt.Errorf("failed to find existing question: %w", err)
	}

	// If answer exists, parse it
	var answer *models.Answer
	if answerID != nil {
		var citations []models.Citation
		if len(answerCitationsJSON) > 0 {
			if err := json.Unmarshal(answerCitationsJSON, &citations); err != nil {
				return nil, nil, fmt.Errorf("failed to unmarshal citations: %w", err)
			}
		}

		answer = &models.Answer{
			ID:           *answerID,
			QuestionID:   *answerQuestionID,
			AnswerText:   *answerText,
			AnswerStatus: models.AnswerStatus(*answerStatus),
			Confidence:   *answerConfidence,
			Citations:    citations,
			Model:        answerModel,
			CreatedAt:    *answerCreatedAt,
		}
	}

	return question, answer, nil
}
