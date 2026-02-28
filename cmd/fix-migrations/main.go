// fix-migrations clears a dirty migration state (version 27) and re-runs migrations.
// Run from repo root: go run ./cmd/fix-migrations
// Requires DATABASE_URL (load from .env automatically).
package main

import (
	"database/sql"
	"fmt"
	"io/fs"
	"log"
	"os"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
	"github.com/psuthar/talkback/internal/migrations"
)

func main() {
	_ = godotenv.Load()
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		log.Fatal("DATABASE_URL is not set")
	}

	migrationsSubFS, err := fs.Sub(migrations.MigrationsFS, "migrations")
	if err != nil {
		log.Fatalf("migrations sub fs: %v", err)
	}
	sourceDriver, err := iofs.New(migrationsSubFS, ".")
	if err != nil {
		log.Fatalf("iofs driver: %v", err)
	}

	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer db.Close()

	postgresDriver, err := postgres.WithInstance(db, &postgres.Config{})
	if err != nil {
		log.Fatalf("postgres driver: %v", err)
	}

	m, err := migrate.NewWithInstance("iofs", sourceDriver, "postgres", postgresDriver)
	if err != nil {
		log.Fatalf("migrate instance: %v", err)
	}
	defer m.Close()

	version, dirty, err := m.Version()
	if err != nil && err != migrate.ErrNilVersion {
		log.Fatalf("version: %v", err)
	}
	if dirty {
		log.Printf("Clearing dirty state (current version %d), forcing to 26...", version)
		if err := m.Force(26); err != nil {
			log.Fatalf("force 26: %v", err)
		}
		log.Println("Forced to version 26.")
	}

	log.Println("Running migrations Up()...")
	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		log.Fatalf("migrate up: %v", err)
	}
	if err == migrate.ErrNoChange {
		log.Println("No new migrations to apply.")
	} else {
		log.Println("Migrations completed successfully.")
	}
	v, _, _ := m.Version()
	fmt.Printf("Database now at version %d.\n", v)
}
