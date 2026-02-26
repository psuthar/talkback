package test

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/joho/godotenv"
)

// isSafeDatabaseName checks if a database name contains only safe characters
// PostgreSQL database names can contain letters, digits, underscores, and hyphens
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

// DBInterface defines the interface needed for test helpers
// This avoids import cycles by not importing the database package
type DBInterface interface {
	GetPool() *pgxpool.Pool
	Close()
}

// ensureTestDatabase drops and recreates the test database
// This ensures a clean state for each test run
func ensureTestDatabase(t *testing.T, testDatabaseURL string) {
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
	// to drop/recreate the test database
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
	// This is necessary because we can't drop a database that has active connections
	// Use parameterized query for the database name
	terminateQuery := `
		SELECT pg_terminate_backend(pg_stat_activity.pid)
		FROM pg_stat_activity
		WHERE pg_stat_activity.datname = $1
		AND pid <> pg_backend_pid();
	`
	_, _ = adminPool.Exec(ctx, terminateQuery, testDBName) // Ignore errors (database might not exist)

	// Drop the test database if it exists (ignore errors if it doesn't exist)
	// Note: Database names in DROP/CREATE DATABASE cannot be parameterized, so we use pg_quote_ident
	// For safety, we validate the database name contains only safe characters
	if !isSafeDatabaseName(testDBName) {
		t.Fatalf("Invalid database name: %s (contains unsafe characters)", testDBName)
	}
	
	// Quote the database name to prevent SQL injection
	quotedDBName := fmt.Sprintf(`"%s"`, strings.ReplaceAll(testDBName, `"`, `""`))
	
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

var testDBCounter uint64

func makeUniqueTestDBName(baseName string) string {
	// PostgreSQL identifier length limit is 63 bytes.
	// Use a short, safe suffix to avoid collisions between parallel packages.
	suffix := fmt.Sprintf("test_%d_%d", os.Getpid(), atomic.AddUint64(&testDBCounter, 1))

	// Reserve 1 char for '_' between base and suffix.
	maxBaseLen := 63 - 1 - len(suffix)
	if maxBaseLen < 1 {
		// Extremely defensive fallback.
		return suffix
	}

	if len(baseName) > maxBaseLen {
		baseName = baseName[:maxBaseLen]
	}
	return baseName + "_" + suffix
}

// dropTestDatabase terminates active connections and drops the database.
func dropTestDatabase(t *testing.T, testDatabaseURL string) {
	t.Helper()

	parsedURL, err := url.Parse(testDatabaseURL)
	if err != nil {
		t.Logf("Warning: failed to parse test database URL for cleanup: %v", err)
		return
	}

	testDBName := strings.TrimPrefix(parsedURL.Path, "/")
	if testDBName == "" {
		t.Logf("Warning: test database name not found in URL for cleanup")
		return
	}

	// Connect to an admin DB to drop the test DB.
	adminURL := *parsedURL
	adminURL.Path = "/postgres"

	existingQuery := adminURL.Query()
	existingQuery.Set("sslmode", "disable")
	existingQuery.Set("connect_timeout", "5")
	adminURL.RawQuery = existingQuery.Encode()

	connectCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	adminPool, err := pgxpool.New(connectCtx, adminURL.String())
	if err != nil {
		// Fallback to talkback if postgres isn't available.
		adminURL.Path = "/talkback"
		adminURL.RawQuery = existingQuery.Encode()
		connectCtx2, cancel2 := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel2()
		adminPool, err = pgxpool.New(connectCtx2, adminURL.String())
		if err != nil {
			t.Logf("Warning: failed to connect to admin DB for cleanup: %v", err)
			return
		}
	}
	defer adminPool.Close()

	ctx := context.Background()
	terminateQuery := `
		SELECT pg_terminate_backend(pg_stat_activity.pid)
		FROM pg_stat_activity
		WHERE pg_stat_activity.datname = $1
		AND pid <> pg_backend_pid();
	`
	_, _ = adminPool.Exec(ctx, terminateQuery, testDBName)

	if !isSafeDatabaseName(testDBName) {
		t.Logf("Warning: invalid database name for cleanup: %s", testDBName)
		return
	}
	quotedDBName := fmt.Sprintf(`"%s"`, strings.ReplaceAll(testDBName, `"`, `""`))

	dropQuery := fmt.Sprintf("DROP DATABASE IF EXISTS %s", quotedDBName)
	_, err = adminPool.Exec(ctx, dropQuery)
	if err != nil {
		t.Logf("Warning: failed to drop test database during cleanup: %v", err)
	}
}

// SetupTestDB creates a test database connection
// Returns the database URL and a cleanup function
// Automatically loads .env.test if it exists
// Drops and recreates the test database for a clean state
func SetupTestDB(t *testing.T) (string, func()) {
	t.Helper()

	// Try to load .env.test file (ignore errors if it doesn't exist)
	// Try multiple paths since test working directory may vary
	_ = godotenv.Load(".env.test")
	_ = godotenv.Load("../.env.test")
	_ = godotenv.Load("../../.env.test")

	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		// Fallback to regular DATABASE_URL
		databaseURL = os.Getenv("DATABASE_URL")
		if databaseURL == "" {
			t.Fatal("TEST_DATABASE_URL or DATABASE_URL must be set")
		}
	}

	// Create an isolated database per test to avoid cross-package collisions.
	parsedURL, err := url.Parse(databaseURL)
	if err != nil {
		t.Fatalf("Failed to parse test database URL: %v", err)
	}
	baseName := strings.TrimPrefix(parsedURL.Path, "/")
	if baseName == "" {
		t.Fatal("Test database name not found in TEST_DATABASE_URL")
	}

	uniqueName := makeUniqueTestDBName(baseName)
	if !isSafeDatabaseName(uniqueName) {
		t.Fatalf("Generated invalid database name: %s", uniqueName)
	}

	testURL := *parsedURL
	testURL.Path = "/" + uniqueName

	// Ensure test database is dropped and recreated for clean state.
	ensureTestDatabase(t, testURL.String())

	return testURL.String(), func() { dropTestDatabase(t, testURL.String()) }
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

	tables := []string{"session_events", "session_participants", "session_invitations", "sessions", "answers", "questions", "video_sources", "materials", "artifacts", "login_sessions", "password_credentials", "users"}
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
