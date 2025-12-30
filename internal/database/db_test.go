package database

import (
	"database/sql"
	"io/fs"
	"os"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	_ "github.com/lib/pq"
	"github.com/psuthar/talkback/internal/migrations"
	"github.com/stretchr/testify/require"
)

// setupTestDB is a shared helper for all database tests
// It automatically runs migrations to ensure the schema is up to date
// Note: Database tests should not run in parallel to avoid deadlocks
func setupTestDB(t *testing.T) *DB {
	t.Helper()

	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		databaseURL = os.Getenv("DATABASE_URL")
		if databaseURL == "" {
			t.Fatal("TEST_DATABASE_URL or DATABASE_URL must be set")
		}
	}

	originalURL := os.Getenv("DATABASE_URL")
	os.Setenv("DATABASE_URL", databaseURL)
	t.Cleanup(func() {
		if originalURL != "" {
			os.Setenv("DATABASE_URL", originalURL)
		} else {
			os.Unsetenv("DATABASE_URL")
		}
	})

	// Run migrations before setting up the database connection
	err := runTestMigrations(databaseURL)
	require.NoError(t, err, "Failed to run test migrations")

	db, err := New()
	require.NoError(t, err)
	return db
}

// runTestMigrations runs migrations for tests using embedded migrations
func runTestMigrations(databaseURL string) error {
	// Get migrations from embedded filesystem
	migrationsSubFS, err := fs.Sub(migrations.MigrationsFS, "migrations")
	if err != nil {
		return err
	}

	// Create iofs driver from embedded filesystem
	sourceDriver, err := iofs.New(migrationsSubFS, ".")
	if err != nil {
		return err
	}

	// Open database connection using database/sql (required by golang-migrate)
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return err
	}
	defer db.Close()

	// Create postgres driver
	postgresDriver, err := postgres.WithInstance(db, &postgres.Config{})
	if err != nil {
		return err
	}

	// Create migrate instance with embedded source
	m, err := migrate.NewWithInstance("iofs", sourceDriver, "postgres", postgresDriver)
	if err != nil {
		return err
	}

	// Run migrations
	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return err
	}

	return nil
}
