package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/joho/godotenv"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"

	_ "github.com/lib/pq"
)

func main() {
	_ = godotenv.Load()

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		log.Fatal("DATABASE_URL is not set")
	}

	// Run from repo root:
	// - go run ./cmd/run-db-migrations
	// - ensure `db/migrations/*.up.sql` is available.
	wd, err := os.Getwd()
	if err != nil {
		log.Fatalf("getwd: %v", err)
	}
	projectRoot := wd
	if filepath.Base(wd) == "cmd" {
		projectRoot = filepath.Dir(wd)
	}

	migrationsDir := filepath.Join(projectRoot, "db", "migrations")
	absMigrationsDir, err := filepath.Abs(migrationsDir)
	if err != nil {
		log.Fatalf("abs migrations dir: %v", err)
	}

	// golang-migrate file source driver concatenates u.Host + u.Path.
	// For Windows we must avoid `file:///C:/...` (would yield `/C:/...`), and instead use `file://C:/...`.
	migrationsSource := "file://" + filepath.ToSlash(absMigrationsDir)

	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer db.Close()

	driver, err := postgres.WithInstance(db, &postgres.Config{})
	if err != nil {
		log.Fatalf("create postgres driver: %v", err)
	}

	m, err := migrate.NewWithDatabaseInstance(migrationsSource, "postgres", driver)
	if err != nil {
		log.Fatalf("create migrate instance: %v", err)
	}
	defer m.Close()

	log.Printf("Running migrations Up() from %s ...", migrationsSource)

	// If the previous run left the schema_migrations table in a dirty state,
	// clear the dirty flag to allow continuing with the next migration.
	v, dirty, err := m.Version()
	if err != nil && err != migrate.ErrNilVersion {
		log.Fatalf("migrate version: %v", err)
	}
	if dirty {
		log.Printf("schema_migrations is dirty; forcing version=%d (dirty=false)", v)
		if err := m.Force(int(v)); err != nil {
			log.Fatalf("force migration version: %v", err)
		}
	}

	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		log.Fatalf("migrate up: %v", err)
	}

	v, dirty, err = m.Version()
	if err != nil {
		log.Fatalf("migrate version: %v", err)
	}
	log.Printf("Migrations complete. schema_migrations version=%d dirty=%v", v, dirty)
	fmt.Println("Done.")
}

