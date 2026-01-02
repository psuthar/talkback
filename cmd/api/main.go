package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
	"github.com/psuthar/talkback/internal/database"
	"github.com/psuthar/talkback/internal/handlers"
	"github.com/psuthar/talkback/internal/migrations"
)

type healthResponse struct {
	Status string `json:"status"`
}

type dbPingResponse struct {
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

func main() {
	// Load .env file (autoload handles it, but log if missing)
	if err := godotenv.Load(); err != nil {
		log.Printf("Warning: .env file not found: %v, using environment variables", err)
	} else {
		log.Println("Successfully loaded .env file")
		// Verify OPENAI_API_KEY is loaded (don't log the actual key)
		if apiKey := os.Getenv("OPENAI_API_KEY"); apiKey != "" {
			log.Printf("OPENAI_API_KEY is set (length: %d)", len(apiKey))
		} else {
			log.Println("Warning: OPENAI_API_KEY not found in environment after loading .env")
		}
	}

	// Run migrations on startup if RUN_MIGRATIONS is set (or default to true in dev)
	runMigrationsEnv := os.Getenv("RUN_MIGRATIONS")
	if runMigrationsEnv == "" || runMigrationsEnv == "true" {
		if err := runMigrations(); err != nil {
			log.Fatalf("Failed to run migrations: %v", err)
		}
		log.Println("Migrations completed successfully")
	} else {
		log.Println("Skipping migrations (RUN_MIGRATIONS != true)")
	}

	// Initialize database
	db, err := database.New()
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer db.Close()
	log.Println("Database connection established")

	// Initialize handlers
	h := handlers.NewHandlers(db)

	// CORS middleware
	corsMiddleware := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusOK)
				return
			}

			next(w, r)
		}
	}

	// Register routes with CORS
	http.HandleFunc("/health", corsMiddleware(healthHandler))
	http.HandleFunc("/db/ping", corsMiddleware(dbPingHandler))

	// Artifact endpoints with CORS
	http.HandleFunc("/artifacts", corsMiddleware(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/artifacts" && r.Method == http.MethodPost {
			h.CreateArtifact(w, r)
		} else {
			h.ArtifactsRouter(w, r)
		}
	}))
	http.HandleFunc("/artifacts/", corsMiddleware(h.ArtifactsRouter))

	// Admin endpoints with CORS
	http.HandleFunc("/admin/reset", corsMiddleware(h.ResetAllData))

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("Server starting on port %s", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		log.Fatal(err)
	}
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(healthResponse{Status: "ok"})
}

func dbPingHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(dbPingResponse{
			Status:  "error",
			Message: "DATABASE_URL environment variable is not set",
		})
		return
	}

	db, err := database.New()
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(dbPingResponse{
			Status:  "error",
			Message: fmt.Sprintf("Failed to connect to database: %v", err),
		})
		return
	}
	defer db.Close()

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(dbPingResponse{
		Status:  "success",
		Message: "Database connection successful",
	})
}

func runMigrations() error {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return fmt.Errorf("DATABASE_URL environment variable is not set")
	}

	// Get migrations from embedded filesystem (imported from internal/migrations)
	migrationsSubFS, err := fs.Sub(migrations.MigrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("failed to get migrations subdirectory: %w", err)
	}

	// Create iofs driver from embedded filesystem
	sourceDriver, err := iofs.New(migrationsSubFS, ".")
	if err != nil {
		return fmt.Errorf("failed to create iofs driver: %w", err)
	}

	// Open database connection using database/sql (required by golang-migrate)
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return fmt.Errorf("failed to open database: %w", err)
	}
	defer db.Close()

	// Create postgres driver
	postgresDriver, err := postgres.WithInstance(db, &postgres.Config{})
	if err != nil {
		return fmt.Errorf("failed to create postgres driver: %w", err)
	}

	// Create migrate instance with embedded source
	m, err := migrate.NewWithInstance("iofs", sourceDriver, "postgres", postgresDriver)
	if err != nil {
		return fmt.Errorf("failed to create migrate instance: %w", err)
	}

	// Run migrations
	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	return nil
}
