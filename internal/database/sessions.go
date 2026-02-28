package database

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/psuthar/talkback/internal/models"
)

// CreateSession creates a new session
func (db *DB) CreateSession(ctx context.Context, session *models.Session) error {
	sourceProvider := string(session.SourceProvider)
	if sourceProvider == "" {
		sourceProvider = string(models.SessionSourceUpload)
	}
	query := `
		INSERT INTO sessions (id, title, created_by, status, source_provider, source_reference_url)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING created_at, updated_at
	`

	err := db.Pool.QueryRow(ctx, query,
		session.ID,
		session.Title,
		session.CreatedBy,
		session.Status,
		sourceProvider,
		session.SourceReferenceURL,
	).Scan(&session.CreatedAt, &session.UpdatedAt)
	if err != nil {
		return fmt.Errorf("failed to create session: %w", err)
	}

	return nil
}

// GetSession retrieves a session by ID
func (db *DB) GetSession(ctx context.Context, sessionID uuid.UUID) (*models.Session, error) {
	session := &models.Session{}
	var sourceProviderStr, sourceRefURL, processingState *string
	var indexUpdatedAt, processingUpdatedAt *time.Time
	query := `
		SELECT id, title, created_by, status, source_provider, source_reference_url, primary_video_artifact_id,
			COALESCE(index_status, 'none'), index_updated_at, processing_state, processing_updated_at,
			created_at, updated_at
		FROM sessions
		WHERE id = $1
	`

	err := db.Pool.QueryRow(ctx, query, sessionID).Scan(
		&session.ID,
		&session.Title,
		&session.CreatedBy,
		&session.Status,
		&sourceProviderStr,
		&sourceRefURL,
		&session.PrimaryVideoArtifactID,
		&session.IndexStatus,
		&indexUpdatedAt,
		&processingState,
		&processingUpdatedAt,
		&session.CreatedAt,
		&session.UpdatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("session not found: %w", err)
		}
		return nil, fmt.Errorf("failed to get session: %w", err)
	}
	if sourceProviderStr != nil {
		session.SourceProvider = models.SessionSourceProvider(*sourceProviderStr)
	}
	session.SourceReferenceURL = sourceRefURL
	session.IndexUpdatedAt = indexUpdatedAt
	if processingState != nil {
		session.ProcessingState = *processingState
	}
	session.ProcessingUpdatedAt = processingUpdatedAt

	return session, nil
}

// ListSessionsByCreatedBy returns sessions where created_by equals the given string, ordered by updated_at DESC.
func (db *DB) ListSessionsByCreatedBy(ctx context.Context, createdBy string) ([]*models.Session, error) {
	query := `
		SELECT id, title, created_by, status, source_provider, source_reference_url, primary_video_artifact_id,
			COALESCE(index_status, 'none'), index_updated_at, processing_state, processing_updated_at,
			created_at, updated_at
		FROM sessions
		WHERE created_by = $1
		ORDER BY updated_at DESC
	`
	rows, err := db.Pool.Query(ctx, query, createdBy)
	if err != nil {
		return nil, fmt.Errorf("failed to list sessions by created_by: %w", err)
	}
	defer rows.Close()
	return scanSessionRows(rows)
}

// ListSessionsForInvitedUser returns sessions the user is invited to (via session_invitations), ordered by updated_at DESC.
func (db *DB) ListSessionsForInvitedUser(ctx context.Context, userID uuid.UUID) ([]*models.Session, error) {
	query := `
		SELECT s.id, s.title, s.created_by, s.status, s.source_provider, s.source_reference_url, s.primary_video_artifact_id,
			COALESCE(s.index_status, 'none'), s.index_updated_at, s.processing_state, s.processing_updated_at,
			s.created_at, s.updated_at
		FROM sessions s
		INNER JOIN session_invitations si ON s.id = si.session_id
		WHERE si.user_id = $1
		ORDER BY s.updated_at DESC
	`
	rows, err := db.Pool.Query(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to list sessions for invited user: %w", err)
	}
	defer rows.Close()
	return scanSessionRows(rows)
}

// ListAllSessions returns all sessions ordered by updated_at DESC (admin).
func (db *DB) ListAllSessions(ctx context.Context) ([]*models.Session, error) {
	query := `
		SELECT id, title, created_by, status, source_provider, source_reference_url, primary_video_artifact_id,
			COALESCE(index_status, 'none'), index_updated_at, processing_state, processing_updated_at,
			created_at, updated_at
		FROM sessions
		ORDER BY updated_at DESC
	`
	rows, err := db.Pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to list all sessions: %w", err)
	}
	defer rows.Close()
	return scanSessionRows(rows)
}

// scanSessionRows scans session rows (shared by ListSessionsByCreatedBy, ListSessionsForInvitedUser, ListAllSessions).
func scanSessionRows(rows interface {
	Next() bool
	Scan(dest ...interface{}) error
	Err() error
}) ([]*models.Session, error) {
	var sessions []*models.Session
	for rows.Next() {
		session := &models.Session{}
		var sourceProviderStr, sourceRefURL, processingState *string
		var indexUpdatedAt, processingUpdatedAt *time.Time
		err := rows.Scan(
			&session.ID,
			&session.Title,
			&session.CreatedBy,
			&session.Status,
			&sourceProviderStr,
			&sourceRefURL,
			&session.PrimaryVideoArtifactID,
			&session.IndexStatus,
			&indexUpdatedAt,
			&processingState,
			&processingUpdatedAt,
			&session.CreatedAt,
			&session.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan session: %w", err)
		}
		if sourceProviderStr != nil {
			session.SourceProvider = models.SessionSourceProvider(*sourceProviderStr)
		}
		session.SourceReferenceURL = sourceRefURL
		session.IndexUpdatedAt = indexUpdatedAt
		if processingState != nil {
			session.ProcessingState = *processingState
		}
		session.ProcessingUpdatedAt = processingUpdatedAt
		sessions = append(sessions, session)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterating sessions: %w", err)
	}
	if sessions == nil {
		sessions = []*models.Session{}
	}
	return sessions, nil
}

// GetArtifactsBySessionID retrieves all artifacts for a session
func (db *DB) GetArtifactsBySessionID(ctx context.Context, sessionID uuid.UUID) ([]*models.Artifact, error) {
	query := `
		SELECT id, session_id, title, description, status, created_at, updated_at
		FROM artifacts
		WHERE session_id = $1
		ORDER BY created_at DESC
	`

	rows, err := db.Pool.Query(ctx, query, sessionID)
	if err != nil {
		return nil, fmt.Errorf("failed to query artifacts: %w", err)
	}
	defer rows.Close()

	var artifacts []*models.Artifact
	for rows.Next() {
		artifact := &models.Artifact{}
		err := rows.Scan(
			&artifact.ID,
			&artifact.SessionID,
			&artifact.Title,
			&artifact.Description,
			&artifact.Status,
			&artifact.CreatedAt,
			&artifact.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan artifact: %w", err)
		}
		artifacts = append(artifacts, artifact)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating artifacts: %w", err)
	}

	// Always return a non-nil slice, even if empty
	if artifacts == nil {
		artifacts = []*models.Artifact{}
	}

	return artifacts, nil
}

// UpsertSessionParticipant creates or updates a session participant
func (db *DB) UpsertSessionParticipant(ctx context.Context, sessionID uuid.UUID, participantRef string) (*models.SessionParticipant, error) {
	query := `
		INSERT INTO session_participants (id, session_id, participant_ref, joined_at, last_seen_at, watch_progress)
		VALUES (gen_random_uuid(), $1, $2, now(), now(), 0)
		ON CONFLICT (session_id, participant_ref) 
		DO UPDATE SET 
			last_seen_at = now(),
			watch_progress = session_participants.watch_progress
		RETURNING id, session_id, participant_ref, joined_at, last_seen_at, watch_progress
	`

	participant := &models.SessionParticipant{}
	err := db.Pool.QueryRow(ctx, query, sessionID, participantRef).Scan(
		&participant.ID,
		&participant.SessionID,
		&participant.ParticipantRef,
		&participant.JoinedAt,
		&participant.LastSeenAt,
		&participant.WatchProgress,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to upsert participant: %w", err)
	}

	return participant, nil
}

// CreateSessionEvent creates a new session event
func (db *DB) CreateSessionEvent(ctx context.Context, event *models.SessionEvent) error {
	// Convert payload map to JSONB
	payloadJSON, err := json.Marshal(event.Payload)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	query := `
		INSERT INTO session_events (id, session_id, participant_ref, event_type, video_time_seconds, payload)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING created_at
	`

	err = db.Pool.QueryRow(ctx, query,
		event.ID,
		event.SessionID,
		event.ParticipantRef,
		event.EventType,
		event.VideoTimeSeconds,
		payloadJSON,
	).Scan(&event.CreatedAt)
	if err != nil {
		return fmt.Errorf("failed to create session event: %w", err)
	}

	return nil
}

// GetQuestionsBySessionID retrieves questions for a session with their latest answers
func (db *DB) GetQuestionsBySessionID(ctx context.Context, sessionID uuid.UUID, limit int) ([]*models.Question, []*models.Answer, error) {
	if limit <= 0 {
		limit = 20
	}

	query := `
		SELECT 
			q.id, q.artifact_id, q.session_id, q.asked_by, q.question_text, q.question_source, q.video_time_seconds, q.created_at,
			a.id, a.question_id, a.answer_text, a.answer_status, a.confidence, a.citations, a.model, a.confirmed, a.created_at
		FROM questions q
		LEFT JOIN LATERAL (
			SELECT id, question_id, answer_text, answer_status, confidence, citations, model, confirmed, created_at
			FROM answers
			WHERE question_id = q.id
			ORDER BY created_at DESC
			LIMIT 1
		) a ON true
		WHERE q.session_id = $1
		ORDER BY q.created_at DESC
		LIMIT $2
	`

	rows, err := db.Pool.Query(ctx, query, sessionID, limit)
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
		var answerConfirmed *bool
		var answerCreatedAt *time.Time

		err := rows.Scan(
			&q.ID,
			&q.ArtifactID,
			&q.SessionID,
			&q.AskedBy,
			&q.QuestionText,
			&q.QuestionSource,
			&q.VideoTimeSeconds,
			&q.CreatedAt,
			&answerID,
			&answerQuestionID,
			&answerText,
			&answerStatus,
			&answerConfidence,
			&answerCitationsJSON,
			&answerModel,
			&answerConfirmed,
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
				Confirmed:    *answerConfirmed,
				CreatedAt:    *answerCreatedAt,
			}
			answers = append(answers, answer)
		}
	}

	return questions, answers, rows.Err()
}

// UpdateSessionStatus updates the status of a session (e.g., close it)
func (db *DB) UpdateSessionStatus(ctx context.Context, sessionID uuid.UUID, status models.SessionStatus) error {
	query := `
		UPDATE sessions
		SET status = $1, updated_at = now()
		WHERE id = $2
	`

	result, err := db.Pool.Exec(ctx, query, status, sessionID)
	if err != nil {
		return fmt.Errorf("failed to update session status: %w", err)
	}

	// Check if any rows were updated
	if result.RowsAffected() == 0 {
		return fmt.Errorf("failed to update session status: session not found")
	}

	return nil
}

// GetSessionTimeline retrieves the timeline for a session (ordered questions and answers)
// This returns questions and answers in chronological order (oldest first) for timeline replay
func (db *DB) GetSessionTimeline(ctx context.Context, sessionID uuid.UUID, limit int) ([]*models.Question, []*models.Answer, error) {
	if limit <= 0 {
		limit = 100 // Default to more for timeline (can be large for long sessions)
	}

	query := `
		SELECT 
			q.id, q.artifact_id, q.session_id, q.asked_by, q.question_text, q.question_source, q.video_time_seconds, q.created_at,
			a.id, a.question_id, a.answer_text, a.answer_status, a.confidence, a.citations, a.model, a.created_at
		FROM questions q
		LEFT JOIN LATERAL (
			SELECT id, question_id, answer_text, answer_status, confidence, citations, model, created_at
			FROM answers
			WHERE question_id = q.id
			ORDER BY created_at DESC
			LIMIT 1
		) a ON true
		WHERE q.session_id = $1
		ORDER BY q.created_at ASC
		LIMIT $2
	`

	rows, err := db.Pool.Query(ctx, query, sessionID, limit)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to query session timeline: %w", err)
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
			&q.SessionID,
			&q.AskedBy,
			&q.QuestionText,
			&q.QuestionSource,
			&q.VideoTimeSeconds,
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
			return nil, nil, fmt.Errorf("failed to scan timeline question: %w", err)
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
