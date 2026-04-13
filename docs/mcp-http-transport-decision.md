# ADR: HTTP Transport Protocol for TalkBack MCP Server

**Ticket:** SCRUM-82  
**Date:** 2026-04-13  
**Status:** Decided

## Decision

Use **StreamableHTTP** transport (`mcp.NewStreamableHTTPHandler`) for the TalkBack MCP HTTP server.

## Context

Before implementing HTTP transport in `cmd/talkback-mcp`, we needed to confirm which protocol
Claude Code and Cursor expect when `.mcp.json` uses a `"url"` field: SSE (older, two-endpoint
model) or StreamableHTTP (current standard, single-endpoint).

## Research findings

### Claude Code

Claude Code's `"url"`-based MCP server entries use **StreamableHTTP** (MCP spec 2025-03-26+).
This is the transport the Claude Code client sends when connecting to a URL-based server entry
in `.mcp.json` or `.cursor/mcp.json`.

### Cursor

Cursor also supports **StreamableHTTP** for URL-based MCP connections as of the 2025 client
releases.

### go-sdk v1.4.1 API

`github.com/modelcontextprotocol/go-sdk v1.4.1` ships both transports:

| Type | Function | MCP spec era |
|------|----------|--------------|
| `*mcp.SSEHandler` | `mcp.NewSSEHandler(...)` | 2024-11-05 (legacy) |
| `*mcp.StreamableHTTPHandler` | `mcp.NewStreamableHTTPHandler(...)` | 2025-03-26+ (current) |

`StreamableHTTPOptions` supports stateless mode, session timeouts, event store for stream
resumption, and localhost/cross-origin protection out of the box.

## Decision rationale

- Claude Code and Cursor both connect via **StreamableHTTP** for `"url"` entries.
- SSE is the older two-endpoint model; StreamableHTTP is the current MCP standard.
- `NewStreamableHTTPHandler` is the correct go-sdk type to use.
- No `go.mod` changes required — `v1.4.1` already includes full StreamableHTTP support.

## Implementation guidance (SCRUM-83)

- Use `mcp.NewStreamableHTTPHandler(getServer, opts)` as the HTTP handler.
- Mount it at the root path (`/`) or a dedicated `/mcp` path on `TALKBACK_MCP_HTTP_ADDR`.
- Use `StreamableHTTPOptions{Stateless: true}` for a stateless deployment on Render.com
  (avoids session affinity requirements on the free tier).
- Auth middleware wrapping the handler validates `Authorization: Bearer <TALKBACK_MCP_API_KEY>`
  before the request reaches MCP protocol handling.
- Keep `StdioTransport` as the default when `TALKBACK_MCP_HTTP_ADDR` is not set.
