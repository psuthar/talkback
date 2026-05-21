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
	"os/signal"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"github.com/google/uuid"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/newrelic/go-agent/v3/newrelic"
	"github.com/psuthar/talkback/internal/auth"
	"github.com/psuthar/talkback/internal/database"
	"github.com/psuthar/talkback/internal/debugfault"
	"github.com/psuthar/talkback/internal/email"
	"github.com/psuthar/talkback/internal/handlers"
	"github.com/psuthar/talkback/internal/invitations"
	"github.com/psuthar/talkback/internal/markitdown"
	"github.com/psuthar/talkback/internal/mcpserver"
	"github.com/psuthar/talkback/internal/migrations"
	"github.com/psuthar/talkback/internal/models"
	"github.com/psuthar/talkback/internal/processing"
	"github.com/psuthar/talkback/internal/rag"
	"github.com/psuthar/talkback/internal/storage"
	"github.com/psuthar/talkback/internal/storage/r2"
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
	bootAt := time.Now().UTC()
	pid := os.Getpid()
	log.Printf("Process boot start: pid=%d started_at=%s", pid, bootAt.Format(time.RFC3339Nano))
	defer func() {
		if r := recover(); r != nil {
			log.Printf("PANIC: pid=%d recovered=%v\n%s", pid, r, string(debug.Stack()))
			os.Exit(1)
		}
	}()

	// Load .env only outside production (Render sets ENV=production and uses dashboard env vars)
	if env := os.Getenv("ENV"); env != "production" {
		if err := godotenv.Load(); err != nil {
			log.Printf("Warning: .env file not found: %v, using environment variables", err)
		}
	}
	// Log auth cookie mode: same-origin (default) uses SameSite=Lax; split-origin needs AllowedOrigins + Secure for SameSite=None.
	if auth.Config.CookieSecure && len(auth.Config.AllowedOrigins) > 0 {
		log.Printf("Auth: cookie SameSite=None (split-origin / explicit cross-site), %d origin(s) in allowlist", len(auth.Config.AllowedOrigins))
	} else if len(auth.Config.AllowedOrigins) == 0 {
		log.Printf("Auth: same-origin mode (cookie SameSite=Lax); CORS/TB_ALLOWED_ORIGINS not required for unified SPA+API. Set allowlists only for split-origin (e.g. Vite :3000 + API :8080)")
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

	// Start job processor in background
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	jobProcessor.Start(ctx)
	log.Printf("Job processor started with %d workers", workers)

	uploadRoot := storage.UploadRoot()
	log.Printf("Uploads will be written to: %s (path: sessions/{session_id}/data/uploads/{filename})", uploadRoot)

	log.Println(utils.LibreOfficeHealthcheck())
	startLibreOfficeWarmup()

	// Storage (R2 when STORAGE_DRIVER=r2)
	var store storage.Interface
	if os.Getenv("STORAGE_DRIVER") == "r2" {
		cfg := r2.LoadConfig()
		if client, err := r2.New(cfg); err != nil {
			log.Printf("R2 storage disabled: %v (set R2_BUCKET, R2_ENDPOINT, R2_ACCESS_KEY_ID, R2_SECRET_ACCESS_KEY on Render)", err)
		} else {
			store = client
			log.Printf("R2 storage enabled (bucket=%s)", cfg.Bucket)
		}
	} else {
		log.Println("R2 storage not configured (STORAGE_DRIVER is not 'r2'); video presign and file artifacts will return 503")
	}

	// Initialize handlers (OnTranscriptCompleted set below after we have h, so Zoom Whisper fallback can broadcast)
	// Invitation service: creates secure invite records and returns accept_url for mailto or Resend.
	// APP_BASE_URL must be the frontend (web app) origin where /accept-invite is served, NOT the API URL.
	// On Render: set APP_BASE_URL on the API service to your web service URL (e.g. https://talkback-ux.onrender.com).
	appBaseURL := strings.TrimSpace(os.Getenv("APP_BASE_URL"))
	if appBaseURL == "" {
		appBaseURL = "http://localhost:3000"
		log.Println("Invitation service: APP_BASE_URL unset, using http://localhost:3000 (set APP_BASE_URL to your frontend URL for production)")
	} else {
		log.Println("Invitation service: accept-invite links will use base:", appBaseURL)
	}
	invRepo := invitations.NewRepository(db.Pool)
	lookup := &invitationLookup{db: db}
	var sendFn invitations.SendInviteFunc
	if resendKey := os.Getenv("RESEND_API_KEY"); resendKey != "" {
		fromAddr := os.Getenv("EMAIL_FROM_ADDRESS")
		if fromAddr == "" {
			fromAddr = "invites@talkback.app"
		}
		resend := email.NewResendSender(resendKey, fromAddr, os.Getenv("EMAIL_FROM_NAME"))
		sendFn = func(ctx context.Context, to, inviterName, sessionTitle, role, acceptURL, expNote string) error {
			return resend.SendInviteEmail(ctx, email.SendInviteParams{ToEmail: to, InviterName: inviterName, SessionTitle: sessionTitle, InvitedRole: role, AcceptURL: acceptURL, ExpirationNote: expNote})
		}
		log.Println("Invitation service: Resend email sending enabled")
	}
	expiryHours := 168
	if h := os.Getenv("INVITE_EXPIRY_HOURS"); h != "" {
		if n, _ := fmt.Sscanf(h, "%d", &expiryHours); n == 1 && expiryHours > 0 {
			// use expiryHours
		} else {
			expiryHours = 168
		}
	}
	invSvc := &invitations.Service{Repo: invRepo, Lookup: lookup, Config: invitations.Config{AppBaseURL: appBaseURL, ExpiryHours: expiryHours}, Send: sendFn}
	log.Println("Invitation service enabled (mailto delivery; set RESEND_API_KEY for email sending)")
	h := handlers.NewHandlers(db, jobProcessor, store, invSvc)
	h.IndexAsync = func(sessionID uuid.UUID) { rag.IndexSessionAsync(sessionID, db, store) }

	// SCRUM-303: wire markitdown sidecar client into both the upload handler
	// (gates pending vs ready at upload time) and the JobProcessor (worker
	// calls the sidecar). NewClient reads env; if MARKITDOWN_SIDECAR_URL or
	// MARKITDOWN_SIDECAR_SECRET is unset, .Enabled() returns false and the
	// upload path stays on the legacy "ready with empty text" branch.
	mdClient := markitdown.NewClient()
	h.Markitdown = mdClient
	jobProcessor.Markitdown = mdClient
	if mdClient.Enabled() {
		log.Printf("Markitdown sidecar client enabled (base=%s, image_extraction=%t)", mdClient.BaseURL, markitdown.ImageExtractionEnabled())
	} else {
		log.Println("Markitdown sidecar client disabled (MARKITDOWN_SIDECAR_URL/SECRET not set)")
	}

	// When a transcript job completes: reindex session for RAG; if it was a Whisper fallback, mark processing job ready and broadcast
	onJobReady := func(sessionID uuid.UUID) { h.Hub.BroadcastSessionProcessingReady(sessionID) }
	jobProcessor.OnTranscriptCompleted = func(sessionID uuid.UUID) {
		rag.IndexSessionAsync(sessionID, db, store)
		// Notify all clients (creator + participants) so UI updates without refresh (e.g. material transcript ready)
		if h.Hub != nil {
			h.Hub.BroadcastSessionUpdated(sessionID)
		}
		// SCRUM-469: flip every awaiting_whisper job for this session
		// to ready. Old code looked up ONE job by session.source_provider,
		// which missed multi-recording sessions where the awaiting job
		// has a different source than the session's original.
		ctxBg := context.Background()
		if flipped := processing.FlipAwaitingWhisperJobsToReady(ctxBg, db, sessionID); flipped > 0 {
			_ = db.UpdateSessionProcessingMirror(ctxBg, sessionID, models.ProcessingStateReady)
			onJobReady(sessionID)
		}
	}
	// When a material extraction job fails (or other session update without reindex): broadcast so UI shows failed state
	jobProcessor.OnSessionUpdated = func(sessionID uuid.UUID) {
		if h.Hub != nil {
			h.Hub.BroadcastSessionUpdated(sessionID)
		}
	}

	// Mission #4: processing worker and reconciler for Zoom + Teams import pipelines
	getZoomToken := func(ctx context.Context, creatorIdentity string) (string, error) {
		tok, _, err := h.GetValidZoomAccessTokenContext(ctx, creatorIdentity)
		return tok, err
	}
	getTeamsToken := func(ctx context.Context, creatorIdentity string) (string, error) {
		if os.Getenv("ENABLE_TEAMS") != "true" {
			return "", fmt.Errorf("teams disabled")
		}
		tok, _, err := h.GetValidTeamsAccessTokenContext(ctx, creatorIdentity)
		return tok, err
	}
	getGoogleMeetToken := func(ctx context.Context, creatorIdentity string) (string, error) {
		if os.Getenv("ENABLE_GOOGLE_MEET") != "true" {
			return "", fmt.Errorf("google_meet disabled")
		}
		tok, _, err := h.GetValidGoogleMeetAccessTokenContext(ctx, creatorIdentity)
		return tok, err
	}
	storagePrefix := ""
	if store != nil {
		storagePrefix = strings.TrimSuffix(strings.TrimSpace(os.Getenv("R2_PREFIX")), "/")
		prefixLog := storagePrefix
		if prefixLog == "" {
			prefixLog = "(none)"
		}
		log.Printf("Zoom MP4 ingest: R2 enabled (bucket=%s prefix=%s)", os.Getenv("R2_BUCKET"), prefixLog)
	} else {
		log.Printf("Zoom MP4 ingest: local disk (no R2; MP4 saved under sessions/{id}/videos/ for debugging)")
	}
	go processing.RunWorker(ctx, db, getZoomToken, getTeamsToken, getGoogleMeetToken, store, storagePrefix, 15*time.Second, 15*time.Minute, jobProcessor, onJobReady)
	go processing.RunReconciler(ctx, db, 20*time.Minute, 20*time.Minute)
	log.Println("Processing worker and reconciler started")

	// CORS: parse CORS_ALLOWED_ORIGINS as comma-separated list (trimmed). Empty => reflect Origin for dev.
	var allowedOrigins []string
	for _, o := range strings.Split(os.Getenv("CORS_ALLOWED_ORIGINS"), ",") {
		if o := strings.TrimSpace(o); o != "" {
			allowedOrigins = append(allowedOrigins, o)
		}
	}
	if len(allowedOrigins) == 0 {
		log.Println("CORS: CORS_ALLOWED_ORIGINS unset; auth routes will reflect request Origin for credentialed requests")
	}
	originAllowed := func(origin string) bool {
		origin = strings.TrimSpace(origin)
		for _, a := range allowedOrigins {
			if a == origin {
				return true
			}
		}
		return false
	}
	corsMiddleware := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			origin := "*"
			if len(allowedOrigins) > 0 {
				if o := strings.TrimSpace(r.Header.Get("Origin")); o != "" && originAllowed(o) {
					origin = o
				} else if len(allowedOrigins) == 1 {
					origin = allowedOrigins[0]
				}
			} else if o := strings.TrimSpace(r.Header.Get("Origin")); o != "" {
				origin = o
			}
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Creator-Identity, X-Participant-Ref, X-Obs-Test-Token")

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusOK)
				return
			}

			next(w, r)
		}
	}
	// CORS with credentials for cookie-based auth (/api/auth, /api/me, /api/sessions). Browser requires
	// a single specific origin and Allow-Credentials. Use request Origin when it's in the allowed list
	// so cross-origin cookies work (e.g. frontend on talkback-ux, API on talkback-895n on Render).
	corsWithCredentials := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			origin := ""
			reqOrigin := strings.TrimSpace(r.Header.Get("Origin"))
			if reqOrigin != "" && len(allowedOrigins) > 0 && originAllowed(reqOrigin) {
				origin = reqOrigin
			} else if len(allowedOrigins) == 1 {
				origin = allowedOrigins[0]
			} else if reqOrigin != "" && len(allowedOrigins) == 0 {
				origin = reqOrigin
			}
			if origin != "" {
				w.Header().Set("Access-Control-Allow-Origin", origin)
			}
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Creator-Identity, X-Participant-Ref, X-Obs-Test-Token")
			w.Header().Set("Access-Control-Allow-Credentials", "true")

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusOK)
				return
			}

			next(w, r)
		}
	}

	// New Relic: initialize app from env (optional; skip if NEW_RELIC_LICENSE_KEY not set).
	// Use a short timeout so startup never blocks Render's health check if New Relic is slow/unreachable.
	var nrApp *newrelic.Application
	if license := os.Getenv("NEW_RELIC_LICENSE_KEY"); license != "" {
		appName := os.Getenv("NEW_RELIC_APP_NAME")
		if appName == "" {
			appName = "Talkback-NewRelic"
		}
		type result struct {
			app *newrelic.Application
			err error
		}
		done := make(chan result, 1)
		go func() {
			app, errNR := newrelic.NewApplication(
				newrelic.ConfigAppName(appName),
				newrelic.ConfigLicense(license),
				newrelic.ConfigAppLogForwardingEnabled(true),
			)
			done <- result{app: app, err: errNR}
		}()
		select {
		case res := <-done:
			if res.err != nil {
				log.Printf("New Relic: failed to create application: %v", res.err)
			} else if res.app != nil {
				nrApp = res.app
				log.Printf("New Relic: application %q enabled", appName)
			}
		case <-time.After(5 * time.Second):
			log.Println("New Relic: init timed out after 5s; starting without APM so health check can pass")
		}
	} else {
		log.Println("New Relic: NEW_RELIC_LICENSE_KEY not set; APM disabled")
	}
	// wrapNR passes (pattern, handler) to http.HandleFunc; WrapHandleFunc returns (string, HandlerFunc) and is safe when nrApp is nil.
	wrapNR := func(pattern string, h http.HandlerFunc) (string, http.HandlerFunc) {
		return newrelic.WrapHandleFunc(nrApp, pattern, h)
	}

	// Register routes with CORS and optional New Relic
	http.HandleFunc(wrapNR("/health", corsMiddleware(healthHandler)))
	http.HandleFunc(wrapNR("/healthz", corsMiddleware(healthHandler)))
	http.HandleFunc(wrapNR("/db/ping", corsMiddleware(dbPingHandler)))

	// Log upload requests so we can confirm they hit this process (debug: breakpoints not hitting)
	logUploadIfMatch := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodPost && (strings.Contains(r.URL.Path, "materials/upload") || (strings.Contains(r.URL.Path, "/artifacts/") && strings.Contains(r.URL.Path, "/materials"))) {
				log.Printf("UPLOAD REQUEST received: %s %s", r.Method, r.URL.Path)
			}
			next(w, r)
		}
	}

	// Artifact endpoints with CORS
	// These endpoints are accessed by the SPA with `credentials: include`, so
	// they must allow credentials in CORS responses.
	http.HandleFunc(wrapNR("/artifacts", corsWithCredentials(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/artifacts" && r.Method == http.MethodPost {
			h.CreateArtifact(w, r)
		} else {
			h.ArtifactsRouter(w, r)
		}
	})))
	http.HandleFunc(wrapNR("/artifacts/", corsWithCredentials(logUploadIfMatch(h.ArtifactsRouter))))

	// Session endpoints with CORS (Phase 3). POST create requires auth + admin/creator role.
	http.HandleFunc(wrapNR("/sessions", corsWithCredentials(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/sessions" && r.Method == http.MethodPost {
			h.RequireAuth(h.CreateSession)(w, r)
		} else {
			h.SessionsRouter(w, r)
		}
	})))
	http.HandleFunc(wrapNR("/sessions/from-zoom", corsWithCredentials(h.RequireAuth(h.CreateSessionFromZoom))))
	http.HandleFunc(wrapNR("/sessions/", corsWithCredentials(logUploadIfMatch(h.SessionsRouter))))

	// WebSocket endpoint for session updates
	http.HandleFunc(wrapNR("/ws/session", corsMiddleware(h.HandleWebSocket(h.Hub))))

	// Debug fault injection (OBS_TEST_MODE=true required; /debug/fault/*)
	http.HandleFunc(wrapNR("/debug/fault", corsMiddleware(debugfault.FaultRouter)))
	http.HandleFunc(wrapNR("/debug/fault/", corsMiddleware(debugfault.FaultRouter)))

	// Admin endpoints with CORS
	// Reset all data: admin only (and ALLOW_DEV_RESET must be set)
	http.HandleFunc(wrapNR("/admin/reset", corsWithCredentials(h.RequireAuth(h.RequireAdmin(h.ResetAllData)))))

	// Zoom OAuth endpoints
	http.HandleFunc(wrapNR("/auth/zoom/start", corsMiddleware(h.ZoomAuthStart)))
	http.HandleFunc(wrapNR("/auth/zoom/callback", corsMiddleware(h.ZoomAuthCallback)))
	http.HandleFunc(wrapNR("/auth/zoom/disconnect", corsMiddleware(h.ZoomAuthDisconnect)))
	http.HandleFunc(wrapNR("/auth/zoom/me", corsMiddleware(h.ZoomAuthMe)))
	// Zoom transcript status (explicit check before creating session)
	http.HandleFunc(wrapNR("/zoom/transcript-status", corsMiddleware(h.ZoomTranscriptStatus)))

	// API Zoom endpoints (mission: /api/zoom/*)
	http.HandleFunc(wrapNR("/api/zoom/status", corsMiddleware(h.ZoomAPIStatus)))
	http.HandleFunc(wrapNR("/api/zoom/connect", corsMiddleware(h.ZoomAPIConnect)))
	http.HandleFunc(wrapNR("/api/zoom/disconnect", corsMiddleware(h.ZoomAPIDisconnect)))
	http.HandleFunc(wrapNR("/api/zoom/recordings", corsMiddleware(h.ZoomAPIRecordings)))

	// Teams: always register GET /api/teams/status so split-origin dev (e.g. Vite :3000 → API :8081) gets CORS
	// headers even when ENABLE_TEAMS is false (handler returns enabled:false). Unregistered /api/* falls through
	// to SPA 404 without CORS and breaks the browser preflight.
	http.HandleFunc(wrapNR("/api/teams/status", corsMiddleware(h.TeamsAPIStatus)))
	// Google Meet status follows the same always-on pattern.
	http.HandleFunc(wrapNR("/api/google-meet/status", corsMiddleware(h.GoogleMeetAPIStatus)))

	// SCRUM-418: aggregate per-platform enabled+connected status. Authenticated
	// users only — used by Add Content tiles + Manage Connections modal so
	// disabled integrations never render a tile.
	http.HandleFunc(wrapNR("/api/integrations/status", corsWithCredentials(h.RequireAuth(h.IntegrationsStatus))))

	// API Session list (my sessions): GET /api/sessions requires auth
	http.HandleFunc(wrapNR("/api/sessions", corsWithCredentials(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/sessions" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h.RequireAuth(h.ListSessions)(w, r)
	})))
	// API Session import + ingestion status (mission: /api/sessions/:id/import/zoom, /api/sessions/:id/ingestion)
	// Session invitations: POST/GET /api/sessions/:id/invitations; legacy /invite returns 410
	http.HandleFunc(wrapNR("/api/sessions/", corsWithCredentials(h.ApiSessionsRouterWithInvite)))
	// Invitation resolve, accept, register-and-accept, resend, revoke
	http.HandleFunc(wrapNR("/api/invitations/", corsWithCredentials(h.ApiInvitationsRouter)))
	// SCRUM-357: template-driven import jobs (POST /api/import-jobs, GET /api/import-jobs/:id)
	http.HandleFunc(wrapNR("/api/import-jobs", corsWithCredentials(h.ImportJobsRouter)))
	http.HandleFunc(wrapNR("/api/import-jobs/", corsWithCredentials(h.ImportJobsRouter)))

	// Admin user management (RequireAuth + RequireAdmin)
	http.HandleFunc(wrapNR("/api/admin/users", corsWithCredentials(h.RequireAuth(h.RequireAdmin(h.AdminUsersHandler)))))
	http.HandleFunc(wrapNR("/api/admin/users/", corsWithCredentials(h.RequireAuth(h.RequireAdmin(h.AdminUsersHandler)))))

	// API file artifacts (presigned PUT/GET for R2)
	http.HandleFunc(wrapNR("/api/artifacts/", corsWithCredentials(h.ApiArtifactsRouter)))

	// TalkBack auth: signup, login, logout (cookie-based); /api/me requires auth
	http.HandleFunc(wrapNR("/api/auth/signup", corsWithCredentials(h.AuthSignup)))
	http.HandleFunc(wrapNR("/api/auth/login", corsWithCredentials(h.AuthLogin)))
	http.HandleFunc(wrapNR("/api/auth/logout", corsWithCredentials(h.AuthLogout)))
	http.HandleFunc(wrapNR("/api/me", corsWithCredentials(h.RequireAuth(h.AuthMe))))

	// Feature flag: Teams video provider (parallel to Zoom; off by default).
	if os.Getenv("ENABLE_TEAMS") == "true" {
		http.HandleFunc(wrapNR("/auth/teams/start", corsMiddleware(h.TeamsAuthStart)))
		http.HandleFunc(wrapNR("/auth/teams/callback", corsMiddleware(h.TeamsAuthCallback)))
		http.HandleFunc(wrapNR("/api/teams/connect", corsMiddleware(h.TeamsAPIConnect)))
		http.HandleFunc(wrapNR("/api/teams/disconnect", corsMiddleware(h.TeamsAPIDisconnect)))
		http.HandleFunc(wrapNR("/api/teams/recordings", corsMiddleware(h.TeamsAPIRecordings)))
		log.Println("Teams integration: ENABLE_TEAMS=true; OAuth and /api/teams/* routes registered")
	} else {
		log.Println("Teams integration: ENABLE_TEAMS not set; OAuth and other /api/teams/* routes disabled (GET /api/teams/status still returns enabled:false)")
	}

	// Feature flag: Google Meet video provider (parallel to Zoom/Teams; off by default).
	// Status endpoint registers separately under SCRUM-315; this block only wires OAuth handlers.
	if os.Getenv("ENABLE_GOOGLE_MEET") == "true" {
		http.HandleFunc(wrapNR("/auth/google-meet/start", corsMiddleware(h.GoogleMeetAuthStart)))
		http.HandleFunc(wrapNR("/auth/google-meet/callback", corsMiddleware(h.GoogleMeetAuthCallback)))
		http.HandleFunc(wrapNR("/auth/google-meet/disconnect", corsMiddleware(h.GoogleMeetAuthDisconnect)))
		http.HandleFunc(wrapNR("/api/google-meet/connect", corsMiddleware(h.GoogleMeetAPIConnect)))
		http.HandleFunc(wrapNR("/api/google-meet/disconnect", corsMiddleware(h.GoogleMeetAPIDisconnect)))
		http.HandleFunc(wrapNR("/api/google-meet/recordings", corsMiddleware(h.GoogleMeetAPIRecordings)))
		log.Println("Google Meet integration: ENABLE_GOOGLE_MEET=true; OAuth and /api/google-meet/* routes registered")
	} else {
		log.Println("Google Meet integration: ENABLE_GOOGLE_MEET not set; OAuth and other /api/google-meet/* routes disabled (GET /api/google-meet/status still returns enabled:false)")
	}

	// MCP StreamableHTTP handler — optional, requires TALKBACK_MCP_API_KEY.
	// When set, mounts the MCP handler at /mcp/ with Bearer auth middleware.
	// /mcp/healthz and /mcp/ready are unauthenticated so cloud health probes work.
	// The /mcp routes do NOT inherit the API's CORS or cookie middleware — they use Bearer auth only.
	if os.Getenv("TALKBACK_MCP_API_KEY") != "" {
		mcpAuth, err := mcpserver.LoadAuthFromEnv()
		if err != nil {
			log.Fatalf("MCP: config error: %v", err)
		}
		mcpSrv := mcpserver.NewTalkbackMCPServer(mcpserver.TalkbackServerConfig{
			Version: "api",
			DB:      db,
			Storage: store,
			Auth:    mcpAuth,
		})
		mcpSubMux := http.NewServeMux()
		mcpserver.RegisterHealthHTTPRoutes(mcpSubMux, "api", db)
		mcpHTTPHandler := mcp.NewStreamableHTTPHandler(func(_ *http.Request) *mcp.Server {
			return mcpSrv
		}, &mcp.StreamableHTTPOptions{
			Stateless: true,
			// DisableLocalhostProtection is required for cloud deployments (e.g. Render) where
			// the reverse proxy connects to the Go process on 127.0.0.1:PORT while forwarding
			// the original public Host header (e.g. talkback-895n.onrender.com). Without this,
			// the go-sdk DNS rebinding guard rejects every request with 403. The endpoint is
			// already protected by HTTPBearerAuthMiddleware, so disabling the guard is safe here.
			DisableLocalhostProtection: true,
		})
		mcpSubMux.Handle("/", mcpAuth.HTTPBearerAuthMiddleware(mcpHTTPHandler))
		http.Handle("/mcp/", http.StripPrefix("/mcp", mcpSubMux))
		log.Println("MCP: StreamableHTTP handler mounted at /mcp/ (Bearer auth required); /mcp/healthz and /mcp/ready are open")
	} else {
		log.Println("MCP: TALKBACK_MCP_API_KEY not set; /mcp routes not registered (set to enable MCP StreamableHTTP on this service)")
	}

	// Embedded React SPA (production same-origin serving).
	// This must be registered after backend routes so those handlers take precedence.
	registerEmbeddedSPA()

	// Local: PORT defaults to 8080 when unset; Render: provides PORT via env (e.g. 10000).
	// Listen on all interfaces (":port") so Render's health check can reach /health. Do not set BIND_ADDRESS=127.0.0.1 on Render.
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
	server := &http.Server{
		Addr:              addr,
		Handler:           nil,
		ReadHeaderTimeout: 15 * time.Second,
		ErrorLog:          log.New(os.Stderr, "http-server: ", log.LstdFlags),
	}

	serveErrCh := make(chan error, 1)
	go func() {
		log.Printf("HTTP listen begin: pid=%d addr=%s", pid, addr)
		serveErrCh <- server.ListenAndServe()
	}()

	sigCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	select {
	case <-sigCtx.Done():
		sig := "unknown"
		if s := sigCtx.Err(); s != nil {
			sig = s.Error()
		}
		log.Printf("Shutdown signal received: pid=%d signal_ctx=%s uptime=%s", pid, sig, time.Since(bootAt).Round(time.Millisecond))
		shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancelShutdown()
		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Printf("HTTP graceful shutdown failed: pid=%d err=%v", pid, err)
		} else {
			log.Printf("HTTP graceful shutdown complete: pid=%d", pid)
		}
		cancel()
	case err := <-serveErrCh:
		switch {
		case err == nil:
			log.Printf("HTTP server stopped cleanly: pid=%d uptime=%s", pid, time.Since(bootAt).Round(time.Millisecond))
		case err == http.ErrServerClosed:
			log.Printf("HTTP server closed: pid=%d uptime=%s", pid, time.Since(bootAt).Round(time.Millisecond))
		default:
			log.Printf("HTTP server exited with error: pid=%d err=%v uptime=%s", pid, err, time.Since(bootAt).Round(time.Millisecond))
			os.Exit(1)
		}
	}

	log.Printf("Process exit: pid=%d uptime=%s", pid, time.Since(bootAt).Round(time.Millisecond))
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

// bootstrapAdminDisplayName returns the display name for the bootstrap admin:
// from TALKBACK_BOOTSTRAP_ADMIN_DISPLAY_NAME if set, otherwise derived from the email local part (e.g. paresh@... -> Paresh).
func bootstrapAdminDisplayName(email, fromEnv string) string {
	if fromEnv != "" {
		return strings.TrimSpace(fromEnv)
	}
	at := strings.Index(email, "@")
	localPart := email
	if at > 0 {
		localPart = email[:at]
	}
	localPart = strings.TrimSpace(strings.ToLower(localPart))
	if localPart == "" {
		return "User"
	}
	return strings.ToUpper(localPart[:1]) + localPart[1:]
}

// ensureBootstrapAdmin creates an admin user at startup when TALKBACK_BOOTSTRAP_ADMIN_EMAIL
// and TALKBACK_BOOTSTRAP_ADMIN_PASSWORD are set and no user with that email exists.
// If the user already exists, their global_role is ensured to be admin.
// Email, display name and password all come from env: TALKBACK_BOOTSTRAP_ADMIN_EMAIL,
// TALKBACK_BOOTSTRAP_ADMIN_DISPLAY_NAME, TALKBACK_BOOTSTRAP_ADMIN_PASSWORD.
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
	displayName := bootstrapAdminDisplayName(email, auth.Config.BootstrapAdminDisplayName)
	if existing != nil {
		// User exists: ensure they have admin role (idempotent)
		if existing.GlobalRole != models.GlobalRoleAdmin {
			if err := db.SetUserGlobalRole(ctx, existing.ID, models.GlobalRoleAdmin); err != nil {
				return err
			}
			log.Printf("Bootstrap admin: existing user %s promoted to admin", email)
		}
		// If display name is still "Admin", update to configured default (e.g. Paresh) so UI shows name not role.
		if existing.DisplayName == "Admin" {
			if err := db.SetUserDisplayName(ctx, existing.ID, displayName); err != nil {
				log.Printf("Bootstrap admin: failed to update display name for %s: %v", email, err)
			} else {
				log.Printf("Bootstrap admin: set display name to %q for %s", displayName, email)
			}
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
		DisplayName: displayName,
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

// invitationLookup implements invitations.SessionInviterLookup using the database.
type invitationLookup struct {
	db *database.DB
}

func (l *invitationLookup) GetSessionTitle(ctx context.Context, sessionID uuid.UUID) (string, error) {
	s, err := l.db.GetSession(ctx, sessionID)
	if err != nil || s == nil {
		return "", err
	}
	return s.Title, nil
}

func (l *invitationLookup) GetUserDisplayName(ctx context.Context, userID uuid.UUID) (string, error) {
	u, err := l.db.GetUserByID(ctx, userID)
	if err != nil || u == nil {
		return "", err
	}
	return u.DisplayName, nil
}

func (l *invitationLookup) GetUserEmail(ctx context.Context, userID uuid.UUID) (string, error) {
	u, err := l.db.GetUserByID(ctx, userID)
	if err != nil || u == nil {
		return "", err
	}
	return u.Email, nil
}
