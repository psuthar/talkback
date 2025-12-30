package test

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// DBInterface defines the interface needed for test helpers
// This avoids import cycles by not importing the database package
type DBInterface interface {
	GetPool() *pgxpool.Pool
	Close()
}

// SetupTestDB creates a test database connection
// Returns the database URL and a cleanup function
func SetupTestDB(t *testing.T) (string, func()) {
	t.Helper()

	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		// Fallback to regular DATABASE_URL
		databaseURL = os.Getenv("DATABASE_URL")
		if databaseURL == "" {
			t.Fatal("TEST_DATABASE_URL or DATABASE_URL must be set")
		}
	}

	return databaseURL, func() {}
}

// TruncateTables truncates all tables for a clean test state
// Note: This should only be called when no other connections are using these tables
// to avoid deadlocks. Tests should run sequentially (-p 1) or use transactions.
func TruncateTables(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	ctx := context.Background()

	// Use a transaction to ensure atomic truncation
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("Failed to begin transaction: %v", err)
	}
	defer tx.Rollback(ctx)

	tables := []string{"video_sources", "materials", "artifacts"}
	for _, table := range tables {
		_, err := tx.Exec(ctx, fmt.Sprintf("TRUNCATE TABLE %s CASCADE", table))
		if err != nil {
			t.Fatalf("Failed to truncate table %s: %v", table, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("Failed to commit truncation: %v", err)
	}
}
