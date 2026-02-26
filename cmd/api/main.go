package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
	"github.com/psuthar/talkback/internal/auth"
	"github.com/psuthar/talkback/internal/database"
	"github.com/psuthar/talkback/internal/handlers"
	"github.com/psuthar/talkback/internal/migrations"
	"github.com/psuthar/talkback/internal/models"
	"github.com/psuthar/talkback/internal/processing"
	"github.com/psuthar/talkback/internal/rag"
	"github.com/psuthar/talkback/internal/utils"
)

type healthResponse struct {
	Status string `json:"status"`
}

type dbPingResponse struct {
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}

func main() {
	// Load .env only outside production (Render sets ENV=production and uses dashboard env vars)
	if env := os.Getenv("ENV"); env != "production" {
		if err := godotenv.Load(); err != nil {
			log.Printf("Warning: .env file not found: %v, using environment variables", err)
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

	// Auto-create bootstrap admin account if configured and not present
	if err := ensureBootstrapAdmin(context.Background(), db); err != nil {
		log.Printf("Warning: bootstrap admin setup failed: %v", err)
	}

	// Initialize job processor for auto-transcription
	workers := 2 // Default to 2 workers
	if workersEnv := os.Getenv("TRANSCRIPT_WORKERS"); workersEnv != "" {
		if parsed, err := fmt.Sscanf(workersEnv, "%d", &workers); err != nil || parsed != 1 || workers <= 0 {
			log.Printf("Warning: Invalid TRANSCRIPT_WORKERS value, using default: 2")
			workers = 2
		}
	}
	jobProcessor := utils.NewJobProcessor(db, workers)
	jobProcessor.OnTranscriptCompleted = func(sessionID uuid.UUID) { rag.IndexSessionAsync(sessionID, db) }

	// Start job processor in background
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	jobProcessor.Start(ctx)
	log.Printf("Job processor started with %d workers", workers)

	// Initialize handlers
	h := handlers.NewHandlers(db, jobProcessor)

	// Mission #4: processing worker and reconciler for Zoom import pipeline
	getZoomToken := func(ctx context.Context, creatorIdentity string) (string, error) {
		tok, _, err := h.GetValidZoomAccessTokenContext(ctx, creatorIdentity)
		return tok, err
	}
	go processing.RunWorker(ctx, db, getZoomToken, 15*time.Second, 15*time.Minute)
	go processing.RunReconciler(ctx, db, 20*time.Minute, 20*time.Minute)
	log.Println("Processing worker and reconciler started")

	// CORS middleware: origin configurable via CORS_ALLOWED_ORIGINS (default * for local dev)
	allowedOrigin := strings.TrimSpace(os.Getenv("CORS_ALLOWED_ORIGINS"))
	if allowedOrigin == "" {
		allowedOrigin = "*"
		log.Println("CORS: CORS_ALLOWED_ORIGINS unset; auth routes will reflect request Origin for credential requests")
	}
	corsOrigin := allowedOrigin
	corsMiddleware := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", corsOrigin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Creator-Identity, X-Participant-Ref")

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusOK)
				return
			}

			next(w, r)
		}
	}
	// CORS with credentials for cookie-based auth (/api/auth, /api/me). Browser requires a specific origin
	// (not *) and Allow-Credentials when credentials: 'include' is used. When CORS_ALLOWED_ORIGINS is unset
	// (e.g. local dev), reflect the request Origin so login works without configuring env.
	corsWithCredentials := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			origin := corsOrigin
			if origin == "*" {
				if o := strings.TrimSpace(r.Header.Get("Origin")); o != "" {
					origin = o
					log.Printf("CORS: reflecting Origin for credentialed request: %s", origin)
				}
			}
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Creator-Identity, X-Participant-Ref")
			w.Header().Set("Access-Control-Allow-Credentials", "true")

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusOK)
				return
			}

			next(w, r)
		}
	}

	// Register routes with CORS
	http.HandleFunc("/health", corsMiddleware(healthHandler))
	http.HandleFunc("/healthz", corsMiddleware(healthHandler))
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

	// Session endpoints with CORS (Phase 3). POST create requires auth + admin/creator role.
	http.HandleFunc("/sessions", corsWithCredentials(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/sessions" && r.Method == http.MethodPost {
			h.RequireAuth(h.CreateSession)(w, r)
		} else {
			h.SessionsRouter(w, r)
		}
	}))
	http.HandleFunc("/sessions/from-zoom", corsWithCredentials(h.RequireAuth(h.CreateSessionFromZoom)))
	http.HandleFunc("/sessions/", corsMiddleware(h.SessionsRouter))

	// WebSocket endpoint for session updates
	http.HandleFunc("/ws/session", corsMiddleware(h.HandleWebSocket(h.Hub)))

	// Admin endpoints with CORS
	// Reset all data: admin only (and ALLOW_DEV_RESET must be set)
	http.HandleFunc("/admin/reset", corsWithCredentials(h.RequireAuth(h.RequireAdmin(h.ResetAllData))))

	// Zoom OAuth endpoints
	http.HandleFunc("/auth/zoom/start", corsMiddleware(h.ZoomAuthStart))
	http.HandleFunc("/auth/zoom/callback", corsMiddleware(h.ZoomAuthCallback))
	http.HandleFunc("/auth/zoom/disconnect", corsMiddleware(h.ZoomAuthDisconnect))
	http.HandleFunc("/auth/zoom/me", corsMiddleware(h.ZoomAuthMe))
	// Zoom transcript status (explicit check before creating session)
	http.HandleFunc("/zoom/transcript-status", corsMiddleware(h.ZoomTranscriptStatus))

	// API Zoom endpoints (mission: /api/zoom/*)
	http.HandleFunc("/api/zoom/status", corsMiddleware(h.ZoomAPIStatus))
	http.HandleFunc("/api/zoom/connect", corsMiddleware(h.ZoomAPIConnect))
	http.HandleFunc("/api/zoom/disconnect", corsMiddleware(h.ZoomAPIDisconnect))
	http.HandleFunc("/api/zoom/recordings", corsMiddleware(h.ZoomAPIRecordings))
	http.HandleFunc("/api/zoom/import", corsWithCredentials(h.RequireAuth(h.ZoomImport)))

	// API Session list (my sessions): GET /api/sessions requires auth
	http.HandleFunc("/api/sessions", corsWithCredentials(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/sessions" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.RequireAuth(h.ListSessions)(w, r)
	}))
	// API Session import + ingestion status (mission: /api/sessions/:id/import/zoom, /api/sessions/:id/ingestion)
	// Session invite: POST /api/sessions/:id/invite requires auth; other /api/sessions/ routes unchanged
	http.HandleFunc("/api/sessions/", corsWithCredentials(h.ApiSessionsRouterWithInvite))

	// Admin user management (RequireAuth + RequireAdmin)
	http.HandleFunc("/api/admin/users", corsWithCredentials(h.RequireAuth(h.RequireAdmin(h.AdminUsersHandler))))
	http.HandleFunc("/api/admin/users/", corsWithCredentials(h.RequireAuth(h.RequireAdmin(h.AdminUsersHandler))))

	// TalkBack auth: signup, login, logout (cookie-based); /api/me requires auth
	http.HandleFunc("/api/auth/signup", corsWithCredentials(h.AuthSignup))
	http.HandleFunc("/api/auth/login", corsWithCredentials(h.AuthLogin))
	http.HandleFunc("/api/auth/logout", corsWithCredentials(h.AuthLogout))
	http.HandleFunc("/api/me", corsWithCredentials(h.RequireAuth(h.AuthMe)))

	// Local: PORT defaults to 8080 when unset; Render: provides PORT via env
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	bindAddress := os.Getenv("BIND_ADDRESS")
	addr := ":" + port
	if bindAddress != "" {
		addr = bindAddress + ":" + port
	}

	log.Printf("Server starting on %s", addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
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

// ensureBootstrapAdmin creates an admin user at startup when TALKBACK_BOOTSTRAP_ADMIN_EMAIL
// and TALKBACK_BOOTSTRAP_ADMIN_PASSWORD are set and no user with that email exists.
// If the user already exists, their global_role is ensured to be admin.
func ensureBootstrapAdmin(ctx context.Context, db *database.DB) error {
	email := auth.Config.BootstrapAdminEmail
	if email == "" {
		return nil
	}
	password := auth.Config.BootstrapAdminPassword
	existing, err := db.GetUserByEmail(ctx, email)
	if err != nil {
		return err
	}
	if existing != nil {
		// User exists: ensure they have admin role (idempotent)
		if existing.GlobalRole != models.GlobalRoleAdmin {
			if err := db.SetUserGlobalRole(ctx, existing.ID, models.GlobalRoleAdmin); err != nil {
				return err
			}
			log.Printf("Bootstrap admin: existing user %s promoted to admin", email)
		}
		// If bootstrap password is set, sync it so admin can always log in with env password after redeploys.
		if password != "" {
			hash, err := auth.HashPassword(password)
			if err != nil {
				return fmt.Errorf("hashing bootstrap admin password for sync: %w", err)
			}
			if err := db.UpdatePasswordCredential(ctx, existing.ID, hash); err != nil {
				return fmt.Errorf("updating bootstrap admin password: %w", err)
			}
			log.Printf("Bootstrap admin: synced password for %s", email)
		}
		return nil
	}
	// No user with this email: create admin account (password required)
	if password == "" {
		log.Printf("Bootstrap admin: skipping auto-create for %s (TALKBACK_BOOTSTRAP_ADMIN_PASSWORD not set)", email)
		return nil
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return fmt.Errorf("hashing bootstrap admin password: %w", err)
	}
	user := &models.User{
		ID:          uuid.New(),
		Email:       email,
		DisplayName: "Admin",
		Status:      models.UserStatusActive,
		GlobalRole:  models.GlobalRoleAdmin,
	}
	if err := db.CreateUser(ctx, user); err != nil {
		return fmt.Errorf("creating bootstrap admin user: %w", err)
	}
	cred := &models.PasswordCredential{
		UserID:            user.ID,
		PasswordHash:      hash,
		PasswordUpdatedAt: time.Now(),
	}
	if err := db.CreatePasswordCredential(ctx, cred); err != nil {
		return fmt.Errorf("creating bootstrap admin credential: %w", err)
	}
	log.Printf("Bootstrap admin: created admin account for %s", email)
	return nil
}
