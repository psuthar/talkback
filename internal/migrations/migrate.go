package migrations

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	_ "github.com/lib/pq"
)

func getMigrationsPath() (string, error) {
	// First, try using the working directory (set in launch.json)
	wd, err := os.Getwd()
	if err == nil {
		migrationsDir := filepath.Join(wd, "migrations")
		if _, err := os.Stat(migrationsDir); err == nil {
			// Directory exists, use it
			absPath, _ := filepath.Abs(migrationsDir)
			path := filepath.ToSlash(absPath)
			if runtime.GOOS == "windows" {
				return "file:///" + path, nil
			}
			if len(path) > 0 && path[0] != '/' {
				path = "/" + path
			}
			return "file://" + path, nil
		}
	}

	// Fallback: use runtime.Caller to find migrations relative to source
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		return "", fmt.Errorf("failed to get caller information")
	}

	dir := filepath.Dir(filename)
	projectRoot := filepath.Join(dir, "..", "..")
	migrationsDir := filepath.Join(projectRoot, "migrations")

	absPath, err := filepath.Abs(migrationsDir)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path: %w", err)
	}

	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		return "", fmt.Errorf("migrations directory does not exist: %s", absPath)
	}

	path := filepath.ToSlash(absPath)
	if runtime.GOOS == "windows" {
		return "file:///" + path, nil
	}
	if len(path) > 0 && path[0] != '/' {
		path = "/" + path
	}
	return "file://" + path, nil
}

func RunMigrations() error {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return fmt.Errorf("DATABASE_URL environment variable is not set")
	}

	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer db.Close()

	driver, err := postgres.WithInstance(db, &postgres.Config{})
	if err != nil {
		return fmt.Errorf("failed to create postgres driver: %w", err)
	}

	migrationsPath, err := getMigrationsPath()
	if err != nil {
		return fmt.Errorf("failed to get migrations path: %w", err)
	}

	m, err := migrate.NewWithDatabaseInstance(migrationsPath, "postgres", driver)
	if err != nil {
		return fmt.Errorf("failed to create migrate instance (path: %s): %w", migrationsPath, err)
	}

	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	return nil
}
