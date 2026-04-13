// Command talkback-mcp runs the TalkBack Model Context Protocol server.
//
// By default it runs over stdio (newline-delimited JSON-RPC). When TALKBACK_MCP_HTTP_ADDR is set
// it starts a StreamableHTTP listener on that address instead and stdio is not started.
package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/psuthar/talkback/internal/database"
	"github.com/psuthar/talkback/internal/mcpserver"
	"github.com/psuthar/talkback/internal/storage"
	"github.com/psuthar/talkback/internal/storage/r2"
)

func main() {
	log.SetOutput(os.Stderr)
	log.SetPrefix("talkback-mcp ")
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)

	version := "dev"
	flag.StringVar(&version, "version", version, "server version string exposed in health_check")
	flag.Parse()

	auth, err := mcpserver.LoadAuthFromEnv()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	if !auth.RequireClientKey {
		log.Printf("warning: TALKBACK_MCP_REQUIRE_CLIENT_KEY=false — tool calls do not require a client API key (local IDE mode)")
	}

	var db *database.DB
	if os.Getenv("DATABASE_URL") != "" {
		d, err := database.New()
		if err != nil {
			log.Fatalf("database: %v", err)
		}
		db = d
		defer db.Close()
		log.Printf("database: connected (session tools enabled)")
	} else {
		log.Printf("warning: DATABASE_URL not set — session DB tools will not be registered")
	}
	if db != nil {
		hasGlobal := strings.TrimSpace(os.Getenv("TALKBACK_MCP_ACTING_USER_ID")) != ""
		if auth.HasPerKeyActingUsers() {
			if !auth.RequireClientKey {
				log.Printf("warning: TALKBACK_MCP_KEY_USER_MAP_JSON is set but TALKBACK_MCP_REQUIRE_CLIENT_KEY=false — per-key user map is ignored; set TALKBACK_MCP_ACTING_USER_ID for session tools")
			} else if !hasGlobal {
				log.Printf("info: DATABASE_URL with TALKBACK_MCP_KEY_USER_MAP_JSON — listed API keys use per-key TalkBack users; other valid keys still need TALKBACK_MCP_ACTING_USER_ID or they get acting_user_not_configured on session tools")
			}
		}
		if !hasGlobal && (!auth.HasPerKeyActingUsers() || !auth.RequireClientKey) {
			log.Printf("warning: DATABASE_URL is set but TALKBACK_MCP_ACTING_USER_ID is unset — session tools will return 403 with error_code=acting_user_not_configured; set a TalkBack users.id UUID (or use TALKBACK_MCP_KEY_USER_MAP_JSON with TALKBACK_MCP_REQUIRE_CLIENT_KEY=true)")
		}
	}

	// Optional object storage — same R2 wiring as cmd/api so MCP RAG indexing matches HTTP SessionAsk (R2-backed PDFs).
	var store storage.Interface
	if os.Getenv("STORAGE_DRIVER") == "r2" {
		cfg := r2.LoadConfig()
		if client, err := r2.New(cfg); err != nil {
			log.Printf("R2 storage disabled for MCP: %v (session index may differ from API for R2-only PDFs)", err)
		} else {
			store = client
			log.Printf("R2 storage enabled for MCP (bucket=%s)", cfg.Bucket)
		}
	}

	server := mcpserver.NewTalkbackMCPServer(mcpserver.TalkbackServerConfig{
		Version: version,
		DB:      db,
		Storage: store,
		Auth:    auth,
	})

	// Resolve HTTP listen address: TALKBACK_MCP_HTTP_ADDR takes precedence; fall back to PORT
	// (injected automatically by Render.com and similar PaaS) so the binary works without
	// extra configuration when deployed as a Render web service.
	if httpAddr := resolveHTTPAddr(os.Getenv); httpAddr != "" {
		// HTTP transport mode (StreamableHTTP, stateless).
		// TALKBACK_MCP_HEALTH_ADDR is ignored in this mode — health routes are on the same mux.
		mux := http.NewServeMux()
		mcpserver.RegisterHealthHTTPRoutes(mux, version, db)
		mcpHandler := mcp.NewStreamableHTTPHandler(func(_ *http.Request) *mcp.Server {
			return server
		}, &mcp.StreamableHTTPOptions{Stateless: true})
		mux.Handle("/", auth.HTTPBearerAuthMiddleware(mcpHandler))
		hs := &http.Server{
			Addr:              httpAddr,
			Handler:           mux,
			ReadHeaderTimeout: 5 * time.Second,
		}
		log.Printf("transport: http (StreamableHTTP, stateless) listening on %s", httpAddr)
		if err := hs.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("http: %v", err)
		}
		return
	}

	// Stdio transport mode (default, backward-compatible).
	if addr := strings.TrimSpace(os.Getenv("TALKBACK_MCP_HEALTH_ADDR")); addr != "" {
		mux := http.NewServeMux()
		mcpserver.RegisterHealthHTTPRoutes(mux, version, db)
		hs := &http.Server{
			Addr:              addr,
			Handler:           mux,
			ReadHeaderTimeout: 5 * time.Second,
		}
		go func() {
			log.Printf("health http listening on %s (GET /healthz, GET /ready)", addr)
			if err := hs.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Fatalf("health http: %v", err)
			}
		}()
	}

	log.Printf("transport: stdio")
	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatalf("server: %v", err)
	}
}
