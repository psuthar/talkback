# TalkBack MCP server (`talkback-mcp`)

Stdio [Model Context Protocol](https://modelcontextprotocol.io) server for agents (Cursor, Claude Code, Claude Desktop). **Stdout** carries the MCP JSON-RPC stream; **stderr** is used for operational logs (tool name and duration).

**Protocol wiring:** The binary uses `internal/mcpserver.NewTalkbackMCPServer`, which constructs the official Go SDK server, attaches receiving middleware so **only** `tools/call` is API-key gated (`initialize` / `tools/list` stay open), registers tools, then runs [`mcp.StdioTransport`](https://pkg.go.dev/github.com/modelcontextprotocol/go-sdk/mcp#StdioTransport) (newline-delimited JSON-RPC).

## Build

```bash
go build -o talkback-mcp ./cmd/talkback-mcp
```

## Authentication

The server always requires **`TALKBACK_MCP_API_KEY`** (non-empty) at startup.

| Variable | Required | Purpose |
|----------|----------|---------|
| `TALKBACK_MCP_API_KEY` | Yes | One or more comma-separated shared secrets the server knows (rotation). |
| `TALKBACK_MCP_REQUIRE_CLIENT_KEY` | No | Default **true** (strict): each `tools/call` must include a key matching `TALKBACK_MCP_API_KEY` (see below). Set to **`false`** so Cursor / Claude Code can use tools **without** per-call metadata (typical local dev). |
| `TALKBACK_MCP_ACTING_USER_ID` | No | TalkBack **users.id** UUID for the acting user. Required for **`get_session_metadata`** (access control); otherwise that tool returns 403. |
| `DATABASE_URL` | No | When set, the server registers **`get_session_metadata`** (Postgres). When unset, only `health_check` is available. |

### API key middleware

Implementation: `internal/mcpserver/middleware.go` — `Auth.RequireToolAuthMiddleware`. Only the JSON-RPC method **`tools/call`** is gated. **`initialize`**, **`tools/list`**, **`ping`**, and other non-tool methods pass through without a client key so the MCP handshake and tool discovery work. Keys are compared in constant time (per equal-length candidate); auth failures log the **tool name** only, never key material.

### Strict mode (`TALKBACK_MCP_REQUIRE_CLIENT_KEY` unset or true)

Pass the key on every **`tools/call`** via `_meta` (or HTTP headers when the transport fills `RequestExtra`):

```json
{
  "name": "health_check",
  "arguments": {},
  "_meta": {
    "talkback": { "apiKey": "same-secret-as-TALKBACK_MCP_API_KEY" }
  }
}
```

Alternatives: `_meta.talkbackApiKey`, `_meta.authorization` as `Bearer <token>`, or `Authorization` / `X-API-Key` headers on HTTP-based transports.

### IDE mode (`TALKBACK_MCP_REQUIRE_CLIENT_KEY=false`)

Many IDE hosts do not attach `_meta` on tool calls. Set **`TALKBACK_MCP_REQUIRE_CLIENT_KEY=false`** in the MCP server `env` block (see [One-shot setup](#one-shot-setup-cursor--claude-code)). The process still loads `TALKBACK_MCP_API_KEY` so only your configured subprocess can run; treat this as **local-trust** (weaker than per-call keys).

Invalid keys in strict mode produce an **unauthorized** error for `tools/call` (no secrets in logs).

## Run (local)

```bash
export TALKBACK_MCP_API_KEY=dev-shared-secret
export TALKBACK_MCP_REQUIRE_CLIENT_KEY=false
./talkback-mcp
# optional:
./talkback-mcp -version=1.0.0
```

## One-shot setup (Cursor + Claude Code)

From the **repository root**:

```bash
./scripts/setup-mcp-config.sh
```

This writes **both** (gitignored, not committed):

- **`.cursor/mcp.json`** — Cursor (project or picked up when you open this repo)
- **`.mcp.json`** — Claude Code **project**-scoped MCP

It sets `TALKBACK_MCP_REQUIRE_CLIENT_KEY=false` and generates a random API key unless you preset **`TALKBACK_MCP_API_KEY`**.

Then **fully quit and reopen** Cursor and/or Claude Code so MCP reloads.

Committed reference (edit paths if you copy manually): [`docs/mcp-config.example.json`](mcp-config.example.json).

## Cursor

**Option A — script:** run `./scripts/setup-mcp-config.sh` (see above).

**Option B — manual:** merge this shape into **`~/.cursor/mcp.json`** (global) or **`.cursor/mcp.json`** (project). Same JSON as Claude Code’s `mcpServers` block:

```json
{
  "mcpServers": {
    "talkback": {
      "command": "go",
      "args": ["run", "/absolute/path/to/talkback/cmd/talkback-mcp", "-version=dev"],
      "env": {
        "TALKBACK_MCP_API_KEY": "your-shared-secret",
        "TALKBACK_MCP_REQUIRE_CLIENT_KEY": "false"
      }
    }
  }
}
```

Use a **built binary** by setting `"command"` to the absolute path of `talkback-mcp` and `"args": ["-version=dev"]`, or keep the `go run` form above so you do not need `cwd`.

## Claude Code

**Option A — script:** `./scripts/setup-mcp-config.sh` creates **`.mcp.json`** at the repo root (project scope). Approve the server when prompted (`claude mcp reset-project-choices` if you need to re-approve).

**Option B — user scope:** add the same `talkback` entry under `mcpServers` in **`~/.claude.json`** (see [Claude Code MCP docs](https://code.claude.com/docs/en/mcp)).

**Secrets in git:** prefer `${TALKBACK_MCP_API_KEY}` style expansion in committed templates if your Claude Code version supports it, or keep `.mcp.json` gitignored and generate it with the script.

## Claude Desktop

Merge the same `mcpServers.talkback` object into:

- **macOS:** `~/Library/Application Support/Claude/claude_desktop_config.json`
- **Windows:** `%APPDATA%\Claude\claude_desktop_config.json`

Restart Claude Desktop after saving.

## Tools (SCRUM-32+)

| Tool | Description |
|------|-------------|
| `health_check` | Returns JSON object: `status` (`ok`; `degraded` reserved), `service` (always `talkback-mcp`), `version` (process version, default `dev`). No secrets or session data. Implemented in `internal/mcpserver/health.go`. |
| `get_session_metadata` | Input: `session_id` (UUID). Output: `title`, `created_at`, `owner` (`created_by`, optional `display_name`). Requires `DATABASE_URL` and `TALKBACK_MCP_ACTING_USER_ID`. Errors mirror HTTP semantics in JSON (`http_status` 400/403/404). |

## Hosted / containers

Run the same binary as the container entrypoint with stdio attached. Set `TALKBACK_MCP_API_KEY` and usually **`TALKBACK_MCP_REQUIRE_CLIENT_KEY=true`**. Ensure nothing else writes to stdout.
