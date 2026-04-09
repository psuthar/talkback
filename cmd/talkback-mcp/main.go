// Command talkback-mcp runs the TalkBack Model Context Protocol server over stdio (newline-delimited JSON-RPC).
// Logs must go to stderr; stdout is reserved for the MCP wire protocol.
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

	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatalf("server: %v", err)
	}
}
