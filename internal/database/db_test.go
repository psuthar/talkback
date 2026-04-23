package database

import (
	"context"
	"database/sql"
	"io/fs"
	"log"
	"os"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/google/uuid"
	_ "github.com/lib/pq"
	"github.com/psuthar/talkback/internal/migrations"
	"github.com/psuthar/talkback/internal/models"
	"github.com/psuthar/talkback/internal/test"
	"github.com/stretchr/testify/require"
)

func TestMain(m *testing.M) {
	sharedURL, cleanupDB, err := test.CreateSharedTestDB()
	if err != nil {
		log.Fatalf("Create shared test DB: %v", err)
	}
	if err := runTestMigrations(sharedURL); err != nil {
		_ = test.DropTestDatabaseURL(sharedURL)
		log.Fatalf("Run test migrations: %v", err)
	}
	originalURL := os.Getenv("DATABASE_URL")
	os.Setenv("DATABASE_URL", sharedURL)
	code := m.Run()
	if cleanupDB != nil {
		cleanupDB()
	}
	if originalURL != "" {
		os.Setenv("DATABASE_URL", originalURL)
	} else {
		os.Unsetenv("DATABASE_URL")
	}
	os.Exit(code)
}

// setupTestDB uses the shared test DB (created in TestMain). Each test must defer test.TruncateTables(t, db.Pool) and defer db.Close() so the next test has a clean state.
func setupTestDB(t *testing.T) *DB {
	t.Helper()
	db, err := New()
	require.NoError(t, err, "DATABASE_URL must be set (TestMain sets it from shared test DB)")
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

// createTestSession is a helper to create a test session for tests that need artifacts
func createTestSession(t *testing.T, db *DB, title string) *models.Session {
	t.Helper()
	ctx := context.Background()

	session := &models.Session{
		ID:     uuid.New(),
		Title:  title,
		Status: models.SessionStatusOpen,
	}

	err := db.CreateSession(ctx, session)
	require.NoError(t, err)
	return session
}

// createTestUser inserts a user with the given email and global role (for cross-session / membership tests).
func createTestUser(t *testing.T, db *DB, email string, role models.GlobalRole) *models.User {
	t.Helper()
	ctx := context.Background()
	u := &models.User{
		ID:          uuid.New(),
		Email:       email,
		DisplayName: "Test User",
		Status:      models.UserStatusActive,
		GlobalRole:  role,
	}
	err := db.CreateUser(ctx, u)
	require.NoError(t, err)
	return u
}

// createSessionWithCreator creates a session whose created_by matches creatorEmail (MCP access rules).
func createSessionWithCreator(t *testing.T, db *DB, title, creatorEmail string) *models.Session {
	t.Helper()
	ctx := context.Background()
	cb := creatorEmail
	s := &models.Session{
		ID:        uuid.New(),
		Title:     title,
		CreatedBy: &cb,
		Status:    models.SessionStatusOpen,
	}
	err := db.CreateSession(ctx, s)
	require.NoError(t, err)
	return s
}
