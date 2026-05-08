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

// SCRUM-357: import_jobs DB layer for SCRUM-350 template-driven session
// imports. Mirrors the session_processing_jobs pattern but persists the
// template descriptor and per-element runtime state.

// CreateImportJob inserts a new job in the queued state. The session_id is
// optional at create time — the worker fills it in once the destination
// session row exists.
func (db *DB) CreateImportJob(ctx context.Context, job *models.ImportJob) error {
	if job.ID == uuid.Nil {
		job.ID = uuid.New()
	}
	if job.State == "" {
		job.State = models.ImportJobStateQueued
	}
	if job.ElementsState == nil {
		job.ElementsState = map[string]models.ImportElementResult{}
	}
	elementsJSON, err := json.Marshal(job.ElementsState)
	if err != nil {
		return fmt.Errorf("marshal elements_state: %w", err)
	}
	descriptor := job.TemplateDescriptor
	if len(descriptor) == 0 {
		descriptor = json.RawMessage("{}")
	}
	query := `
		INSERT INTO import_jobs (id, session_id, user_id, state, template_descriptor, elements_state, error_message)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING created_at
	`
	return db.Pool.QueryRow(ctx, query,
		job.ID,
		job.SessionID,
		job.UserID,
		string(job.State),
		descriptor,
		elementsJSON,
		job.ErrorMessage,
	).Scan(&job.CreatedAt)
}

// GetImportJob retrieves an import job by id. Returns nil, nil when not
// found.
func (db *DB) GetImportJob(ctx context.Context, id uuid.UUID) (*models.ImportJob, error) {
	query := `
		SELECT id, session_id, user_id, state, template_descriptor, elements_state, error_message,
			created_at, started_at, completed_at
		FROM import_jobs
		WHERE id = $1
	`
	var job models.ImportJob
	var stateStr string
	var descriptor []byte
	var elementsStateBytes []byte
	err := db.Pool.QueryRow(ctx, query, id).Scan(
		&job.ID,
		&job.SessionID,
		&job.UserID,
		&stateStr,
		&descriptor,
		&elementsStateBytes,
		&job.ErrorMessage,
		&job.CreatedAt,
		&job.StartedAt,
		&job.CompletedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get import_job: %w", err)
	}
	job.State = models.ImportJobState(stateStr)
	job.TemplateDescriptor = json.RawMessage(descriptor)
	if len(elementsStateBytes) > 0 {
		if err := json.Unmarshal(elementsStateBytes, &job.ElementsState); err != nil {
			return nil, fmt.Errorf("unmarshal elements_state: %w", err)
		}
	}
	return &job, nil
}

// UpdateImportJobState transitions a job's state and stamps started_at /
// completed_at where appropriate.
func (db *DB) UpdateImportJobState(ctx context.Context, id uuid.UUID, newState models.ImportJobState, errorMessage *string) error {
	now := time.Now()
	var startedAt, completedAt *time.Time
	switch newState {
	case models.ImportJobStateRunning:
		startedAt = &now
	case models.ImportJobStateSucceeded, models.ImportJobStatePartial, models.ImportJobStateFailed:
		completedAt = &now
	}
	query := `
		UPDATE import_jobs
		SET state = $1,
		    error_message = COALESCE($2, error_message),
		    started_at = COALESCE($3, started_at),
		    completed_at = COALESCE($4, completed_at)
		WHERE id = $5
	`
	tag, err := db.Pool.Exec(ctx, query, string(newState), errorMessage, startedAt, completedAt, id)
	if err != nil {
		return fmt.Errorf("update import_job state: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("import_job not found: %s", id)
	}
	return nil
}

// SetImportJobSessionID sets session_id on a job once the worker has created
// the destination session.
func (db *DB) SetImportJobSessionID(ctx context.Context, id, sessionID uuid.UUID) error {
	tag, err := db.Pool.Exec(ctx, `UPDATE import_jobs SET session_id = $1 WHERE id = $2`, sessionID, id)
	if err != nil {
		return fmt.Errorf("set import_job session_id: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("import_job not found: %s", id)
	}
	return nil
}

// SetImportJobElement sets one element's per-element state in elements_state.
func (db *DB) SetImportJobElement(ctx context.Context, id uuid.UUID, elementID string, result models.ImportElementResult) error {
	resJSON, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("marshal element result: %w", err)
	}
	// jsonb_set(elements_state, '{<element_id>}', '<result>'::jsonb, true).
	// The path is a TEXT[] literal so we build it carefully with a parameter.
	tag, err := db.Pool.Exec(ctx,
		`UPDATE import_jobs SET elements_state = jsonb_set(elements_state, ARRAY[$1], $2::jsonb, true) WHERE id = $3`,
		elementID, string(resJSON), id,
	)
	if err != nil {
		return fmt.Errorf("set import_job element: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("import_job not found: %s", id)
	}
	return nil
}
