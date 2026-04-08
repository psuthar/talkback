// Command talkback-mcp runs the TalkBack Model Context Protocol server over stdio (newline-delimited JSON-RPC).
// Logs must go to stderr; stdout is reserved for the MCP wire protocol.
package main

import (
	"context"
	"flag"
	"log"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/psuthar/talkback/internal/database"
	"github.com/psuthar/talkback/internal/mcpserver"
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
		log.Printf("database: connected (get_session_metadata enabled)")
	} else {
		log.Printf("warning: DATABASE_URL not set — get_session_metadata tool will not be registered")
	}

	server := mcpserver.NewTalkbackMCPServer(mcpserver.TalkbackServerConfig{
		Version: version,
		DB:      db,
		Auth:    auth,
	})

	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatalf("server: %v", err)
	}
}
