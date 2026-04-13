package main

import "strings"

// resolveHTTPAddr returns the TCP listen address for HTTP transport mode.
//
// Resolution order:
//  1. TALKBACK_MCP_HTTP_ADDR — explicit address (e.g. ":8080"); takes precedence.
//  2. PORT — injected by Render.com and similar PaaS; converted to ":<PORT>".
//
// Returns an empty string when neither env var is set, which keeps the binary
// in stdio transport mode (the default, backward-compatible path).
//
// getenv is injectable so the function can be tested without modifying
// process environment.
func resolveHTTPAddr(getenv func(string) string) string {
	if addr := strings.TrimSpace(getenv("TALKBACK_MCP_HTTP_ADDR")); addr != "" {
		return addr
	}
	if port := strings.TrimSpace(getenv("PORT")); port != "" {
		return ":" + port
	}
	return ""
}
