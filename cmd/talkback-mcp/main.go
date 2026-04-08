// Command talkback-mcp runs the TalkBack Model Context Protocol server over stdio (newline-delimited JSON-RPC).
// Logs must go to stderr; stdout is reserved for the MCP wire protocol.
package main

import (
	"context"
	"flag"
	"log"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"
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

	server := mcp.NewServer(&mcp.Implementation{
		Name:    "talkback-mcp",
		Version: version,
	}, nil)
	server.AddReceivingMiddleware(auth.RequireToolAuthMiddleware())
	mcpserver.Register(server, version)

	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatalf("server: %v", err)
	}
}
