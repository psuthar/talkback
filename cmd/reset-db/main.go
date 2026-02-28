package main

import (
	"database/sql"
	"log"
	"os"

	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
)

func main() {
	// Load .env if present
	_ = godotenv.Load()

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		log.Fatal("DATABASE_URL is not set (put it in .env)")
	}

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer db.Close()

	log.Println("Resetting public schema and migration state (this will delete ALL data in this database)...")

	_, err = db.Exec(`
DROP SCHEMA IF EXISTS public CASCADE;
CREATE SCHEMA public;
GRANT ALL ON SCHEMA public TO public;
DROP TABLE IF EXISTS schema_migrations;
`)
	if err != nil {
		log.Fatalf("reset schema: %v", err)
	}

	log.Println("Database schema reset complete. On next app start, baseline migrations will run from scratch.")
}

