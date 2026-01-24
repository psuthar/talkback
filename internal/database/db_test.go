package database

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
	"github.com/psuthar/talkback/internal/models"
	"github.com/psuthar/talkback/internal/migrations"
	"github.com/psuthar/talkback/internal/test"
	"github.com/stretchr/testify/require"
)

// setupTestDB is a shared helper for all database tests
// It automatically drops/recreates the test database and runs migrations
// Note: Database tests should not run in parallel to avoid deadlocks
func setupTestDB(t *testing.T) *DB {
	t.Helper()

	// Load .env.test if it exists (try multiple possible paths)
	_ = godotenv.Load(".env.test")
	_ = godotenv.Load("../.env.test")
	_ = godotenv.Load("../../.env.test")

	databaseURL, cleanupDB := test.SetupTestDB(t)
	t.Cleanup(func() { cleanupDB() })

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

// isSafeDatabaseName checks if a database name contains only safe characters
func isSafeDatabaseName(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-') {
			return false
		}
	}
	return true
}

// ensureTestDatabaseForDBTests drops and recreates the test database
// This is a duplicate of the function in internal/test to avoid import cycles
func ensureTestDatabaseForDBTests(t *testing.T, testDatabaseURL string) {
	t.Helper()

	// Parse the test database URL to extract components
	parsedURL, err := url.Parse(testDatabaseURL)
	if err != nil {
		t.Fatalf("Failed to parse test database URL: %v", err)
	}

	testDBName := strings.TrimPrefix(parsedURL.Path, "/")
	if testDBName == "" {
		t.Fatal("Test database name not found in TEST_DATABASE_URL")
	}

	// Create a connection URL to the default postgres database (or talkback database)
	adminURL := *parsedURL
	adminURL.Path = "/postgres" // Connect to default postgres database to manage databases

	// Add connect_timeout to connection string for faster failure
	// Merge with existing query parameters
	existingQuery := adminURL.Query()
	existingQuery.Set("sslmode", "disable")
	existingQuery.Set("connect_timeout", "5")
	adminURL.RawQuery = existingQuery.Encode()
	
	// Create context with timeout to prevent hanging on connection issues
	connectCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Try connecting to postgres database first, fallback to talkback
	adminPool, err := pgxpool.New(connectCtx, adminURL.String())
	if err != nil {
		// If postgres database doesn't exist, try connecting to talkback database
		adminURL.Path = "/talkback"
		adminURL.RawQuery = existingQuery.Encode()
		connectCtx2, cancel2 := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel2()
		adminPool, err = pgxpool.New(connectCtx2, adminURL.String())
		if err != nil {
			// Last resort: try connecting directly to test database (it might already exist)
			testURL := *parsedURL
			testQuery := testURL.Query()
			testQuery.Set("sslmode", "disable")
			testQuery.Set("connect_timeout", "5")
			testURL.RawQuery = testQuery.Encode()
			connectCtx3, cancel3 := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel3()
			testPool, testErr := pgxpool.New(connectCtx3, testURL.String())
			if testErr == nil {
				// Test database exists and is accessible, skip drop/recreate
				testPool.Close()
				t.Logf("Test database %s already exists and is accessible. Skipping drop/recreate.", testDBName)
				return
			}
			t.Fatalf("Failed to connect to PostgreSQL (timeout after 10s): %v\n\nTroubleshooting:\n1. Is PostgreSQL running? Try: docker compose -f deploy/docker-compose.yml up -d\n2. If Docker API version mismatch (500 Internal Server Error), restart Docker Desktop:\n   - Right-click Docker Desktop system tray icon -> Restart\n   - Or: docker context use default && docker context use desktop-linux\n3. Verify port 5432 is accessible: Test-NetConnection -ComputerName localhost -Port 5432\n4. Set TEST_DATABASE_URL manually if using external PostgreSQL", err)
		}
	}
	defer adminPool.Close()

	// Use background context for database operations (connection already established)
	ctx := context.Background()

	// Terminate all connections to the test database before dropping
	terminateQuery := `
		SELECT pg_terminate_backend(pg_stat_activity.pid)
		FROM pg_stat_activity
		WHERE pg_stat_activity.datname = $1
		AND pid <> pg_backend_pid();
	`
	_, _ = adminPool.Exec(ctx, terminateQuery, testDBName) // Ignore errors (database might not exist)

	// Validate and quote database name
	if !isSafeDatabaseName(testDBName) {
		t.Fatalf("Invalid database name: %s (contains unsafe characters)", testDBName)
	}
	quotedDBName := fmt.Sprintf(`"%s"`, strings.ReplaceAll(testDBName, `"`, `""`))

	// Drop the test database if it exists
	dropQuery := fmt.Sprintf("DROP DATABASE IF EXISTS %s", quotedDBName)
	_, err = adminPool.Exec(ctx, dropQuery)
	if err != nil {
		t.Logf("Warning: Failed to drop test database (may not exist): %v", err)
	}

	// Create the test database using template0 to avoid collation issues
	createQuery := fmt.Sprintf("CREATE DATABASE %s WITH TEMPLATE template0", quotedDBName)
	_, err = adminPool.Exec(ctx, createQuery)
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}
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
