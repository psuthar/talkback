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

// EnsureTestDatabaseURL drops and recreates the test database at the given URL.
// Returns an error instead of using t.Fatal so it can be used from TestMain.
func EnsureTestDatabaseURL(testDatabaseURL string) error {
	parsedURL, err := url.Parse(testDatabaseURL)
	if err != nil {
		return fmt.Errorf("parse test database URL: %w", err)
	}
	testDBName := strings.TrimPrefix(parsedURL.Path, "/")
	if testDBName == "" {
		return fmt.Errorf("test database name not found in URL")
	}
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
		adminURL.Path = "/talkback"
		adminURL.RawQuery = existingQuery.Encode()
		connectCtx2, cancel2 := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel2()
		adminPool, err = pgxpool.New(connectCtx2, adminURL.String())
		if err != nil {
			return fmt.Errorf("connect to PostgreSQL (timeout 10s): %w", err)
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
		return fmt.Errorf("invalid database name: %s", testDBName)
	}
	quotedDBName := fmt.Sprintf(`"%s"`, strings.ReplaceAll(testDBName, `"`, `""`))
	dropQuery := fmt.Sprintf("DROP DATABASE IF EXISTS %s", quotedDBName)
	_, _ = adminPool.Exec(ctx, dropQuery)
	createQuery := fmt.Sprintf("CREATE DATABASE %s WITH TEMPLATE template0", quotedDBName)
	if _, err := adminPool.Exec(ctx, createQuery); err != nil {
		return fmt.Errorf("create test database: %w", err)
	}
	return nil
}

// DropTestDatabaseURL terminates connections and drops the database at the given URL.
// Returns an error so it can be used from TestMain.
func DropTestDatabaseURL(testDatabaseURL string) error {
	parsedURL, err := url.Parse(testDatabaseURL)
	if err != nil {
		return fmt.Errorf("parse URL: %w", err)
	}
	testDBName := strings.TrimPrefix(parsedURL.Path, "/")
	if testDBName == "" {
		return fmt.Errorf("database name not found in URL")
	}
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
		adminURL.Path = "/talkback"
		adminURL.RawQuery = existingQuery.Encode()
		connectCtx2, cancel2 := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel2()
		adminPool, err = pgxpool.New(connectCtx2, adminURL.String())
		if err != nil {
			return fmt.Errorf("connect to admin DB: %w", err)
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
		return fmt.Errorf("invalid database name: %s", testDBName)
	}
	quotedDBName := fmt.Sprintf(`"%s"`, strings.ReplaceAll(testDBName, `"`, `""`))
	dropQuery := fmt.Sprintf("DROP DATABASE IF EXISTS %s", quotedDBName)
	if _, err := adminPool.Exec(ctx, dropQuery); err != nil {
		return fmt.Errorf("drop test database: %w", err)
	}
	return nil
}

// CreateSharedTestDB creates a single test database for the package, runs no migrations.
// Caller must run migrations and set DATABASE_URL. Cleanup drops the database.
// Use from TestMain to get one DB for all tests; each test then truncates and reuses.
func CreateSharedTestDB() (databaseURL string, cleanup func(), err error) {
	_ = godotenv.Load(".env.test")
	_ = godotenv.Load("../.env.test")
	_ = godotenv.Load("../../.env.test")
	databaseURL = os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		databaseURL = os.Getenv("DATABASE_URL")
		if databaseURL == "" {
			return "", nil, fmt.Errorf("TEST_DATABASE_URL or DATABASE_URL must be set")
		}
	}
	parsedURL, err := url.Parse(databaseURL)
	if err != nil {
		return "", nil, fmt.Errorf("parse database URL: %w", err)
	}
	baseName := strings.TrimPrefix(parsedURL.Path, "/")
	if baseName == "" {
		return "", nil, fmt.Errorf("database name not found in URL")
	}
	uniqueName := makeUniqueTestDBName(baseName)
	if !isSafeDatabaseName(uniqueName) {
		return "", nil, fmt.Errorf("invalid database name: %s", uniqueName)
	}
	testURL := *parsedURL
	testURL.Path = "/" + uniqueName
	finalURL := testURL.String()
	if err := EnsureTestDatabaseURL(finalURL); err != nil {
		return "", nil, err
	}
	cleanup = func() { _ = DropTestDatabaseURL(finalURL) }
	return finalURL, cleanup, nil
}

// ensureTestDatabase drops and recreates the test database
// This ensures a clean state for each test run
func ensureTestDatabase(t *testing.T, testDatabaseURL string) {
	t.Helper()
	if err := EnsureTestDatabaseURL(testDatabaseURL); err != nil {
		t.Fatalf("Ensure test database: %v\n\nTroubleshooting:\n1. Is PostgreSQL running? Try: docker compose -f deploy/docker-compose.yml up -d\n2. Verify port 5432 is accessible.\n3. Set TEST_DATABASE_URL manually if using external PostgreSQL", err)
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
	if err := DropTestDatabaseURL(testDatabaseURL); err != nil {
		t.Logf("Warning: drop test database: %v", err)
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

// TruncateTables truncates all tables for a clean test state.
// Uses a single TRUNCATE ... CASCADE so PostgreSQL acquires locks once and avoids lock ordering / long holds.
// Uses a 15s timeout so we fail fast if another connection holds locks instead of blocking for minutes.
func TruncateTables(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()

	ctx := context.Background()
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	// Single TRUNCATE with root tables; CASCADE truncates all dependent tables in one go.
	// Order: artifacts (refs sessions), then sessions, then users. CASCADE handles the rest.
	_, err := pool.Exec(ctx, `TRUNCATE TABLE artifacts, sessions, users RESTART IDENTITY CASCADE`)
	if err != nil {
		t.Fatalf("TruncateTables (timeout 15s): %v. If this blocks, another connection may be holding locks (e.g. open transaction).", err)
	}
}
