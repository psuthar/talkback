// set-migration-version sets schema_migrations to version 1 (one-time fix when switching to squashed migrations on an existing DB).
// Usage: set MIGRATION_FIX_DATABASE_URL to the Render external DB URL only for this run so your local .env is unchanged:
//   PowerShell: $env:MIGRATION_FIX_DATABASE_URL = "postgresql://..."; go run ./cmd/set-migration-version
package main

import (
	"database/sql"
	"log"
	"os"

	_ "github.com/lib/pq"
)

func main() {
	// Use MIGRATION_FIX_DATABASE_URL so you don't have to change local .env (DATABASE_URL stays localhost).
	dsn := os.Getenv("MIGRATION_FIX_DATABASE_URL")
	if dsn == "" {
		log.Fatal("MIGRATION_FIX_DATABASE_URL is not set. Set it only for this run (e.g. in PowerShell) to the Render external DB URL so your local DATABASE_URL is untouched.")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer db.Close()
	_, err = db.Exec("UPDATE schema_migrations SET version = 1, dirty = false")
	if err != nil {
		log.Fatalf("update schema_migrations: %v", err)
	}
	log.Println("Done. schema_migrations set to version 1, dirty = false. Redeploy the app so migration 2 runs.")
}
