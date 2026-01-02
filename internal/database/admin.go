package database

import (
	"context"
	"fmt"
)

// DeleteAllArtifacts deletes all artifacts (cascade will handle related records)
func (db *DB) DeleteAllArtifacts(ctx context.Context) (int, error) {
	query := `DELETE FROM artifacts`
	result, err := db.Pool.Exec(ctx, query)
	if err != nil {
		return 0, fmt.Errorf("failed to delete artifacts: %w", err)
	}
	return int(result.RowsAffected()), nil
}

// DeleteAllMaterials deletes all materials
func (db *DB) DeleteAllMaterials(ctx context.Context) (int, error) {
	query := `DELETE FROM materials`
	result, err := db.Pool.Exec(ctx, query)
	if err != nil {
		return 0, fmt.Errorf("failed to delete materials: %w", err)
	}
	return int(result.RowsAffected()), nil
}

// DeleteAllVideoSources deletes all video sources
func (db *DB) DeleteAllVideoSources(ctx context.Context) (int, error) {
	query := `DELETE FROM video_sources`
	result, err := db.Pool.Exec(ctx, query)
	if err != nil {
		return 0, fmt.Errorf("failed to delete video sources: %w", err)
	}
	return int(result.RowsAffected()), nil
}

// DeleteAllQuestions deletes all questions (cascade will handle answers)
func (db *DB) DeleteAllQuestions(ctx context.Context) (int, error) {
	query := `DELETE FROM questions`
	result, err := db.Pool.Exec(ctx, query)
	if err != nil {
		return 0, fmt.Errorf("failed to delete questions: %w", err)
	}
	return int(result.RowsAffected()), nil
}

// DeleteAllAnswers deletes all answers
func (db *DB) DeleteAllAnswers(ctx context.Context) (int, error) {
	query := `DELETE FROM answers`
	result, err := db.Pool.Exec(ctx, query)
	if err != nil {
		return 0, fmt.Errorf("failed to delete answers: %w", err)
	}
	return int(result.RowsAffected()), nil
}
