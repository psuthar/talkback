package mcpserver

import (
	"database/sql"
	"io/fs"
	"log"
	"os"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
	"github.com/psuthar/talkback/internal/database"
	"github.com/psuthar/talkback/internal/migrations"
	"github.com/psuthar/talkback/internal/test"
	"github.com/stretchr/testify/require"
)

// mcptestDBEnabled is true when TestMain connected to a migrated shared test DB.
var mcptestDBEnabled bool

func TestMain(m *testing.M) {
	_ = godotenv.Load(".env.test")
	_ = godotenv.Load("../.env.test")
	_ = godotenv.Load("../../.env.test")

	sharedURL, cleanupDB, err := test.CreateSharedTestDB()
	if err != nil {
		log.Printf("mcpserver: skipping DB-backed tests: %v", err)
		os.Exit(m.Run())
	}
	if err := runMcptestMigrations(sharedURL); err != nil {
		if cleanupDB != nil {
			cleanupDB()
		}
		log.Fatalf("mcpserver TestMain: run migrations: %v", err)
	}
	originalURL := os.Getenv("DATABASE_URL")
	os.Setenv("DATABASE_URL", sharedURL)
	mcptestDBEnabled = true
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

func setupMCPTestDB(t *testing.T) *database.DB {
	t.Helper()
	if !mcptestDBEnabled {
		t.Skip("database not available (set TEST_DATABASE_URL or DATABASE_URL for mcpserver DB tests)")
	}
	db, err := database.New()
	require.NoError(t, err, "DATABASE_URL must be set (TestMain sets it from shared test DB)")
	return db
}

func runMcptestMigrations(databaseURL string) error {
	migrationsSubFS, err := fs.Sub(migrations.MigrationsFS, "migrations")
	if err != nil {
		return err
	}
	sourceDriver, err := iofs.New(migrationsSubFS, ".")
	if err != nil {
		return err
	}
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return err
	}
	defer db.Close()
	postgresDriver, err := postgres.WithInstance(db, &postgres.Config{})
	if err != nil {
		return err
	}
	m, err := migrate.NewWithInstance("iofs", sourceDriver, "postgres", postgresDriver)
	if err != nil {
		return err
	}
	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return err
	}
	return nil
}
